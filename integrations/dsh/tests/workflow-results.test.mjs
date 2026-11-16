import assert from 'node:assert/strict';
import test from 'node:test';
import { document, id, revision } from './fixtures/workflow-cli.mjs';
const api = await import('../lib/workflows/results.js').catch(() => ({}));
const artifact = { draft_file: 'draft.json', draft_bytes: 123, draft_sha256: 'b'.repeat(64) };
test('history document revision differs from current head and pages 4096 first-seen versions', () => {
  assert.equal(typeof api.projectWorkflowDocument, 'function');
  const revisions = Array.from({ length: 4096 }, (_, i) => i.toString(16).padStart(64, '0'));
  const body = { ...document, revision: revisions[0], workflow: { ...document.workflow, revision: revisions[20], revisions } };
  const result = api.projectWorkflowDocument(body, artifact, { offset: 0, limit: 20 });
  assert.equal(result.document_revision, revisions[0]); assert.equal(result.head_revision, revisions[20]); assert.equal(result.version_count, 4096);
  assert.equal(result.versions[0], revisions.at(-1)); assert.equal(result.versions_next_offset, 20); assert.equal(result.versions_has_more, true);
  assert.equal(api.projectWorkflowDocument(body, artifact, { offset: 4090, limit: 100 }).versions.length, 6);
});
test('bounded projections preserve recovery fields and page offsets without leaking text', () => {
  assert.equal(typeof api.projectWorkflowPage, 'function');
  const huge = '界'.repeat(20000);
  const body = { ...document, execution_note: huge, issues: Array.from({ length: 100 }, () => ({ severity: 'error', code: huge, path: huge, message: huge })), display: { private: true } };
  const result = api.projectWorkflowDocument(body, artifact);
  assert.equal(result.issue_count, 100); assert.equal(result.issues.length, 20); assert.equal(result.issues_truncated, true); assert.equal(result.details_truncated, true);
  assert.ok(Buffer.byteLength(JSON.stringify({ operation: 'workflow_get', workspace: '/work', ok: true, result })) <= 65536);
  for (const key of ['source', 'ui_source', 'display']) assert.equal(result[key], undefined);
  const page = api.projectWorkflowPage({ workflows: Array.from({ length: 100 }, () => ({ ...document.workflow, title: huge })), next_offset: 105, has_more: false }, 5, 100);
  assert.equal(page.next_offset, 5 + page.workflows.length); assert.ok(page.workflows.length > 0 && page.workflows.length < 100); assert.equal(page.has_more, true);
  assert.ok(Buffer.byteLength(JSON.stringify(page)) < 65536); assert.equal(page.workflows[0].workflow_id, id);
});
test('confirmed save retains identity and mismatch never becomes confirmed', () => {
  assert.equal(typeof api.projectWorkflowSave, 'function');
  const input = { operation: 'workflow_save', draft: { format: 'yaml', source: 'x' }, draft_revision: revision };
  const result = api.projectWorkflowSave(document, input);
  assert.equal(result.mutation, 'confirmed'); assert.equal(result.saved_revision, revision); assert.equal(result.workflow_id, id);
  for (const body of [{ ...document, revision: 'b'.repeat(64) }, { ...document, valid: false }, { ...document, workflow: { ...document.workflow, revision: 'b'.repeat(64) } }]) assert.throws(() => api.projectWorkflowSave(body, input));
  assert.throws(() => api.projectWorkflowSave(document, { ...input, workflow_id: 'c'.repeat(64), expected_revision: revision }));
  assert.equal(api.projectWorkflowDocument({ ...document, valid: false }, artifact).valid, false);
});
