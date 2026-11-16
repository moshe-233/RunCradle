import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtemp, readFile, readdir, writeFile, rm, symlink, mkdir } from 'node:fs/promises';
import { join, dirname } from 'node:path';
import { tmpdir } from 'node:os';
import { createHash } from 'node:crypto';
import { registerWorkflowTools } from '../lib/workflows/tools.js';
import { document, id, revision } from './fixtures/workflow-cli.mjs';
const protocol = { api_version: 'v1', contract_version: '1.0.0', features: ['workflow_library', 'cas_save', 'structured_edit', 'workflow_delete'] };
const draft = { format: document.format, source: document.source, entry: document.entry };
async function fixture(t, options = {}) {
  const root = await mkdtemp(join(tmpdir(), 'workflow-tools-')); t.after(() => rm(root, { recursive: true, force: true }));
  const workspace = options.escaped ? join(root, ...Array.from({ length: 15 }, () => '\u0001'.repeat(230))) : root;
  if (options.escaped) await mkdir(workspace, { recursive: true });
  const tools = new Map(), calls = [], cleaners = []; let approvals = 0;
  const policy = { mode: options.mode ?? 'danger-full-access', workspaceRoot: workspace };
  const services = { workingDirectory: { get: () => workspace }, sandboxPolicy: { resolve: () => policy }, approval: { async request() { approvals++; return options.approval ?? 'rejected'; } } };
  const provider = {
    async resolveExecutable(path) { return path; },
    spawn(spec) {
      calls.push(spec.argv.slice(1)); let stdout;
      const done = (async () => {
        const args = spec.argv; if (args[2] === 'describe') { stdout = JSON.stringify(options.noDelete ? {...protocol, features: protocol.features.filter(v=>v !== 'workflow_delete')} : protocol); return { exitCode: 0 }; }
        const path = args[args.indexOf('--path') + 1]; const out = args[args.indexOf('--out') + 1];
        const input = args.includes('--input') ? JSON.parse(await readFile(join(workspace, args[args.indexOf('--input') + 1]), 'utf8')) : undefined;
        let body = path === '/api/v1/protocol' ? protocol : path.startsWith('/api/v1/workflows?') ? { workflows: options.largePage ? Array.from({ length: 100 }, () => ({ ...document.workflow, title: 'x'.repeat(1024) })) : [document.workflow], has_more: false, next_offset: options.largePage ? 100 : 1 } : structuredClone(document);
        if (path === '/api/v1/imports' && options.substitutedImport) {
          await writeFile(join(workspace, 'flow.yaml'), 'sensitive substituted content');
          body.source = 'sensitive substituted content';
        }
        if (options.largeIssues && path.startsWith('/api/v1/workflows/')) body.issues = Array.from({ length: 100 }, () => ({ severity: 'error', code: 'E', path: 'p', message: 'x'.repeat(2048) }));
        const deleting = args.includes('DELETE');
        if (deleting) body = {workflow_id:id, deleted_revision:revision, deleted:true};
        let status = path === '/api/v1/workflows' ? 201 : 200;
        const saving = path === '/api/v1/workflows' || (path === '/api/v1/workflows/' + id && args.includes('PUT'));
        if (!saving && !deleting && path !== '/api/v1/protocol' && options.invalid) body.valid = false;
        if (saving && options.saveStatus) { status = options.saveStatus; body = { status, code: status === 409 ? 'conflict' : 'invalid_request', error: 'write failure', next_action: 'inspect' }; }
        if (path === '/api/v1/validate' && input.edit && options.editRejected) { status = 400; body = { status, code: 'invalid_request', error: 'edit rejected', next_action: 'repair' }; }
        if ((saving || deleting) && options.lost) throw new Error('response lost');
        if (deleting && options.mismatch) body.deleted_revision = 'b'.repeat(64);
        if (deleting && options.deleteStatus) { status=options.deleteStatus; body={status,code:status===409?'conflict':status===404?'not_found':'internal_error',error:'delete failure',next_action:'inspect'}; }
        if (saving && options.mismatch) body.revision = 'b'.repeat(64);
        if (saving) options.onSave?.(input);
        if (deleting) options.onDelete?.(input);
        const bytes = Buffer.from(JSON.stringify(body)); await writeFile(join(workspace, out), bytes, { flag: 'wx' });
        if ((saving || deleting) && options.cleanup) await writeFile(join(dirname(join(workspace, out)), 'foreign'), 'keep');
        stdout = JSON.stringify({ status: status < 400 ? 'ok' : 'error', contract_version: '1.0.0', content_type: 'application/json', http_status: status, output: out, bytes: bytes.length, sha256: createHash('sha256').update(bytes).digest('hex') });
        return { exitCode: status < 400 ? 0 : 2 };
      })();
      return { done, terminate() {}, async waitForExit() { await done.catch(() => {}); return true; }, collected: { stdout: { readFrom: () => ({ text: stdout, lossy: false }) } } };
    },
  };
  const ctx = { get: (name) => services[name], workingDirectory: services.workingDirectory, subprocess: provider, tools: { register: (tool) => tools.set(tool.name, tool) }, effect(fn) { cleaners.push(fn()); } };
  registerWorkflowTools(ctx, '/fixture-cli'); t.after(async () => { for (const close of cleaners) await close(); });
  const exec = { agent: { session: {}, ctx: { effect() { return async () => {}; } } }, callId: 'call', signal: new AbortController().signal };
  return { workspace, calls, approvals: () => approvals, async call(name, args) { const tool=tools.get('runcradle_' + name); assert.ok(tool, `missing tool: ${name}`); const result = await tool.execute(args, exec); assert.ok(Buffer.byteLength(JSON.stringify(result)) <= 65536); return result; } };
}
test('get/import/validate publish complete drafts; invalid raw draft remains editable', async (t) => {
  const f = await fixture(t, { invalid: true }); await writeFile(join(f.workspace, 'flow.yaml'), document.source);
  for (const [name, args] of [['workflow_get', { workflow_id: id }], ['workflow_import', { file: 'flow.yaml' }], ['workflow_validate', { draft }]]) {
    const result = await f.call(name, { ...args, output_file: name + '.json' }); assert.equal(result.ok, true, JSON.stringify(result)); assert.equal(result.result.valid, false);
    assert.equal(JSON.parse(await readFile(join(f.workspace, result.result.draft_file))).source, document.source);
    assert.equal(result.result.source, undefined);
  }
});
test('save prevalidates then submits the same frozen Draft; invalid or stale draft never writes', async (t) => {
  const f = await fixture(t, { onSave(input) { assert.deepEqual(input, draft); } });
  const saved = await f.call('workflow_save', { draft, draft_revision: revision }); assert.equal(saved.ok, true, JSON.stringify(saved)); assert.equal(saved.result.mutation, 'confirmed');
  assert.equal(f.calls.filter((args) => args.includes('/api/v1/workflows')).length, 1);
  const before = f.calls.length; const bad = await f.call('workflow_save', { draft, draft_revision: 'c'.repeat(64) }); assert.equal(bad.result.mutation, 'not_attempted');
  assert.equal(f.calls.slice(before).filter((args) => args.includes('/api/v1/workflows')).length, 0);
});
test('save conflict, response loss, protocol mismatch, HTTP errors and cleanup preserve mutation facts', async (t) => {
  for (const [options, mutation] of [[{ saveStatus: 409 }, 'not_saved'], [{ saveStatus: 400 }, 'unknown'], [{ saveStatus: 404 }, 'unknown'], [{ saveStatus: 500 }, 'unknown'], [{ lost: true }, 'unknown'], [{ mismatch: true }, 'unknown'], [{ cleanup: true }, 'confirmed']]) {
    const f = await fixture(t, options); const got = await f.call('workflow_save', { draft, draft_revision: revision });
    assert.equal(got.ok, false); assert.equal(got.result.mutation, mutation, JSON.stringify(got)); assert.equal(f.calls.filter((args) => args.includes('/api/v1/workflows')).length, 1);
    if (mutation === 'confirmed') { assert.equal(got.result.workflow_id, id); assert.equal(got.result.saved_revision, revision); assert.equal(got.code, 'cleanup_uncertain'); }
  }
});
test('approval denial and private aliases cause zero process/file effects', async (t) => {
  const f = await fixture(t, { mode: 'workspace-write' });
  const denied = await f.call('workflow_save', { draft, draft_revision: revision }); assert.equal(denied.ok, false); assert.equal(f.approvals(), 1); assert.equal(f.calls.length, 0); assert.deepEqual(await readdir(f.workspace), []);
  await mkdir(join(f.workspace, '.runcradle')); await writeFile(join(f.workspace, '.runcradle/secret'), 'secret'); await symlink('.runcradle/secret', join(f.workspace, 'alias'));
  const privateInput = await f.call('workflow_import', { file: 'alias', output_file: 'out' }); assert.equal(privateInput.ok, false); assert.equal(f.calls.length, 0);
});
test('rejected structured edit and occupied output do not publish an artifact', async (t) => {
  const f = await fixture(t, { editRejected: true });
  const rejected = await f.call('workflow_validate', { draft, output_file: 'new', edit_json: '{"process":"x","field":"command","value":"true"}' }); assert.equal(rejected.ok, false); assert.deepEqual(await readdir(f.workspace), []);
  await writeFile(join(f.workspace, 'keep'), 'mine'); const before = f.calls.length;
  const occupied = await f.call('workflow_get', { workflow_id: id, output_file: 'keep' }); assert.equal(occupied.ok, false); assert.equal(f.calls.length, before); assert.equal(await readFile(join(f.workspace, 'keep'), 'utf8'), 'mine');
});
test('import rejects a service read substituted after the authorized snapshot', async (t) => {
  const f = await fixture(t, { substitutedImport: true }); await writeFile(join(f.workspace, 'flow.yaml'), draft.source);
  const got = await f.call('workflow_import', { file: 'flow.yaml', output_file: 'import-replacement.json' });
  assert.equal(got.ok, false); assert.equal(got.result.draft_file, undefined);
  assert.ok(!JSON.stringify(got).includes('sensitive substituted content'));
  await assert.rejects(readFile(join(f.workspace, 'import-replacement.json')), { code: 'ENOENT' });
});
test('bounded error projection includes a real escaped workspace and large page', async (t) => {
  const f = await fixture(t, { escaped: true, largePage: true });
  const got = await f.call('workflows', { limit: 100 });
  assert.ok(Buffer.byteLength(JSON.stringify(got)) <= 65536);
  assert.equal(got.ok, false); assert.equal(got.result.details_truncated, true); assert.ok(got.workspace || got.workspace_omitted);
});

