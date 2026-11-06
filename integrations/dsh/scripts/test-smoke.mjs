import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { cp, mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { createInterface } from 'node:readline';
import { setTimeout as delay } from 'node:timers/promises';
import { manifest, packageRoot, pack, workspace } from './support.mjs';
import { stopOwnedProcess } from './process.mjs';

const work = await workspace('smoke');
const profile = 'runcradle-entry';
const profileDir = join(work.env.DSH_HOME, 'profiles', profile);
const patchPath = join(profileDir, 'cordis.patch.yml');
const driver = join(work.dir, 'driver.mjs');
const overlay = join(work.dir, 'driver.patch.yml');
const children = new Set();
let sequence = 0;

async function patch({ hmr = true, skill = true, disabled = false } = {}) {
  await writeFile(patchPath, `- id: hmr\n  disabled: ${!hmr}\n- id: runcradle-host\n  disabled: ${disabled}\n  config:\n    enableSkill: ${skill}\n`);
}

async function start(label) {
  work.owner.assertRunning();
  console.log(`[smoke] start ${label}`);
  const child = spawn('dsh', ['--profile', profile, '--patch', overlay], {
    cwd: work.cwd, env: work.env, detached: true, stdio: ['pipe', 'pipe', 'pipe'],
  });
  children.add(child);
  let output = '', ready = false, disposed = false, exit;
  const requests = new Map();
  const lines = createInterface({ input: child.stdout });
  child.stdout.on('data', (data) => { output += data; });
  child.stderr.on('data', (data) => { output += data; });
  const closed = new Promise((accept) => {
    child.once('error', (error) => { exit = { error: error.message }; accept(exit); });
    child.once('close', (code, signal) => { exit = { code, signal }; accept(exit); });
  });
  lines.on('line', (line) => {
    if (!line.startsWith('@@RUNCRADLE_TEST@@')) return;
    const response = JSON.parse(line.slice('@@RUNCRADLE_TEST@@'.length));
    if (response.ready) ready = true;
    if (response.disposed) disposed = true;
    const request = requests.get(response.id);
    if (request) {
      requests.delete(response.id);
      response.error ? request.reject(new Error(response.error)) : request.resolve(response.result);
    }
  });
  const log = join(work.dir, `${label}.log`);
  work.owner.track(child, closed.then(async (outcome) => {
    await writeFile(log, output);
    work.records.push({ label: `process-${label}`, ...outcome, cleanupAcknowledged: disposed, log });
    await work.save();
  }));
  async function stop() {
    let clean = false;
    try {
      await stopOwnedProcess(child, closed, { completed: () => disposed });
      clean = true;
    } finally {
      lines.close();
      children.delete(child);
      await writeFile(log, output);
      work.records.push({ label: `stop-${label}`, ...exit, cleanupAcknowledged: disposed, passed: clean, log });
      await work.save();
    }
  }
  const deadline = Date.now() + 30000;
  while (!ready) {
    if (exit || Date.now() > deadline) {
      await stop();
      throw new Error(`${label} did not become ready: ${JSON.stringify(exit)}\n${output.slice(-4000)}`);
    }
    await delay(50);
  }
  async function request(method) {
    work.owner.assertRunning();
    if (exit) throw new Error(`${label} exited: ${output.slice(-4000)}`);
    const id = ++sequence;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { requests.delete(id); reject(new Error(`${method} timed out\n${output.slice(-4000)}`)); }, 15000);
      requests.set(id, { resolve(value) { clearTimeout(timer); resolve(value); }, reject(error) { clearTimeout(timer); reject(error); } });
      child.stdin.write(JSON.stringify({ id, method }) + '\n');
    });
  }
  async function wait(expected, afterReload = -1) {
    const deadline = Date.now() + 15000;
    let actual;
    do {
      actual = await request('snapshot');
      if (Object.entries(expected).every(([key, value]) => actual[key] === value) && actual.reloads > afterReload) return actual;
      await delay(50);
    } while (Date.now() < deadline);
    throw new Error(`Expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n${output.slice(-4000)}`);
  }
  return { request, wait, stop };
}

async function record(label, result) {
  work.records.push({ label, passed: true, result });
  await work.save();
}

