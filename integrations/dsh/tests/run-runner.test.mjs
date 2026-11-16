import assert from 'node:assert/strict';
import { mkdtemp, readdir, rm, writeFile, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { CliRunner } from '../lib/runs/runner.js';
import { apiRequest } from '../lib/runs/api.js';
import { localSubprocess } from './fixtures/subprocess.mjs';

const fixture = fileURLToPath(new URL('./fixtures/cli-output.mjs', import.meta.url));

test('runner collects bounded JSON without a shell, spill or ambient env override', async () => {
  const provider = localSubprocess(); const runner = new CliRunner(provider, process.execPath);
  const result = await runner.run([fixture, 'error'], process.cwd(), new AbortController().signal);
  assert.equal(result.exitCode, 2); assert.equal(result.receipt.code, 'host_unavailable');
  assert.equal(provider.specs[0].env, undefined);
  assert.deepEqual(provider.specs[0].stdio, { stdin: 'ignore', stdout: { maxBytes: 4096 }, stderr: { maxBytes: 8192 } });
  for (const mode of ['truncated', 'malformed', 'signal']) await assert.rejects(runner.run([fixture, mode], process.cwd(), new AbortController().signal));
  await runner.dispose();
});

test('cancelling one client and disposing the runner settle owned clients only', async () => {
  const provider = localSubprocess(); const runner = new CliRunner(provider, process.execPath);
  const controller = new AbortController();
  const cancelled = runner.run([fixture, 'hang'], process.cwd(), controller.signal);
  const rejected = assert.rejects(cancelled, /cancel|abort/i);
  await new Promise((resolve) => setTimeout(resolve, 30)); controller.abort(); await rejected;
  assert.equal((await runner.run([fixture, 'ok'], process.cwd(), new AbortController().signal)).exitCode, 0);
  const pending = runner.run([fixture, 'hang'], process.cwd(), new AbortController().signal);
  const disposed = assert.rejects(pending, /cancel|abort|disposed/i);
  await new Promise((resolve) => setTimeout(resolve, 30)); await runner.dispose(); await disposed;
  assert.ok(provider.specs.every((spec) => !spec.argv.includes('stop') && !spec.argv.includes('serve')));
});

test('API bridge verifies receipts and cleans only its unique call directory', async (t) => {
  const workspace = await mkdtemp(join(tmpdir(), 'run-api-')); t.after(() => rm(workspace, { recursive: true, force: true }));
  await writeFile(join(workspace, 'keep'), 'mine');
  const provider = localSubprocess(); const real = new CliRunner(provider, process.execPath);
  const access = { workspace, async recheck() {} };
  const runner = { async run(args, cwd, signal) { return real.run([fixture, 'api', ...args], cwd, signal); } };
  const result = await apiRequest(runner, access, '/api/v1/runs?offset=0&limit=20', new AbortController().signal);
  assert.deepEqual(result, { runs: [], has_more: false, next_offset: 0 });
  assert.deepEqual(await readdir(workspace), ['keep']);
  for (const key of ['sha256', 'bytes', 'output']) {
    const wrong = { async run(...args) { const result = await runner.run(...args); result.receipt[key] = key === 'bytes' ? 1 : 'bad'; return result; } };
    await assert.rejects(apiRequest(wrong, access, '/api/v1/protocol', new AbortController().signal), /receipt|response/i);
    assert.deepEqual(await readdir(workspace), ['keep']);
  }
  assert.equal(await readFile(join(workspace, 'keep'), 'utf8'), 'mine');
  await real.dispose();
});

test('deadline and unobservable managed ranges report client cleanup as unknown', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  let spawned = false, terminated = false;
  const provider = {
    async resolveExecutable(path) { return path; },
    spawn() { spawned = true; return { done: new Promise(() => {}), terminate() { terminated = true; }, async waitForExit() { return false; } }; },
  };
  const runner = new CliRunner(provider, '/cli');
  const pending = runner.run([], '/w', new AbortController().signal);
  const checked = assert.rejects(pending, (error) => error.code === 'client_timeout' && error.clientCleanup === 'unknown');
  await Promise.resolve(); await Promise.resolve(); await Promise.resolve();
  assert.equal(spawned, true);
  t.mock.timers.tick(60000);
  await checked; assert.equal(terminated, true); await runner.dispose();
});

test('a response directory substitution never removes files in the replacement', async (t) => {
  const { rename, mkdir } = await import('node:fs/promises');
  const workspace = await mkdtemp(join(tmpdir(), 'run-api-race-')); t.after(() => rm(workspace, { recursive: true, force: true }));
  const access = { workspace, async recheck() {} };
  let replacement;
  const runner = { async run(args) {
    const output = args[args.indexOf('--out') + 1];
    const dir = join(workspace, output.split('/')[0]); replacement = dir;
    await rename(dir, dir + '-moved'); await mkdir(dir);
    await writeFile(join(dir, 'response.json'), 'user-owned');
    return { exitCode: 0, receipt: { output, bytes: 0, sha256: '' } };
  } };
  await assert.rejects(apiRequest(runner, access, '/api/v1/protocol', new AbortController().signal), /identity changed/);
  assert.equal(await readFile(join(replacement, 'response.json'), 'utf8'), 'user-owned');
});

test('a pre-cancelled client never begins executable resolution', async () => {
  let resolved = 0;
  const runner = new CliRunner({ async resolveExecutable() { resolved++; return '/cli'; }, spawn() { throw new Error('unexpected spawn'); } }, '/cli');
  await assert.rejects(runner.run([], '/w', AbortSignal.abort()), /cancel/);
  assert.equal(resolved, 0);
  await runner.dispose();
});