test('delete uses CAS, preserves the receipt identity and sends a single write', async t => {
  const f=await fixture(t,{onDelete(input){assert.deepEqual(input,{expected_revision:revision});}});
  const got=await f.call('workflow_delete',{workflow_id:id,expected_revision:revision});
  assert.equal(got.ok,true,JSON.stringify(got));
  assert.deepEqual(got.result,{workflow_id:id,deleted_revision:revision,deleted:true,mutation:'confirmed'});
  const writes=f.calls.filter(args=>args.includes('DELETE'));
  assert.equal(writes.length,1);
  assert.equal(writes[0][writes[0].indexOf('--path')+1],'/api/v1/workflows/'+id);
  assert.deepEqual(await readdir(f.workspace),[]);
});
test('delete failures preserve uncertainty without retry and require restricted approval', async t => {
  for(const [options,mutation] of [[{deleteStatus:409},'not_deleted'],[{deleteStatus:404},'not_deleted'],[{deleteStatus:500},'unknown'],[{lost:true},'unknown'],[{mismatch:true},'unknown'],[{cleanup:true},'confirmed']]) {
    const f=await fixture(t,options);
    const got=await f.call('workflow_delete',{workflow_id:id,expected_revision:revision});
    assert.equal(got.ok,false,JSON.stringify(got)); assert.equal(got.result.mutation,mutation,JSON.stringify(got));
    assert.equal(f.calls.filter(args=>args.includes('DELETE')).length,1);
  }
  const denied=await fixture(t,{mode:'workspace-write'});
  const got=await denied.call('workflow_delete',{workflow_id:id,expected_revision:revision});
  assert.equal(got.ok,false); assert.equal(got.result.mutation,'not_attempted');
  assert.equal(denied.approvals(),1); assert.equal(denied.calls.length,0);
  const old=await fixture(t,{noDelete:true});
  const unsupported=await old.call('workflow_delete',{workflow_id:id,expected_revision:revision});
  assert.equal(unsupported.ok,false); assert.equal(unsupported.result.mutation,'not_attempted');
  assert.equal(old.calls.filter(args=>args.includes('DELETE')).length,0);
});
