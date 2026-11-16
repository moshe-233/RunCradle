import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { cp, mkdir, readdir, readFile, writeFile } from 'node:fs/promises';
import { isAbsolute, join } from 'node:path';
import { createInterface } from 'node:readline';
import { setTimeout as delay } from 'node:timers/promises';
import { manifest, packageRoot, pack, workspace } from './support.mjs';
import { stopOwnedProcess } from './process.mjs';
import { exerciseWorkflowRuntime } from './workflow-runtime.mjs';

const cli = process.env.RUNCRADLE_CLI;
const engine = process.env.RUNCRADLE_PROCESS_COMPOSE;
assert.ok(cli && isAbsolute(cli) && engine && isAbsolute(engine), 'Set absolute RUNCRADLE_CLI and RUNCRADLE_PROCESS_COMPOSE; missing real engine is not a pass');
const work = await workspace('runtime');
const children = [];
const profile = 'runcradle-runtime';
async function record(label, result) { work.records.push({ label, passed: true, result }); await work.save(); }
async function start(label, command, args, marker) {
  const child = spawn(command, args, { cwd: work.cwd, env: work.env, detached: true, stdio: ['pipe', 'pipe', 'pipe'] });
  let output = '', ready = false, disposed = false, seq = 0;
  const requests = new Map();
  const closed = new Promise((resolve) => { child.once('error', (error) => resolve({ error: error.message })); child.once('close', (code, signal) => resolve({ code, signal })); });
  const lines = createInterface({ input: child.stdout });
  child.stdout.on('data', (data) => { output += data; }); child.stderr.on('data', (data) => { output += data; });
  lines.on('line', (line) => {
    if (marker && !line.startsWith(marker)) return;
    let data; try { data = JSON.parse(marker ? line.slice(marker.length) : line); } catch { return; }
    if (data.ready || data.status === 'serving') ready = true;
    if (data.disposed) disposed = true;
    const request = requests.get(data.id);
    if (request) { requests.delete(data.id); data.error ? request.reject(new Error(data.error)) : request.resolve(data.result); }
  });
  const settled = closed.then(async (result) => { await writeFile(join(work.dir, `${label}.log`), output); work.records.push({ label, command: [command, ...args], ...result, cleanupAcknowledged: disposed }); await work.save(); return result; });
  work.owner.track(child, settled);
  let stopped = false;
  const live = {
    child,
    async stop() {
      if (stopped) return; stopped = true;
      try {
        // RunCradle's documented SIGTERM completion is 143; DSH acknowledges exit 0.
        const expected = marker ? closed : closed.then((value) => {
          assert.equal(value.code, 143, JSON.stringify(value));
          assert.equal(value.signal, null);
          return { code: 0, signal: null };
        });
        await stopOwnedProcess(child, expected, { completed: () => !marker || disposed });
      }
      finally { lines.close(); await settled; }
    },
    request(method, params = {}) {
      const id = ++seq;
      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => { requests.delete(id); reject(new Error(`${method} timed out: ${output.slice(-4000)}`)); }, 90000);
        requests.set(id, { resolve(v) { clearTimeout(timer); resolve(v); }, reject(e) { clearTimeout(timer); reject(e); } });
        child.stdin.write(JSON.stringify({ id, method, params }) + '\n');
      });
    },
  };
  children.push(live);
  const deadline = Date.now() + 30000;
  while (!ready) { if (child.exitCode !== null || child.signalCode || Date.now() > deadline) throw new Error(`${label} not ready: ${output.slice(-6000)}`); await delay(50); }
  return live;
}
try {
  assert.equal((await work.run('dsh-version', 'dsh', ['--version'])).stdout.trim(), manifest.engines.dsh);
  await work.run('engine-version', engine, ['version']);
  await writeFile(join(work.cwd, 'flow.yaml'), 'processes:\n  long:\n    command: "sleep 120"\n  success:\n    command: "echo once >> calls"\n  failure:\n    command: "exit 7"\n');
  const server = await start('external-server', cli, ['serve', '--workspace', work.cwd, '--process-compose', engine]);
  const tgz = await pack(work);
  await work.run('install', 'dsh', ['plugin', '--profile', profile, 'add', tgz]);
  const profileDir = join(work.env.DSH_HOME, 'profiles', profile);
  await writeFile(join(profileDir, 'cordis.patch.yml'), `- id: hmr\n  disabled: true\n- id: runcradle-host\n  config:\n    runcradleExecutable: ${JSON.stringify(cli)}\n`);
  const driver = join(work.dir, 'runtime-driver.mjs'); await cp(join(packageRoot, 'tests/fixtures/runtime-driver.mjs'), driver);
  const overlay = join(work.dir, 'driver.patch.yml');
  await writeFile(overlay, `- insert:\n    - id: runcradle-runtime-test-driver\n      name: ${JSON.stringify(driver)}\n`);
  const dsh = await start('dsh', 'dsh', ['--profile', profile, '--patch', overlay], '@@RUNCRADLE_RUNTIME@@');
  const create = (key, mode = 'danger-full-access', approval) => dsh.request('create', { key, cwd: work.cwd, mode, approval });
  async function call(key, name, args, extra) {
    const value = await dsh.request('call', { key, name: 'runcradle_' + name, args, ...extra });
    assert.equal(value.result.isError, false, JSON.stringify(value));
    return value;
  }
  async function ok(key, name, args) { const got = await call(key, name, args); assert.equal(got.result.value.ok, true, JSON.stringify(got)); return got.result.value.result; }
  async function terminal(run_id) {
    const deadline = Date.now() + 20000;
    while (Date.now() < deadline) { const state = await ok('B', 'inspect', { run_id }); if (state.final) { assert.equal(state.cleanup, 'complete', JSON.stringify(state)); return state; } await delay(100); }
    throw new Error('Run did not reach terminal state');
  }
  async function running(key, run_id) {
    const deadline = Date.now() + 15000;
    while (Date.now() < deadline) {
      const state = await ok(key, 'inspect', { run_id });
      assert.equal(state.final, false, JSON.stringify(state));
      if (state.nodes?.some((node) => node.state === 'running')) return;
      await delay(100);
    }
    throw new Error('Run did not enter a running node');
  }
  assert.equal((await create('A')).length, 11);
  await exerciseWorkflowRuntime({
    call: (key, name, args) => dsh.request('call', { key, name, args }), create,
    dispose: (key) => dsh.request('dispose', { key }),
    configure: async () => {
      assert.equal((await dsh.request('configure', { config: { enableSkill: false } })).length, 1);
      assert.equal((await dsh.request('configure', { config: { runcradleExecutable: cli } })).length, 11);
    }, workspace: work.cwd, record,
  });
  const first = await ok('A', 'run', { request_id: 'cross-session/一', file: 'flow.yaml', targets: ['long'] });
  assert.equal(first.accepted, true); assert.equal(first.final, false);
  await running('A', first.run_id);
  await dsh.request('dispose', { key: 'A' }); await create('B');
  const found = await ok('B', 'runs', {}); assert.ok(found.runs.some((v) => v.run_id === first.run_id));
  assert.equal((await ok('B', 'inspect', { request_id: 'cross-session/一' })).run_id, first.run_id);
  const stop = await ok('B', 'stop', { run_id: first.run_id }); assert.equal(stop.status, 'stop_requested');
  const cancelled = await terminal(first.run_id); assert.equal(cancelled.reason, 'cancelled');
  await record('cross-session', { run_id: first.run_id, stop, final: cancelled });

  for (const [target, expected] of [['success', 'succeeded'], ['failure', 'failed'], ['long', 'timed_out']]) {
    const args = { request_id: `outcome/${target}`, file: 'flow.yaml', targets: [target], timeout_ms: target === 'long' ? 5000 : 10000 };
    const submitted = await ok('B', 'run', args);
    const replay = await ok('B', 'run', args); assert.equal(replay.run_id, submitted.run_id);
    const conflict = await call('B', 'run', { ...args, timeout_ms: args.timeout_ms + 1 }); assert.equal(conflict.result.value.ok, false);
    const result = await terminal(submitted.run_id); assert.equal(result.status, expected, JSON.stringify(result));
    await record(`outcome-${target}`, { run_id: submitted.run_id, result, conflict: conflict.result.value });
  }
  assert.equal(await readFile(join(work.cwd, 'calls'), 'utf8'), 'once\n');
  await writeFile(join(work.cwd, 'library-input.json'), JSON.stringify({ format: 'yaml', entry: 'saved.yaml', source: 'processes:\n  saved:\n    command: "true"\n' }));
  await work.run('create-saved-fixture', cli, ['api', 'request', '--workspace', work.cwd, '--method', 'POST', '--path', '/api/v1/workflows', '--input', 'library-input.json', '--out', 'library-response.json']);
  const saved = JSON.parse(await readFile(join(work.cwd, 'library-response.json'), 'utf8'));
  const savedRun = await ok('B', 'run', { request_id: 'saved-workflow', workflow_id: saved.workflow.workflow_id, revision: saved.revision, targets: ['saved'] });
  assert.equal((await terminal(savedRun.run_id)).task_result, 'succeeded');
  const savedPage = await ok('B', 'runs', { workflow_id: saved.workflow.workflow_id, limit: 1 });
  assert.equal(savedPage.runs[0].run_id, savedRun.run_id); assert.equal(savedPage.next_offset, 1);
  assert.equal((await ok('B', 'runs', { workflow_id: saved.workflow.workflow_id, offset: 1 })).runs.length, 0);
  assert.equal((await ok('B', 'inspect', { run_id: savedRun.run_id, node: 'saved' })).nodes[0].name, 'saved');
  assert.equal((await ok('B', 'inspect', { run_id: savedRun.run_id, nodes_after: 0, limit: 1 })).nodes.length, 1);
  const events = await ok('B', 'inspect', { run_id: savedRun.run_id, events_after: '0', limit: 1 });
  assert.equal(typeof events.next_seq, 'string'); assert.equal(typeof events.events[0].seq, 'string');
  await record('saved-workflow-and-views', { run_id: savedRun.run_id, workflow_id: saved.workflow.workflow_id, events });
  const active = [];
  for (let i = 0; i < 4; i++) active.push((await ok('B', 'run', { request_id: `capacity/${i}`, file: 'flow.yaml', targets: ['long'] })).run_id);
  const rejected = await call('B', 'run', { request_id: 'capacity/overflow', file: 'flow.yaml', targets: ['long'] });
  assert.equal(rejected.result.value.ok, false); assert.equal(rejected.result.value.result.reason, 'not_submitted');
  await record('capacity', rejected.result.value);
  for (const run_id of active) { await running('B', run_id); await ok('B', 'stop', { run_id }); await terminal(run_id); }

  for (const mode of ['read-only', 'workspace-write']) {
    await create(mode, mode, 'rejected');
    const before = await readdir(work.cwd);
    const denied = await call(mode, 'run', { request_id: `denied/${mode}`, file: 'flow.yaml', targets: ['success'] });
    assert.equal(denied.result.value.ok, false); assert.equal(denied.approvals, 1);
    const query = await call(mode, 'runs', {}); assert.equal(query.result.value.ok, mode === 'workspace-write');
    assert.deepEqual(await readdir(work.cwd), before);
    await create(mode + '-allow', mode, 'allowed-once');
    const approved = await call(mode + '-allow', 'inspect', { run_id: first.run_id });
    assert.equal(approved.result.value.ok, true);
    assert.equal(approved.approvals, mode === 'read-only' ? 1 : 0);
    const granted = await call(mode + '-allow', 'run', { request_id: 'approved/' + mode, file: 'flow.yaml', targets: ['long'] });
    assert.equal(granted.result.value.ok, true);
    assert.equal(granted.approvals, (mode === 'read-only' ? 1 : 0) + 1);
    await running('B', granted.result.value.result.run_id);
    const grantedStop = await call(mode + '-allow', 'stop', { run_id: granted.result.value.result.run_id });
    assert.equal(grantedStop.result.value.ok, true); assert.equal(grantedStop.approvals, granted.approvals + 1);
    await terminal(granted.result.value.result.run_id);
    await record(`permissions-${mode}`, { denied: denied.result.value, query: query.result.value.ok, approved: approved.result.value.ok });
  }
  await create('unavailable', 'read-only');
  assert.equal((await call('unavailable', 'runs', {})).result.value.ok, false);
  await create('approval-cancel', 'read-only', 'pending');
  const beforeApprovalCancel = await readdir(work.cwd);
  const approvalCancel = await dsh.request('call', { key: 'approval-cancel', name: 'runcradle_run', args: { request_id: 'approval-cancel', file: 'flow.yaml', targets: ['long'] }, cancel_ms: 50 });
  assert.equal(approvalCancel.result.isError, true); assert.equal(approvalCancel.approvals, 1);
  assert.deepEqual(await readdir(work.cwd), beforeApprovalCancel);
  assert.equal((await call('B', 'inspect', { request_id: 'approval-cancel' })).result.value.ok, false);
  await record('cancelled-approval', { noSubmission: true, noTemporaryFiles: true });
  // Delay only the client receipt AFTER the real CLI has submitted successfully.
  const delayedCLI = join(work.dir, 'delayed-cli.mjs');
  await writeFile(delayedCLI, `#!/usr/bin/env node
import { spawnSync } from 'node:child_process';
import { writeFileSync, appendFileSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
const args = process.argv.slice(2);
appendFileSync(${JSON.stringify(join(work.dir, 'client-calls.jsonl'))}, JSON.stringify(args) + '\\n');
const result = spawnSync(${JSON.stringify(cli)}, args, { encoding: 'utf8' });
const request = args[args.indexOf('--request-id') + 1];
if (args[0] === 'workflow' && args[1] === 'run' && request?.startsWith('lost/') && result.status === 0) {
  writeFileSync(join(${JSON.stringify(work.cwd)}, 'held-' + request.split('/').at(-1)), result.stdout);
  setInterval(() => {}, 1000);
} else if (args[0] === 'api' && args[1] === 'request' && args.includes('--input') && args[args.indexOf('--path') + 1] === '/api/v1/workflows' && result.status === 0) {
  const input = JSON.parse(readFileSync(join(${JSON.stringify(work.cwd)}, args[args.indexOf('--input') + 1]), 'utf8'));
  const marker = input.source.match(/# workflow-held-(cancel|dispose|configure)/)?.[1];
  if (marker) {
    const reply = JSON.parse(result.stdout);
    writeFileSync(join(${JSON.stringify(work.cwd)}, 'held-workflow-' + marker), readFileSync(join(${JSON.stringify(work.cwd)}, reply.output)));
    setInterval(() => {}, 1000);
  } else { process.stdout.write(result.stdout ?? ''); process.stderr.write(result.stderr ?? ''); process.exit(result.status ?? 125); }
} else { process.stdout.write(result.stdout ?? ''); process.stderr.write(result.stderr ?? ''); process.exit(result.status ?? 125); }
`, { mode: 0o700 });
  for (const action of ['cancel', 'dispose', 'configure']) {
    const draft = { format: 'yaml', entry: 'held.yaml', source: '# workflow-held-' + action + '\nprocesses:\n  held:\n    command: "true"\n' };
    const validated = await ok('B', 'workflow_validate', { draft, output_file: 'held-draft-' + action + '.json' });
    await dsh.request('configure', { config: { runcradleExecutable: delayedCLI } });
    await create('wf-lost-' + action);
    const pendingCall = dsh.request('call', { key: 'wf-lost-' + action, name: 'runcradle_workflow_save', args: { draft, draft_revision: validated.document_revision } });
    let saved;
    const deadline = Date.now() + 15000;
    while (Date.now() < deadline) {
      try { saved = JSON.parse(await readFile(join(work.cwd, 'held-workflow-' + action), 'utf8')); break; }
      catch (error) { if (error.code !== 'ENOENT') throw error; await delay(50); }
    }
    assert.ok(saved?.workflow?.workflow_id, 'Real workflow save must precede client cancellation');
    const visible = await ok('B', 'workflow_get', { workflow_id: saved.workflow.workflow_id, output_file: 'held-visible-' + action + '.json' });
    assert.equal(visible.document_revision, validated.document_revision);
    await dsh.request(action, action === 'configure' ? { config: { enableSkill: false } } : { key: 'wf-lost-' + action });
    const interrupted = await pendingCall;
    assert.ok(interrupted.result.isError || (interrupted.result.value.ok === false && interrupted.result.value.result.mutation === 'unknown'), JSON.stringify(interrupted));
    await dsh.request('configure', { config: { runcradleExecutable: cli } });
    assert.equal((await ok('B', 'workflow_get', { workflow_id: saved.workflow.workflow_id, output_file: 'held-recovered-' + action + '.json' })).head_revision, validated.document_revision);
    // Identity comes from test-only fault instrumentation, not a product recovery guarantee.
    await record('workflow-lost-reply-' + action, { workflow_id: saved.workflow.workflow_id, revision: validated.document_revision, mutationRemainsUnknown: true, savedContentSurvived: true });
  }
  for (const action of ['cancel', 'dispose', 'configure']) {
    await dsh.request('configure', { config: { runcradleExecutable: delayedCLI } });
    await create('lost-' + action);
    const pendingCall = dsh.request('call', { key: 'lost-' + action, name: 'runcradle_run', args: { request_id: 'lost/' + action, file: 'flow.yaml', targets: ['long'] } });
    let receipt;
    const deadline = Date.now() + 15000;
    while (Date.now() < deadline) {
      try { receipt = JSON.parse(await readFile(join(work.cwd, 'held-' + action), 'utf8')); break; }
      catch (error) { if (error.code !== 'ENOENT') throw error; await delay(50); }
    }
    assert.ok(receipt?.accepted, 'Real submission must precede cancellation');
    // Another Agent can query while the first client's receipt is blocked.
    assert.equal((await ok('B', 'inspect', { request_id: 'lost/' + action })).run_id, receipt.run_id);
    await dsh.request(action, action === 'configure' ? { config: { enableSkill: false } } : { key: 'lost-' + action });
    const interrupted = await pendingCall;
    assert.ok(interrupted.result.isError || interrupted.result.value.ok === false, JSON.stringify(interrupted));
    await dsh.request('configure', { config: { runcradleExecutable: cli } });
    assert.equal((await ok('B', 'inspect', { request_id: 'lost/' + action })).final, false);
    await running('B', receipt.run_id);
    await ok('B', 'stop', { run_id: receipt.run_id }); await terminal(receipt.run_id);
    await record(`lost-reply-${action}`, { run_id: receipt.run_id, request_id: 'lost/' + action, independentQuery: true, acceptedRunSurvived: true });
  }
  // Plugin configuration cycles keep the service-owned run alive.
  const surviving = await ok('B', 'run', { request_id: 'survive-unload', file: 'flow.yaml', targets: ['long'] });
  await running('B', surviving.run_id);
  for (let i = 0; i < 3; i++) {
    assert.equal((await dsh.request('configure', { config: { enableSkill: false } })).length, 1);
    assert.equal((await dsh.request('configure', { config: { runcradleExecutable: cli } })).length, 11);
  }
  assert.equal((await ok('B', 'inspect', { run_id: surviving.run_id })).final, false);
  await dsh.stop();
  const afterExit = JSON.parse((await work.run('server-after-dsh-exit', cli, ['workflow', 'inspect', '--workspace', work.cwd, '--run-id', surviving.run_id])).stdout);
  assert.equal(afterExit.final, false);
  await record('plugin-and-dsh-exit', { run_id: surviving.run_id, stillRunning: true });
  await work.run('driver-owned-stop', cli, ['workflow', 'stop', '--workspace', work.cwd, '--run-id', surviving.run_id]);
  await server.stop();
  const offlineDsh = await start('offline-dsh', 'dsh', ['--profile', profile, '--patch', overlay], '@@RUNCRADLE_RUNTIME@@');
  await offlineDsh.request('create', { key: 'offline', cwd: work.cwd });
  for (const [name, args, success] of [['inspect', { run_id: first.run_id }, true], ['runs', {}, false], ['stop', { run_id: first.run_id }, false]]) {
    const got = await offlineDsh.request('call', { key: 'offline', name: 'runcradle_' + name, args });
    assert.equal(got.result.isError, false); assert.equal(got.result.value.ok, success, JSON.stringify(got));
    await record(`offline-${name}`, got.result.value);
  }
  await offlineDsh.stop();
  const retainedCalls = (await readdir(work.cwd)).filter((v) => v.startsWith('.dsh-runcradle-call-'));
  assert.equal(retainedCalls.length, 3, 'Only the three deliberately interrupted workflow write evidence directories remain');
  for (const directory of retainedCalls) {
    const input = JSON.parse(await readFile(join(work.cwd, directory, 'input.json'), 'utf8'));
    assert.match(input.source, /# workflow-held-(cancel|dispose|configure)/);
  }
  const clientCalls = (await readFile(join(work.dir, 'client-calls.jsonl'), 'utf8')).trim().split('\n').map(JSON.parse);
  assert.equal(clientCalls.filter((args) => args[0] === 'api' && args[args.indexOf('--path') + 1] === '/api/v1/workflows').length, 3, 'Interrupted creates are never retried');
  await record('workflow-retained-write-evidence', { directories: retainedCalls, deliberatelyRetained: true, writes: 3 });
  await record('complete', { realAgents: true, realEngine: true, crossSession: true, externalServiceOwnership: true, manualAcceptance: false });
  console.log('PASS: workflow CRUD/CAS/history/drafts/fixed-version runs, real DSH Agents, permissions, cross-session execution, outcomes, idempotency, capacity, unload and offline inspection');
} catch (error) {
  work.records.push({ label: 'failure', passed: false, error: error.stack }); await work.save(); throw error;
} finally {
  try { for (const child of children.toReversed()) await child.stop(); }
  finally { work.owner.close(); }
}
