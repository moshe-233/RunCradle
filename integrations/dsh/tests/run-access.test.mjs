import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, symlink, rm, rename, readdir } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { authorize } from '../lib/runs/access.js';
import { parseInput } from '../lib/runs/contract.js';

async function fixture(t, mode = 'workspace-write') {
  const root = await mkdtemp(join(tmpdir(), 'run-access-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const workspace = join(root, 'workspace');
  const outside = join(root, 'outside');
  await mkdir(workspace); await mkdir(outside);
  await writeFile(join(workspace, 'flow.yaml'), 'one');
  let directory = workspace;
  const calls = [];
  const policy = { mode, workspaceRoot: workspace };
  const services = {
    workingDirectory: { get() { return directory; } },
    sandboxPolicy: { resolve() { return policy; } },
    approval: { async request(request) { calls.push(request); return 'allowed-once'; } },
  };
  const ctx = { get(name) { return services[name]; }, workingDirectory: services.workingDirectory };
  const exec = { agent: { session: {} }, callId: 'call', signal: new AbortController().signal };
  return { root, workspace, outside, ctx, exec, services, policy, calls, setDirectory(value) { directory = value; } };
}

test('permission classification follows actual side effects and never grants from cwd alone', async (t) => {
  const f = await fixture(t);
  await authorize(f.ctx, f.exec, 'inspect', parseInput('inspect', { request_id: 'r' }));
  assert.equal(f.calls.length, 0);
  await authorize(f.ctx, f.exec, 'run', parseInput('run', { request_id: 'r', file: 'flow.yaml', targets: ['a'] }));
  assert.equal(f.calls.length, 1);
  f.policy.mode = 'read-only';
  await authorize(f.ctx, f.exec, 'inspect', parseInput('inspect', { request_id: 'r' }));
  assert.equal(f.calls.length, 2);
  f.policy.mode = 'workspace-write'; f.setDirectory(f.outside);
  await authorize(f.ctx, f.exec, 'runs', parseInput('runs', {}));
  assert.equal(f.calls.length, 3);
  assert.equal(f.calls[2].agent, f.exec.agent);
  assert.deepEqual(await readdir(f.outside), []);
});

test('full access does not ask and missing services fail only when needed', async (t) => {
  const f = await fixture(t, 'danger-full-access');
  delete f.services.approval;
  await authorize(f.ctx, f.exec, 'stop', parseInput('stop', { run_id: 'a'.repeat(32) }));
  f.policy.mode = 'workspace-write';
  await assert.rejects(authorize(f.ctx, f.exec, 'stop', parseInput('stop', { run_id: 'a'.repeat(32) })), /approval/i);
  delete f.services.sandboxPolicy;
  await assert.rejects(authorize(f.ctx, f.exec, 'runs', parseInput('runs', {})), /policy/i);
  await assert.rejects(authorize(f.ctx, { signal: f.exec.signal }, 'runs', parseInput('runs', {})), /Agent/i);
});

test('rejected, unavailable and cancelled approvals leave the workspace untouched', async (t) => {
  const f = await fixture(t, 'read-only');
  for (const answer of ['rejected', 'unavailable', 'cancelled', undefined]) {
    f.services.approval.request = async () => answer;
    await assert.rejects(authorize(f.ctx, f.exec, 'runs', parseInput('runs', {})), /approval/i);
    assert.deepEqual(await readdir(f.workspace), ['flow.yaml']);
  }
});

test('canonical aliases share authorization, file escapes fail, and approval-time replacements are rejected', async (t) => {
  const f = await fixture(t);
  const alias = join(f.root, 'alias'); await symlink(f.workspace, alias);
  const resolved = await authorize(f.ctx, f.exec, 'runs', parseInput('runs', { workspace: alias }));
  assert.equal(resolved.workspace, f.workspace); assert.equal(f.calls.length, 0);
  await writeFile(join(f.outside, 'secret'), 'outside');
  await symlink(join(f.outside, 'secret'), join(f.workspace, 'escape'));
  await assert.rejects(authorize(f.ctx, f.exec, 'run', parseInput('run', { request_id: 'r', targets: ['a'], file: 'escape' })), /workspace/i);
  f.services.approval.request = async () => {
    await rename(join(f.workspace, 'flow.yaml'), join(f.workspace, 'old.yaml'));
    await writeFile(join(f.workspace, 'flow.yaml'), 'two');
    return 'allowed-once';
  };
  await assert.rejects(authorize(f.ctx, f.exec, 'run', parseInput('run', { request_id: 'r', targets: ['a'], file: 'flow.yaml' })), /changed/i);
});

test('workspace replacement and policy narrowing after approval cannot reuse an earlier grant', async (t) => {
  const f = await fixture(t);
  f.services.approval.request = async () => {
    await rename(f.workspace, f.workspace + '-old'); await mkdir(f.workspace);
    return 'allowed-once';
  };
  await assert.rejects(authorize(f.ctx, f.exec, 'stop', parseInput('stop', { run_id: 'a'.repeat(32) })), /changed/i);
});

test('fresh context service wrappers are allowed but a changed effective policy is rejected', async (t) => {
  const f = await fixture(t, 'danger-full-access');
  f.ctx.get = (name) => name === 'sandboxPolicy' ? { resolve: () => f.policy } : f.services[name];
  const access = await authorize(f.ctx, f.exec, 'runs', parseInput('runs', {}));
  await access.recheck();
  f.policy.mode = 'read-only';
  await assert.rejects(access.recheck(), /policy changed/);
});
