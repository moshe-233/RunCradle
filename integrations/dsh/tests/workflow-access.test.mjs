import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtemp, mkdir, writeFile, symlink, rm, rename, readdir } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
const api = await import('../lib/shared/access.js').catch(() => ({}));
async function setup(t, mode = 'workspace-write') {
  const workspace = await mkdtemp(join(tmpdir(), 'workflow-access-')); t.after(() => rm(workspace, { recursive: true, force: true }));
  await writeFile(join(workspace, 'draft'), 'not read');
  const policy = { mode, workspaceRoot: workspace }; let calls = 0;
  const services = { workingDirectory: { get: () => workspace }, sandboxPolicy: { resolve: () => policy }, approval: { request: async () => { calls++; return 'allowed-once'; } } };
  const ctx = { get: (name) => services[name], workingDirectory: services.workingDirectory };
  const exec = { agent: { session: {} }, callId: 'call', signal: new AbortController().signal };
  return { workspace, ctx, exec, policy, services, calls: () => calls };
}
const request = (save = false) => ({ toolName: save ? 'runcradle_workflow_save' : 'runcradle_workflow_get', inputFiles: ['draft'], outputFiles: save ? [] : ['out'], requiresRestrictedApproval: save, reason: 'Create/save the specified workflow.', forbidPrivatePaths: true });
test('workflow permission matrix preserves per-operation approval', async (t) => {
  assert.equal(typeof api.resolveAccess, 'function');
  for (const mode of ['danger-full-access', 'workspace-write', 'read-only']) {
    const f = await setup(t, mode); await api.resolveAccess(f.ctx, f.exec, request()); await api.resolveAccess(f.ctx, f.exec, request(true));
    assert.equal(f.calls(), mode === 'danger-full-access' ? 0 : mode === 'workspace-write' ? 1 : 2);
    assert.deepEqual(await readdir(f.workspace), ['draft']);
  }
});
test('denial, missing services and approval-time changes have no file effects', async (t) => {
  assert.equal(typeof api.resolveAccess, 'function');
  const f = await setup(t, 'read-only');
  for (const answer of ['rejected', undefined, 'cancelled']) { f.services.approval.request = async () => answer; await assert.rejects(api.resolveAccess(f.ctx, f.exec, request())); }
  delete f.services.approval; await assert.rejects(api.resolveAccess(f.ctx, f.exec, request()));
  f.policy.mode = 'workspace-write'; await api.resolveAccess(f.ctx, f.exec, request());
  delete f.services.sandboxPolicy; await assert.rejects(api.resolveAccess(f.ctx, f.exec, request()));
  await assert.rejects(api.resolveAccess(f.ctx, { signal: f.exec.signal }, request()));
  assert.deepEqual(await readdir(f.workspace), ['draft']);
  for (const change of ['policy', 'directory', 'input', 'parent']) {
    const g = await setup(t, 'read-only'); await mkdir(join(g.workspace, 'p'));
    g.services.approval.request = async () => {
      if (change === 'policy') g.policy.mode = 'workspace-write';
      if (change === 'directory') g.services.workingDirectory.get = () => '/tmp';
      if (change === 'input') { await rename(join(g.workspace, 'draft'), join(g.workspace, 'old')); await writeFile(join(g.workspace, 'draft'), 'new'); }
      if (change === 'parent') { await rename(join(g.workspace, 'p'), join(g.workspace, 'old')); await mkdir(join(g.workspace, 'p')); }
      return 'allowed-once';
    };
    await assert.rejects(api.resolveAccess(g.ctx, g.exec, { ...request(), outputFiles: ['p/out'] }), /changed/i);
  }
});
test('private aliases and symlink output parents are denied before approval', async (t) => {
  assert.equal(typeof api.resolveAccess, 'function');
  const f = await setup(t, 'read-only'); await mkdir(join(f.workspace, '.git')); await writeFile(join(f.workspace, '.git/config'), 'private');
  await symlink('.git/config', join(f.workspace, 'alias')); await symlink('.', join(f.workspace, 'dir-alias'));
  for (const inputFiles of [['alias'], ['.git/config']]) await assert.rejects(api.resolveAccess(f.ctx, f.exec, { ...request(), inputFiles }));
  await assert.rejects(api.resolveAccess(f.ctx, f.exec, { ...request(), outputFiles: ['dir-alias/out'] }));
  assert.equal(f.calls(), 0);
});
