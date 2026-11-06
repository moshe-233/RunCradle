import { spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdir, mkdtemp, readFile, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createProcessOwner } from './process.mjs';

export const packageRoot = fileURLToPath(new URL('../', import.meta.url));
export const manifest = JSON.parse(await readFile(join(packageRoot, 'package.json'), 'utf8'));

export async function workspace(kind) {
  const parent = resolve(process.env.RUNCRADLE_DSH_TEST_DIR ?? tmpdir());
  await mkdir(parent, { recursive: true });
  const dir = await mkdtemp(join(parent, `runcradle-${kind}-`));
  const records = [];
  const env = { ...process.env, COREPACK_ENABLE_AUTO_PIN: '0', COREPACK_DEFAULT_TO_LATEST: '0' };
  for (const [key, relative] of Object.entries({
    DSH_HOME: 'dsh-home', XDG_RUNTIME_DIR: 'runtime', XDG_CACHE_HOME: 'cache',
    XDG_STATE_HOME: 'state', XDG_DATA_HOME: 'data',
  })) {
    env[key] = join(dir, relative);
    await mkdir(env[key], { recursive: true, mode: 0o700 });
  }
  if (process.env.npm_execpath) {
    const bin = join(dir, 'bin');
    await mkdir(bin);
    await symlink(process.env.npm_execpath, join(bin, 'pnpm'));
    env.PATH = `${bin}:${env.PATH}`;
  }
  const cwd = join(dir, 'workspace');
  await mkdir(cwd);
  async function save() {
    await writeFile(join(dir, 'verification.json'), JSON.stringify({
      kind, node: process.version, cwd, dshHome: env.DSH_HOME, records,
    }, null, 2) + '\n');
  }
  const owner = createProcessOwner({ onInterrupt: async (signal) => {
    records.push({ label: 'interrupted', signal, passed: false });
    await save();
  } });
  async function run(label, command, args, options = {}) {
    owner.assertRunning();
    console.log(`[${kind}] ${label}`);
    const started = new Date().toISOString();
    let stdout = '', stderr = '';
    const child = spawn(command, args, { cwd: options.cwd ?? cwd, env, detached: true, stdio: ['ignore', 'pipe', 'pipe'] });
    child.stdout.on('data', (data) => { stdout += data; });
    child.stderr.on('data', (data) => { stderr += data; });
    const kill = (signal) => {
      if (child.pid) try { process.kill(-child.pid, signal); } catch (error) { if (error.code !== 'ESRCH') throw error; }
    };
    const timeout = setTimeout(() => kill('SIGTERM'), options.timeout ?? 120000);
    const hardTimeout = setTimeout(() => kill('SIGKILL'), (options.timeout ?? 120000) + 5000);
    const closed = new Promise((accept) => {
      child.once('error', (error) => accept({ code: null, signal: null, error: error.message }));
      child.once('close', (code, signal) => accept({ code, signal }));
    });
    const log = `${records.length + 1}-${label}.log`;
    const settled = closed.then(async (outcome) => {
      clearTimeout(timeout);
      clearTimeout(hardTimeout);
      await writeFile(join(dir, log), stdout + stderr);
      records.push({ label, command: [command, ...args], cwd: options.cwd ?? cwd, started, ...outcome, log });
      await save();
      return outcome;
    });
    owner.track(child, settled);
    const outcome = await settled;
    owner.assertRunning();
    if (!options.allowFailure && outcome.code !== 0) throw new Error(`${label} failed (${outcome.code ?? outcome.signal}):\n${stdout}\n${stderr}`);
    return { ...outcome, stdout, stderr };
  }
  console.log(`Evidence: ${dir}`);
  return { dir, cwd, env, records, run, save, owner };
}

export async function pack(work) {
  const dest = join(work.dir, 'artifacts');
  await mkdir(dest);
  await work.run('pack', 'pnpm', ['pack', '--pack-destination', dest], { cwd: packageRoot });
  const file = join(dest, `${manifest.name}-${manifest.version}.tgz`);
  const sha256 = createHash('sha256').update(await readFile(file)).digest('hex');
  await writeFile(join(dest, 'SHA256SUMS'), `${sha256}  ${file.split('/').at(-1)}\n`);
  work.records.push({ label: 'artifact', file, sha256 });
  await work.save();
  return file;
}