try {
  assert.equal((await work.run('pnpm-version', 'pnpm', ['--version'])).stdout.trim(), '12.9.1');
  assert.equal((await work.run('dsh-version', 'dsh', ['--version'])).stdout.trim(), manifest.engines.dsh);
  const tarball = await pack(work);
  await work.run('install', 'dsh', ['plugin', '--profile', profile, 'add', tarball]);
  const installed = JSON.parse(await readFile(join(profileDir, 'package.json'), 'utf8'));
  assert.ok(installed.dsh.profile.bundles.includes(manifest.name));
  await patch();
  const dump = await work.run('dump-config', 'dsh', ['--profile', profile, '--dump-config']);
  assert.equal((dump.stdout.match(/id: runcradle-host/g) ?? []).length, 1);
  await cp(join(packageRoot, 'tests/fixtures/driver.mjs'), driver);
  await writeFile(overlay, `- insert:\n    - id: runcradle-entry-test-driver\n      name: ${JSON.stringify(driver)}\n`);

  const live = await start('hmr');
  try {
    let state = await live.wait({ tools: 1, skills: 1, active: true });
    await record('real-agent-dispatch', await live.request('exercise'));
    for (let i = 0; i < 3; i++) {
      await patch({ skill: false });
      state = await live.wait({ tools: 1, skills: 0, active: true }, state.reloads);
      await patch({ disabled: true });
      state = await live.wait({ tools: 0, skills: 0, active: false }, state.reloads);
      await patch();
      state = await live.wait({ tools: 1, skills: 1, active: true }, state.reloads);
    }
    await record('three-hmr-cycles', state);
    await work.run('remove-live', 'dsh', ['plugin', '--profile', profile, 'remove', manifest.name]);
    await record('live-uninstall', await live.wait({ tools: 0, skills: 0, active: false }));
  } finally { await live.stop(); }

  await work.run('reinstall', 'dsh', ['plugin', '--profile', profile, 'add', tarball]);
  await patch({ hmr: false });
  const cold = await start('no-hmr-before');
  try {
    await cold.wait({ tools: 1, skills: 1, active: true });
    await patch({ hmr: false, skill: false });
    await delay(1500);
    await record('no-hmr-keeps-current-config', await cold.wait({ tools: 1, skills: 1, active: true }));
  } finally { await cold.stop(); }
  const changed = await start('no-hmr-after');
  try { await record('no-hmr-restart-applies-config', await changed.wait({ tools: 1, skills: 0, active: true })); }
  finally { await changed.stop(); }
  await work.run('remove-stopped', 'dsh', ['plugin', '--profile', profile, 'remove', manifest.name]);
  const removed = await start('no-hmr-removed');
  try { await record('no-hmr-uninstall', await removed.wait({ tools: 0, skills: 0, active: false })); }
  finally { await removed.stop(); }

  const badDir = join(work.dir, 'incompatible');
  await mkdir(badDir);
  await writeFile(join(badDir, 'package.json'), JSON.stringify({
    name: 'dsh-runcradle-incompatible-fixture', version: '0.0.0',
    engines: { dsh: '0.0.0' }, dsh: { bundle: { patch: './cordis.patch.yml' } },
  }));
  await writeFile(join(badDir, 'cordis.patch.yml'), '[]\n');
  const incompatible = await work.run('incompatible-version', 'dsh', ['plugin', '--profile', profile, 'add', badDir], { allowFailure: true });
  assert.ok(Number.isInteger(incompatible.code) && incompatible.code !== 0);
  assert.match(incompatible.stdout + incompatible.stderr, /incompatib|engines\.dsh|0\.0\.0/i);
  await record('incompatible-version-rejected', { exitCode: incompatible.code });
  await record('complete', { runtimeAcceptance: true, externalModelCalls: false, manualAcceptance: false });
  console.log('PASS: isolated CLI installation, Agent scope/dispatch, HMR, restart, uninstall and version rejection');
} catch (error) {
  if (work.owner.interrupted) await work.owner.finished;
  work.records.push({ label: 'failure', passed: false, error: error.stack });
  await work.save();
  throw error;
} finally {
  for (const child of children) if (child.pid && child.exitCode === null && child.signalCode === null) {
    try { process.kill(-child.pid, 'SIGTERM'); } catch (error) { if (error.code !== 'ESRCH') throw error; }
  }
  work.owner.close();
}
