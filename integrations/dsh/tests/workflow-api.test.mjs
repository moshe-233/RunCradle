import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtemp, readdir, readFile, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { fixtureRunner, document } from './fixtures/workflow-cli.mjs';
const api = await import('../lib/shared/api.js').catch(() => ({}));
async function setup(t) { const workspace = await mkdtemp(join(tmpdir(), 'workflow-api-')); t.after(() => rm(workspace, { recursive: true, force: true })); return { workspace, async recheck() {} }; }
const signal = () => new AbortController().signal;
const request = { method: 'POST', path: '/api/v1/workflows', bodyText: '{"format":"yaml","source":"x"}', mutating: true };
test('POST PUT DELETE framing and bounded authenticated response', async (t) => {
  assert.equal(typeof api.requestJSON, 'function');
  const a = await setup(t);
  for (const method of ['POST', 'PUT', 'DELETE']) {
    const runner = fixtureRunner(); const response = await api.requestJSON(runner, a, { ...request, method }, signal());
    assert.deepEqual(response.body, document); assert.equal(response.httpStatus, 201);
    assert.ok(runner.calls[0].includes('--input')); assert.equal(runner.calls[0][runner.calls[0].indexOf('--method') + 1], method);
    assert.deepEqual(await readdir(a.workspace), []);
  }
  for (const mode of ['digest', 'oversize', 'status', 'non-json', 'invalid-utf8']) await assert.rejects(api.requestJSON(fixtureRunner(mode), a, request, signal()), (e) => e.dispatched === true && e.body === undefined);
});
test('create response loss is unknown without retry; pre-dispatch failure differs', async (t) => {
  assert.equal(typeof api.requestJSON, 'function');
  const a = await setup(t); const runner = fixtureRunner('lost');
  await assert.rejects(api.requestJSON(runner, a, request, signal()), (e) => e.dispatched === true && Boolean(e.temporaryPath)); assert.equal(runner.calls.length, 1);
  const before = await readdir(a.workspace); a.recheck = async () => { throw new Error('denied'); };
  await assert.rejects(api.requestJSON(runner, a, request, signal()), (e) => e.dispatched === false); assert.equal(runner.calls.length, 1); assert.deepEqual(await readdir(a.workspace), before);
});
test('save success survives cleanup failure without deleting foreign files', async (t) => {
  assert.equal(typeof api.requestJSON, 'function');
  const a = await setup(t);
  await assert.rejects(api.requestJSON(fixtureRunner('cleanup'), a, request, signal()), (e) => e.code === 'cleanup_uncertain' && e.httpStatus === 201 && e.body.workflow.workflow_id === document.workflow.workflow_id);
  const [dir] = await readdir(a.workspace); assert.equal(await readFile(join(a.workspace, dir, 'foreign'), 'utf8'), 'keep');
});
test('CAS conflict and published-record HTTP failures preserve verified Problem status', async (t) => {
  assert.equal(typeof api.requestJSON, 'function');
  const a = await setup(t);
  for (const status of [409, 400, 404, 500]) await assert.rejects(api.requestJSON(fixtureRunner(status), a, request, signal()), (e) => e.dispatched && e.httpStatus === status && e.body.status === status && (status !== 409 || e.body.code === 'conflict'));
});
test('directory substitution retains replacement and rejects receipt', async (t) => {
  assert.equal(typeof api.requestJSON, 'function');
  const a = await setup(t);
  await assert.rejects(api.requestJSON(fixtureRunner('directory'), a, request, signal()), /identity changed/i);
  const names = await readdir(a.workspace); const replacement = names.find((v) => !v.endsWith('-moved')); assert.equal(await readFile(join(a.workspace, replacement, 'response.json'), 'utf8'), 'replacement');
});
