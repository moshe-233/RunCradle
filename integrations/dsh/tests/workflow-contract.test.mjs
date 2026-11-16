import assert from 'node:assert/strict';
import test from 'node:test';
const module = await import('../lib/workflows/contract.js').catch(() => ({}));
const draftModule = await import('../lib/workflows/draft.js').catch(() => ({}));
const hash = 'a'.repeat(64);
const draft = { format: 'yaml', source: '# comment\r\nprocesses: {}\r\n', ui_source: '{"n":9223372036854775807}' };
test('rejects ambiguous draft and save identities', () => {
  assert.equal(typeof module.parseWorkflowInput, 'function');
  const parse = module.parseWorkflowInput;
  for (const args of [{}, { draft }, { draft, draft_revision: hash, draft_file: 'a', draft_sha256: hash }, { draft_file: 'a', draft_revision: hash }, { draft, draft_revision: hash, workflow_id: hash }, { draft, draft_revision: hash, expected_revision: hash }, { draft, draft_revision: hash, edit_json: '{}' }]) assert.throws(() => parse('workflow_save', args));
  assert.equal(parse('workflow_save', { draft, draft_revision: hash }).draft.source, draft.source);
  for (const args of [{ workflow_id: hash }, { workflow_id: hash, output_file: 'a', revision: '' }, { workflow_id: 'A'.repeat(64), output_file: 'a' }]) assert.throws(() => parse('workflow_get', args));
  for (const output_file of ['/tmp/out', '../out', '.git/out', '.runcradle/out', 'a'.repeat(513), 'a/../out']) assert.throws(() => parse('workflow_get', { workflow_id: hash, output_file }));
  assert.deepEqual(parse('workflows', {}), { operation: 'workflows', offset: 0, limit: 20 });
});
test('preserves exact edit integer tokens and source bytes', () => {
  assert.equal(typeof draftModule.buildValidateBody, 'function');
  const build = draftModule.buildValidateBody;
  const edit = '{"action":"save_preset","preset_spec":{"id":"large","label":"Large","values":{"n":9223372036854775807}}}';
  const body = build(draft, undefined, edit);
  assert.ok(body.includes(edit)); assert.equal(JSON.parse(body).source, draft.source); assert.equal(JSON.parse(body).ui_source, draft.ui_source);
  const bind = '{"action":"bind_input","input_spec":{"name":"n","type":"integer","default":9223372036854775807,"bindings":[]}}';
  assert.ok(build(draft, undefined, bind).includes(bind));
  for (const bad of ['{} {}', '[]', '{"action":"unknown"}', '{"action":"update_meta","typo":1}', '{"action":"update_meta","title":"\\ud800"}', '{"action":"save_preset","preset_spec":{"id":"x","typo":1}}']) assert.throws(() => build(draft, undefined, bad));
  assert.throws(() => build(draft, '{}', edit));
  assert.throws(() => build(draft, '{"process":"x","field":"unknown","value":1}'));
  assert.throws(() => build(draft, undefined, '{"action":"update_meta","title":"' + 'x'.repeat(8 * 1024 * 1024) + '"}'));
});
test('validates UTF-8 budgets and freezes a distinct Draft', () => {
  assert.equal(typeof module.parseDraft, 'function');
  const parse = module.parseDraft;
  assert.equal(Buffer.byteLength(parse({ format: 'yaml', source: 'é'.repeat(512 * 1024) }).source), 1024 * 1024);
  for (const value of [{ ...draft, source: 'é'.repeat(512 * 1024) + 'x' }, { ...draft, ui_source: 'x'.repeat(256 * 1024 + 1) }, { ...draft, source: '\ud800' }, { ...draft, revision: hash }, { ...draft, ui_path: '.git/config' }]) assert.throws(() => parse(value));
  const frozen = parse(draft); assert.notEqual(frozen, draft); assert.ok(Object.isFrozen(frozen));
});
test('workbench nested fields match the public dependency and input schemas', () => {
  const build = draftModule.buildValidateBody;
  assert.ok(build(draft, undefined, '{"action":"update_process","process":"b","depends_on":[{"from":"a","condition":"process_completed_successfully"}]}').includes('"from":"a"'));
  assert.throws(() => build(draft, undefined, '{"action":"bind_input","input_spec":{"name":"n","type":"integer","required":true,"bindings":[]}}'));
});

test('delete requires an exact workflow and expected revision without file or draft input', () => {
  const parse = module.parseWorkflowInput;
  for(const args of [{},{workflow_id:hash},{workflow_id:hash,expected_revision:'head'},{workflow_id:hash,expected_revision:hash,file:'flow.yaml'}]) assert.throws(()=>parse('workflow_delete',args));
  assert.equal(parse('workflow_delete',{workflow_id:hash,expected_revision:hash}).expected_revision,hash);
});
