import assert from 'node:assert/strict';
import test from 'node:test';
import { commandFor, parseInput, parseReceipt, projectRuns, checkProtocol, outcome } from '../lib/runs/contract.js';

test('run builds a detached argv without shell interpretation and preserves request identity', () => {
  const input = parseInput('run', { request_id: ' 请求 $(touch nope) ', targets: ['检查; exit 1'], file: 'a b.yaml' });
  assert.deepEqual(commandFor('run', input, '/tmp/工作区'), ['workflow', 'run', '--workspace', '/tmp/工作区', '--request-id', ' 请求 $(touch nope) ', '--detach', '--timeout-ms', '300000', '--file', 'a b.yaml', '--target', '检查; exit 1']);
  assert.deepEqual(commandFor('run', parseInput('run', { request_id: 'r', targets: ['a'], workflow_id: 'wf', revision: 'rev', retain_evidence: true }), '/w').slice(-7), ['--workflow-id', 'wf', '--revision', 'rev', '--target', 'a', '--retain-evidence']);
});

test('invalid identities, source combinations, timeout and unknown parameters fail before dispatch', () => {
  const base = { request_id: 'r', targets: ['a'], file: 'f' };
  for (const change of [{ request_id: '' }, { request_id: '中'.repeat(43) }, { request_id: 'x\n' }, { targets: [] }, { targets: [''] }, { targets: ['a', 'a'] }, { timeout_ms: 0 }, { timeout_ms: 3600001 }, { timeout_ms: 1.5 }, { workflow_id: 'wf', revision: 'r' }, { revision: 'r' }, { detach: false }]) {
    assert.throws(() => parseInput('run', { ...base, ...change }));
  }
  assert.throws(() => parseInput('run', { request_id: 'r', targets: ['a'], workflow_id: 'wf' }));
  assert.throws(() => parseInput('run', { request_id: 'r', targets: ['a'], workflow_id: 'wf', revision: 'rev', ui: 'ui.json' }));
});

test('inspect selects one identity and one view including explicit zero offsets', () => {
  assert.deepEqual(commandFor('inspect', parseInput('inspect', { request_id: 'r', events_after: '18446744073709551615' }), '/w'), ['workflow', 'inspect', '--workspace', '/w', '--request-id', 'r', '--limit', '20', '--events-after', '18446744073709551615']);
  for (const input of [{}, { run_id: 'a'.repeat(32), request_id: 'r' }, { request_id: 'r', node: 'a', nodes_after: 0 }, { request_id: 'r', nodes_after: 0, events_after: '0' }, { request_id: 'r', events_after: '18446744073709551616' }, { request_id: 'r', events_after: 1 }, { request_id: 'r', limit: 101 }, { request_id: 'r', nodes_after: -1 }]) assert.throws(() => parseInput('inspect', input));
  assert.throws(() => parseInput('stop', { run_id: '../x' }));
});

test('receipt parsing preserves only protocol uint64 cursors without changing user declaration fields', () => {
  const value = parseReceipt('{"status":"in_progress","next_seq":18446744073709551615,"events":[{"seq":9007199254740993}],"declaration":{"seq":12,"next_seq":13}}');
  assert.equal(value.next_seq, '18446744073709551615');
  assert.equal(value.events[0].seq, '9007199254740993');
  assert.deepEqual(value.declaration, { seq: 12, next_seq: 13 });
  for (const raw of ['', 'noise\n{}', '{}\n{}', '[]', 'null', '{"next_seq":-1}', '{"next_seq":1e2}']) assert.throws(() => parseReceipt(raw));
});

test('protocol mismatches and missing detached execution capabilities are rejected', () => {
  const protocol = { api_version: 'v1', contract_version: '1.0.0', features: ['idempotent_run', 'detached_submit', 'offline_inspect', 'explicit_stop', 'event_resume'] };
  checkProtocol(protocol, 'run');
  assert.throws(() => checkProtocol({ ...protocol, contract_version: '2.0.0' }, 'run'));
  assert.throws(() => checkProtocol({ ...protocol, features: ['idempotent_run'] }, 'run'));
});

test('bounded run pages do not leak inputs or skip records when the output budget truncates a page', () => {
  const rows = Array.from({ length: 100 }, (_, i) => ({ run_id: i.toString(16).padStart(32, '0'), revision: 'r'.repeat(64), reason: 'x'.repeat(5000), values: { secret: 'never-show' }, nodes: [{ name: 'not-in-list' }], final: false, task_result: 'pending' }));
  const page = projectRuns({ runs: rows, has_more: false, next_offset: 110 }, 10, 100, 4096);
  assert.ok(page.runs.length > 0 && page.runs.length < 100);
  assert.equal(page.next_offset, 10 + page.runs.length);
  assert.equal(page.has_more, true);
  assert.equal(page.details_truncated, true);
  assert.ok(Buffer.byteLength(JSON.stringify(page)) <= 4096);
  assert.ok(!JSON.stringify(page).includes('never-show'));
  assert.ok(!JSON.stringify(page).includes('not-in-list'));
  assert.throws(() => projectRuns({ runs: rows, has_more: false, next_offset: 3 }, 10, 100));
});

test('operation success stays independent of historical failure, acceptance and cleanup', () => {
  assert.equal(outcome('inspect', '/w', 0, { status: 'failed', final: true, task_result: 'failed' }).ok, true);
  const accepted = outcome('run', '/w', 0, { status: 'accepted', accepted: true, run_id: 'a'.repeat(32), request_id: 'r', task_result: 'unknown' });
  assert.equal(accepted.result.task_result, 'unknown');
  assert.equal(outcome('stop', '/w', 0, { status: 'stop_requested', final: false, cleanup: 'unknown' }).result.final, false);
  assert.equal(outcome('run', '/w', 2, { status: 'not_started', reason: 'not_submitted' }).ok, false);
  assert.throws(() => outcome('run', '/w', 0, {}));
});

test('invalid UTF-16 cannot silently replace a caller request identity in UTF-8', () => {
  assert.throws(() => parseInput('run', { request_id: 'bad\ud800', file: 'f', targets: ['a'] }), /UTF|Unicode/);
  assert.throws(() => outcome('run', '/w', 0, { status: 'accepted', accepted: true, run_id: 'bad', request_id: 'r' }));
});

test('successful receipts must match the original request or run identity', () => {
  assert.throws(() => outcome('run', '/w', 0, { status: 'accepted', accepted: true, run_id: 'a'.repeat(32), request_id: 'different' }, { request_id: 'original' }), /identity/);
  assert.throws(() => outcome('stop', '/w', 0, { status: 'stop_requested', run_id: 'b'.repeat(32) }, { run_id: 'a'.repeat(32) }), /identity/);
  assert.throws(() => outcome('inspect', '/w', 0, { status: 'error' }), /status/);
});
