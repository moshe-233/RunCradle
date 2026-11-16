import assert from 'node:assert/strict';
import { readFile, writeFile, readdir, symlink, mkdir } from 'node:fs/promises';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { setTimeout as delay } from 'node:timers/promises';
const sha = (raw) => createHash('sha256').update(raw).digest('hex');
const draftInput = (value) => ({ draft_file: value.draft_file, draft_sha256: value.draft_sha256 });

/** Uses only the existing driver's Agents and service; owns no subprocesses. */
export async function exerciseWorkflowRuntime({ call, create, dispose, configure, workspace, record }) {
  const raw = async (agent, name, args) => {
    const got = await call(agent, 'runcradle_' + name, args);
    assert.equal(got.result.isError, false, JSON.stringify(got));
    assert.ok(Buffer.byteLength(JSON.stringify(got.result.value)) <= 65536);
    return got;
  };
  const ok = async (agent, name, args) => { const got = await raw(agent, name, args); assert.equal(got.result.value.ok, true, JSON.stringify(got)); return got.result.value.result; };
  const save = (agent, draft, extra = {}) => ok(agent, 'workflow_save', { ...draftInput(draft), draft_revision: draft.document_revision, ...extra });
  await create('WF-A'); await create('WF-B');
  const native = '# retained comment\r\n#' + 'x'.repeat(70000) + '\r\nprocesses:\r\n  saved:\r\n    command: "true"\r\n  long:\r\n    command: "sleep 90"\r\n';
  const ui = '{"version":1,"title":"Original","inputs":[{"name":"n","label":"N","type":"integer","default":9223372036854775807,"bindings":[{"process":"saved","field":"environment","key":"N"}]}],"presets":[{"id":"large","label":"Large","values":{"n":9223372036854775807}}]}';
  await writeFile(join(workspace, 'managed.yaml'), native); await writeFile(join(workspace, 'managed-ui.json'), ui);
  const before = await ok('WF-A', 'workflows', {});
  const imported = await ok('WF-A', 'workflow_import', { file: 'managed.yaml', ui: 'managed-ui.json', output_file: 'wf-import.json' });
  assert.equal(imported.valid, true); assert.ok(imported.draft_bytes > 65536);
  assert.equal((await ok('WF-A', 'workflows', {})).workflows.length, before.workflows.length);
  const importedDraft = JSON.parse(await readFile(join(workspace, imported.draft_file), 'utf8')); assert.equal(importedDraft.source, native); assert.equal(importedDraft.ui_source, ui);
  const edited = await ok('WF-A', 'workflow_validate', { ...draftInput(imported), workbench_edit_json: '{"action":"update_meta","title":"Managed"}', output_file: 'wf-edited.json' });
  const editedDraft = JSON.parse(await readFile(join(workspace, edited.draft_file), 'utf8'));
  assert.equal(editedDraft.source, native); assert.ok(editedDraft.ui_source.includes('9223372036854775807'));
  const saved = await save('WF-A', edited); assert.equal(saved.mutation, 'confirmed'); const workflow_id = saved.workflow_id;
  await dispose('WF-A');
  const listed = await ok('WF-B', 'workflows', {}); assert.ok(listed.workflows.some((v) => v.workflow_id === workflow_id));
  const head = await ok('WF-B', 'workflow_get', { workflow_id, output_file: 'wf-head.json' }); assert.equal(head.head_revision, saved.saved_revision);
  assert.deepEqual(JSON.parse(await readFile(join(workspace, head.draft_file), 'utf8')), editedDraft);
  assert.equal(sha(await readFile(join(workspace, 'managed.yaml'))), sha(native)); assert.equal(sha(await readFile(join(workspace, 'managed-ui.json'))), sha(ui));
  await record('workflow-import-create-cross-session', { workflow_id, revision: saved.saved_revision, largeDraftBytes: head.draft_bytes, sourceUnchanged: true, int64Preserved: true });

  await create('WF-C');
  const variantA = await ok('WF-B', 'workflow_validate', { ...draftInput(head), workbench_edit_json: '{"action":"update_meta","title":"Variant A"}', output_file: 'wf-a.json' });
  const variantB = await ok('WF-C', 'workflow_validate', { ...draftInput(head), workbench_edit_json: '{"action":"update_meta","title":"Variant B"}', output_file: 'wf-b.json' });
  assert.notEqual(variantA.document_revision, variantB.document_revision); assert.notEqual(variantA.document_revision, head.document_revision); assert.notEqual(variantB.document_revision, head.document_revision);
  const racers = await Promise.all([['WF-B', variantA], ['WF-C', variantB]].map(([agent, d]) => raw(agent, 'workflow_save', { ...draftInput(d), draft_revision: d.document_revision, workflow_id, expected_revision: head.head_revision })));
  assert.deepEqual(racers.map((r) => r.result.value.result.mutation).sort(), ['confirmed', 'not_saved']);
  const winner = racers.find((r) => r.result.value.ok).result.value.result;
  const old = await ok('WF-B', 'workflow_get', { workflow_id, revision: saved.saved_revision, output_file: 'wf-old.json', versions_limit: 1 });
  assert.equal(old.document_revision, saved.saved_revision); assert.equal(old.head_revision, winner.saved_revision); assert.equal(old.versions_has_more, true);
  const restored = await save('WF-B', old, { workflow_id, expected_revision: old.head_revision }); assert.equal(restored.saved_revision, saved.saved_revision); assert.equal(restored.version_count, 2);
  const noop = await save('WF-B', old, { workflow_id, expected_revision: restored.head_revision }); assert.equal(noop.version_count, 2);
  const restoredHead = await ok('WF-B', 'workflow_get', { workflow_id, output_file: 'wf-restored.json' }); assert.equal(restoredHead.head_revision, saved.saved_revision); assert.ok(restoredHead.versions.includes(winner.saved_revision));
  await record('workflow-cas-history-restore', { workflow_id, mutations: racers.map((r) => r.result.value.result.mutation), versionCount: restoredHead.version_count, noOpPreservedHistory: true });

  const invalid = await ok('WF-B', 'workflow_validate', { draft: { format: 'yaml', source: 'processes: [', entry: 'invalid.yaml' }, output_file: 'wf-invalid.json' }); assert.equal(invalid.valid, false);
  const refused = await raw('WF-B', 'workflow_save', { ...draftInput(invalid), draft_revision: invalid.document_revision }); assert.equal(refused.result.value.result.mutation, 'not_attempted');
  const fixed = await ok('WF-B', 'workflow_validate', { draft: { format: 'yaml', source: 'processes:\n  repaired:\n    command: "true"\n', entry: 'invalid.yaml' }, output_file: 'wf-repaired.json' }); assert.equal(fixed.valid, true);
  const nativeEdit = await ok('WF-B', 'workflow_validate', { ...draftInput(fixed), edit_json: '{"process":"repaired","field":"command","value":"echo repaired"}', output_file: 'wf-native-edit.json' }); assert.equal(nativeEdit.valid, true);
  const presetEdit = await ok('WF-B', 'workflow_validate', { ...draftInput(head), workbench_edit_json: '{"action":"save_preset","preset_spec":{"id":"new","label":"New","values":{"n":9223372036854775807}}}', output_file: 'wf-int64-edit.json' });
  assert.ok(JSON.parse(await readFile(join(workspace, presetEdit.draft_file), 'utf8')).ui_source.includes('9223372036854775807'));
  const badEdit = await raw('WF-B', 'workflow_validate', { ...draftInput(head), edit_json: '{"process":"absent","field":"command","value":"true"}', output_file: 'wf-rejected.json' }); assert.equal(badEdit.result.value.ok, false); await assert.rejects(readFile(join(workspace, 'wf-rejected.json')), { code: 'ENOENT' });
  await record('workflow-validation-and-editing', { invalidPreserved: true, invalidSaveRejected: true, rawRepair: true, nativeEdit: true, int64Edit: true, rejectedEditNoOutput: true });

  for (const mode of ['read-only', 'workspace-write']) {
    const key = 'WF-' + mode; await create(key, mode, 'rejected');
    const denied = await raw(key, 'workflow_save', { ...draftInput(head), draft_revision: head.document_revision }); assert.equal(denied.result.value.ok, false); assert.equal(denied.approvals, 1);
    const query = await raw(key, 'workflows', {}); assert.equal(query.result.value.ok, mode === 'workspace-write');
    await create(key + '-allow', mode, 'allowed-once');
    const approved = await raw(key + '-allow', 'workflow_save', { ...draftInput(head), draft_revision: head.document_revision, workflow_id, expected_revision: restored.head_revision }); assert.equal(approved.result.value.ok, true); assert.equal(approved.approvals, 1);
  }
  await mkdir(join(workspace, 'wf-private')); // Alias targets existing core-private metadata without reading it.
  await symlink('.runcradle', join(workspace, 'wf-private-alias'));
  const privateDenied = await raw('WF-B', 'workflow_import', { file: 'wf-private-alias/server/connection.json', output_file: 'wf-secret.json' }); assert.equal(privateDenied.result.value.ok, false);
  const occupied = await raw('WF-B', 'workflow_get', { workflow_id, output_file: head.draft_file }); assert.equal(occupied.result.value.ok, false); assert.equal(sha(await readFile(join(workspace, head.draft_file))), head.draft_sha256);
  await writeFile(join(workspace, 'wf-changed.json'), JSON.stringify(editedDraft)); const oldHash = sha(await readFile(join(workspace, 'wf-changed.json'))); await writeFile(join(workspace, 'wf-changed.json'), JSON.stringify({ ...editedDraft, source: 'processes: {}' }));
  const changed = await raw('WF-B', 'workflow_validate', { draft_file: 'wf-changed.json', draft_sha256: oldHash, output_file: 'wf-no-output.json' }); assert.equal(changed.result.value.ok, false);
  await record('workflow-runtime-permissions-files', { restrictedSaveApproved: true, readOnlyDenied: true, privateAliasDenied: true, occupiedOutputPreserved: true, changedDraftRejected: true });

  const run = await ok('WF-B', 'run', { request_id: 'workflow-managed/fixed', workflow_id, revision: saved.saved_revision, targets: ['long'], timeout_ms: 120000 }); assert.equal(run.accepted, true);
  const runningDeadline = Date.now() + 20000;
  let running = false;
  while (Date.now() < runningDeadline) {
    const state = await ok('WF-B', 'inspect', { run_id: run.run_id });
    assert.equal(state.final, false, JSON.stringify(state));
    if (state.nodes?.some((node) => node.state === 'running')) { running = true; break; }
    await delay(100);
  }
  assert.equal(running, true, 'Saved workflow must actually start before reload/stop verification');
  const updated = await save('WF-B', variantA, { workflow_id, expected_revision: restored.head_revision });
  assert.equal((await ok('WF-C', 'inspect', { run_id: run.run_id })).revision, saved.saved_revision);
  await dispose('WF-B'); await configure(); await create('WF-D');
  const recovered = await ok('WF-D', 'workflow_get', { workflow_id, output_file: 'wf-after-reload.json' }); assert.equal(recovered.head_revision, updated.saved_revision);
  const revalidated = await ok('WF-D', 'workflow_validate', { ...draftInput(head), output_file: 'wf-old-draft-after-reload.json' }); assert.equal(revalidated.document_revision, saved.saved_revision);
  assert.equal((await ok('WF-D', 'inspect', { run_id: run.run_id })).final, false);
  await ok('WF-D', 'stop', { run_id: run.run_id });
  let final; const deadline = Date.now() + 20000;
  while (Date.now() < deadline) { final = await ok('WF-D', 'inspect', { run_id: run.run_id }); if (final.final) break; await delay(100); }
  assert.equal(final.final, true); assert.equal(final.cleanup, 'complete');
  const completedRun = await ok('WF-D', 'run', { request_id: 'workflow-managed/success', workflow_id, revision: updated.saved_revision, targets: ['saved'], preset: 'large' });
  const successDeadline = Date.now() + 20000;
  while (Date.now() < successDeadline) { final = await ok('WF-D', 'inspect', { run_id: completedRun.run_id }); if (final.final) break; await delay(100); }
  assert.equal(final.task_result, 'succeeded'); assert.equal(final.cleanup, 'complete');
  assert.equal((await readdir(workspace)).filter((v) => v.startsWith('.dsh-runcradle-call-')).length, 0);
  await record('workflow-saved-run-reload', { workflow_id, fixedRunRevision: saved.saved_revision, currentHead: updated.saved_revision, crossSessionAndReload: true, persistentDraft: true, successfulRun: completedRun.run_id });
  // Deleting one entry keeps shared archived revisions and its accepted run alive.
  const deletable = await save('WF-D', head);
  const deletingRun = await ok('WF-D', 'run', { request_id: 'workflow-delete/active', workflow_id: deletable.workflow_id, revision: deletable.saved_revision, targets: ['long'], timeout_ms: 120000 });
  const startedDeadline = Date.now() + 20000;
  let deletingRunStarted = false;
  while (Date.now() < startedDeadline) {
    const snapshot = await ok('WF-D', 'inspect', {run_id: deletingRun.run_id});
    assert.equal(snapshot.final, false);
    if (snapshot.nodes?.some(n => n.state === 'running')) { deletingRunStarted = true; break; }
    await delay(100);
  }
  assert.equal(deletingRunStarted, true, 'Wait for the run to start before testing deletion retention');
  const conflictDelete = await raw('WF-D', 'workflow_delete', { workflow_id: deletable.workflow_id, expected_revision: variantA.document_revision });
  assert.equal(conflictDelete.result.value.result.mutation, 'not_deleted');
  const deleted = await ok('WF-D', 'workflow_delete', { workflow_id: deletable.workflow_id, expected_revision: deletable.saved_revision });
  assert.equal(deleted.deleted, true); assert.equal(deleted.deleted_revision, deletable.saved_revision);
  assert.equal((await ok('WF-C', 'workflows', {})).workflows.some(w=>w.workflow_id===deletable.workflow_id),false);
  const missing = await raw('WF-D','workflow_get',{workflow_id:deletable.workflow_id,output_file:'wf-deleted.json'});
  assert.equal(missing.result.value.ok,false);
  const retained = await ok('WF-D','workflow_get',{workflow_id,revision:saved.saved_revision,output_file:'wf-shared-after-delete.json'});
  assert.equal(retained.document_revision,saved.saved_revision);
  assert.equal((await ok('WF-D','inspect',{run_id:deletingRun.run_id})).final,false);
  await ok('WF-D','stop',{run_id:deletingRun.run_id});
  const deletionDeadline=Date.now()+20000;
  while(Date.now()<deletionDeadline){final=await ok('WF-D','inspect',{run_id:deletingRun.run_id}); if(final.final)break; await delay(100);}
  assert.equal(final.final,true); assert.equal(final.cleanup,'complete');
  assert.equal((await ok('WF-D','inspect',{run_id:completedRun.run_id})).task_result,'succeeded');
  assert.equal(sha(await readFile(join(workspace,'managed.yaml'))),sha(native));
  await record('workflow-delete-cas-cross-session-retention',{workflow_id:deletable.workflow_id,deleted,sharedArchiveRetained:true,activeRunRetained:true,existingHistoryRetained:true,sourceRetained:true});

}
