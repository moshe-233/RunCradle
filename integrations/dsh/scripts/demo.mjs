#!/usr/bin/env node
import { spawn, spawnSync } from 'node:child_process';
import { access, appendFile, chmod, copyFile, mkdir, readFile, stat, symlink, writeFile } from 'node:fs/promises';
import { constants } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { once } from 'node:events';

const packageRoot = fileURLToPath(new URL('../', import.meta.url));
const root = resolve(packageRoot, '../..');
const state = join(root, '.work/demo');
const workspace = join(state, 'workspace');
const dshHome = join(state, 'dsh-home');
const profile = join(dshHome, 'profiles/runcradle-demo');
if (process.argv.includes('--stop')) {
  await mkdir(state, { recursive: true });
  await writeFile(join(state, 'stop.request'), String(Date.now()));
  console.log('Stop requested for this demo.');
  process.exit(0);
}
const executable = process.env.RUNCRADLE_CLI || join(root, '.work/bin/runcradle');
const engine = process.env.RUNCRADLE_PROCESS_COMPOSE || join(root, '.cache/process-compose/v1.122.0/process-compose');
const port = Number(process.env.RUNCRADLE_DEMO_PORT || 3085);
if (!Number.isInteger(port) || port < 1024 || port > 65535) throw new Error('RUNCRADLE_DEMO_PORT must be 1024..65535');
const exists = async path => access(path, constants.F_OK).then(() => true, () => false);
for (const dir of [workspace, profile, join(profile, 'node_modules'), join(root, '.work/bin'), join(state, 'runtime')]) await mkdir(dir, { recursive: true, mode: 0o700 });

function command(command, args, cwd = root) {
  const result = spawnSync(command, args, { cwd, stdio: 'inherit', env: { ...process.env, GOCACHE: join(root, '.cache/go-build') } });
  if (result.error || result.status !== 0) throw result.error || new Error(`${command} exited ${result.status}`);
}
if (!await exists(join(packageRoot, 'node_modules/.bin/tsc'))) command('pnpm', ['install', '--frozen-lockfile'], packageRoot);
command(join(packageRoot, 'node_modules/.bin/tsc'), [], packageRoot);
command(process.execPath, ['scripts/build-client.mjs'], packageRoot);
if (!process.env.RUNCRADLE_CLI) command('go', ['build', '-buildvcs=false', '-o', executable, './cmd/runcradle']);
if (!await exists(engine)) command('make', ['prepare']);
await access(engine, constants.X_OK);
const seedExamples = !await exists(join(workspace, 'AGENTS.md'));
for (const file of ['process-compose.yaml', 'ui.json']) {
  if (seedExamples && !await exists(join(workspace, file))) await copyFile(join(root, 'examples/dsh-demo', file), join(workspace, file));
}
await writeFile(join(workspace, 'AGENTS.md'), `# RunCradle 工作区\n\n当前工作区为 ${workspace}，RunCradle 服务已由启动器启动。\n创建、修改、保存、运行工作流或查看结果时使用 runcradle Skill，用户只需描述业务需求。\n右侧 RunCradle 工作台和 Agent 工具共用当前工作区的工作流库与运行记录。\n`);

await writeFile(join(profile, 'package.json'), JSON.stringify({ name: 'dsh-profile-runcradle-demo', private: true,
  dependencies: { 'dsh-runcradle': `link:${packageRoot}` },
  dsh: { profile: { bundles: ['@deepseek-ai/dsh-base', '@deepseek-ai/dsh-web-app', 'dsh-runcradle'] } },
}, null, 2));
const pluginLink = join(profile, 'node_modules/dsh-runcradle');
if (!await exists(pluginLink)) await symlink(packageRoot, pluginLink, 'dir');

// Only reuse model-provider rows; no daily profile modifications or unrelated plugin settings.
// The copied rows may contain credentials: keep them in ignored, private demo state and never print them.
const sourceProfile = process.env.RUNCRADLE_DEMO_MODEL_PROFILE;
let modelPatch = '';
if (sourceProfile && await exists(sourceProfile)) {
  const source = await readFile(sourceProfile, 'utf8');
  modelPatch = source.split(/(?=^- id: )/m).filter(block => /^- id: (llm-pi-ai|agent-default-model)\s*$/m.test(block.split('\n')[0])).join('\n');
}
if (!await exists(join(profile, 'cordis.patch.yml'))) await writeFile(join(profile, 'cordis.patch.yml'), modelPatch || '[]\n', { mode: 0o600 });
// Register the directory through DSH's registry, so the chat and panel share
// the same workspace without requiring a native folder chooser.
const bootstrap = join(state, 'bootstrap.mjs');
await writeFile(bootstrap, `export const inject = ['workspaceRegistry'];\nexport async function apply(ctx) { await ctx.workspaceRegistry.create(${JSON.stringify(workspace)}, 'RunCradle Demo'); }\n`);
const patch = `- id: runcradle-host\n  config:\n    enableSkill: true\n    runcradleExecutable: ${JSON.stringify(executable)}\n    demoWorkspace: ${JSON.stringify(workspace)}\n- id: webserver\n  config:\n    host: 127.0.0.1\n    port: ${port}\n    compression: gzip\n- insert:\n    - id: runcradle-demo-workspace\n      name: ${JSON.stringify(pathToFileURL(bootstrap).href)}\n- id: web-runtime\n  config:\n    openBrowser: ${!process.argv.includes('--no-open')}\n    printUrl: true\n    surfaceContext: true\n`;
await writeFile(join(state, 'demo.patch.yml'), patch, { mode: 0o600 });
await chmod(join(profile, 'cordis.patch.yml'), 0o600);
console.log(`Demo workspace: ${workspace}\nDSH profile: runcradle-demo\nDSH address: http://127.0.0.1:${port}\nModel configuration: ${modelPatch ? 'copied from requested profile' : 'use the account/model configured in this demo'}\nCtrl+C stops this demo's DSH and RunCradle processes.\n`);

const children = [];
let closing = false;
const startedAt = Date.now();
const stopWatcher = setInterval(async () => {
  try { if ((await stat(join(state, 'stop.request'))).mtimeMs >= startedAt) await shutdown(); } catch { /* no stop requested */ }
}, 1000);
async function shutdown(code = 0) {
  if (closing) return; closing = true;
  clearInterval(stopWatcher);
  for (const child of children.toReversed()) {
    if (child.exitCode !== null || child.signalCode !== null) continue;
    try { process.kill(-child.pid, 'SIGTERM'); } catch { /* already exited */ }
  }
  const timer = setTimeout(() => {
    for (const child of children) if (child.exitCode === null && child.signalCode === null) {
      try { process.kill(-child.pid, 'SIGKILL'); } catch { /* already exited */ }
    }
  }, 15000);
  await Promise.allSettled(children.map(child => child.exitCode !== null || child.signalCode !== null ? undefined : once(child, 'close')));
  clearTimeout(timer); process.exitCode = code;
}
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => { void shutdown(); });
function start(command, args, options = {}) {
  const child = spawn(command, args, { cwd: workspace, detached: true, ...options });
  children.push(child);
  child.on('error', error => { console.error(error.message); void shutdown(1); });
  child.on('close', code => { if (!closing) { console.log(`${command} exited (${code})`); void shutdown(code || 0); } });
  return child;
}
let workbenchUrl;
const service = start(executable, ['serve', '--workspace', workspace, '--process-compose', engine, '--frame-origin', `http://127.0.0.1:${port}`], { stdio: ['ignore', 'pipe', 'inherit'] });
await new Promise((accept, reject) => {
  let output = '';
  const timer = setTimeout(() => reject(new Error('RunCradle startup timed out')), 15000);
  service.once('close', () => { clearTimeout(timer); reject(new Error('RunCradle exited before readiness')); });
  service.stdout.on('data', chunk => {
    output += chunk;
    if (!output.includes('\n')) return;
    try {
      const ready = JSON.parse(output.split('\n')[0]);
      if (ready.status !== 'serving') throw new Error('RunCradle did not become ready');
      workbenchUrl = ready.pairing_url;
      clearTimeout(timer); console.log(`RunCradle ready: ${ready.url}`); accept();
    } catch (error) { clearTimeout(timer); reject(error); }
  });
}).catch(async error => { console.error(error.message); await shutdown(1); });
if (!closing) {
  await appendFile(join(state, 'demo.patch.yml'), `- id: runcradle-host\n  config:\n    enableSkill: true\n    runcradleExecutable: ${JSON.stringify(executable)}\n    demoWorkspace: ${JSON.stringify(workspace)}\n    demoWorkbenchUrl: ${JSON.stringify(workbenchUrl)}\n`);
  const env = { ...process.env, DSH_HOME: dshHome, XDG_RUNTIME_DIR: join(state, 'runtime') };
  const logPath = join(state, 'dsh.log');
  await writeFile(logPath, '', { mode: 0o600 });
  const dsh = start('dsh', ['--profile', 'runcradle-demo', '--patch', join(state, 'demo.patch.yml')], { env, stdio: ['ignore', 'pipe', 'pipe'] });
  let logTail = Promise.resolve();
  const remember = data => {
    logTail = logTail.then(() => appendFile(logPath, data)).catch(error => console.error(`Could not append demo log: ${error.message}`));
  };
  dsh.stdout.on('data', data => { process.stdout.write(data); void remember(data); });
  dsh.stderr.on('data', data => { process.stderr.write(data); void remember(data); });
}
