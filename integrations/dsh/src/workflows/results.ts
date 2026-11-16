import { hex, object, parseDraft, text, type Draft, type DraftArtifact, type ObjectValue, type WorkflowSaveInput, type WorkflowDeleteInput } from './contract.js';

const PROJECTION_BYTES = 48 * 1024;
export function summaryText(value: unknown, name: string, max: number, flags: ObjectValue): string {
  text(value, name, 32 * 1024 * 1024, true);
  // Budget encoded JSON too: control characters can expand to six bytes each.
  let result = '', size = 0;
  for (const char of value) { const cost = Buffer.byteLength(JSON.stringify(char)) - 2; if (size + cost > max) break; result += char; size += cost; }
  if (result !== value) flags.details_truncated = true;
  return result;
}
export function documentDraft(body: unknown): Draft {
  const value = object(body);
  const selected: ObjectValue = {};
  for (const key of ['format', 'source', 'ui_source', 'entry', 'ui_path']) if (key in value) selected[key] = value[key];
  return parseDraft(selected);
}
function record(raw: unknown, history: boolean): ObjectValue {
  const row = object(raw); hex(row.workflow_id, 'workflow_id'); hex(row.revision, 'head_revision');
  text(row.title, 'title', 32 * 1024 * 1024, true); text(row.updated_at, 'updated_at', 128);
  if (history) {
    if (!Array.isArray(row.revisions) || row.revisions.length < 1 || row.revisions.length > 4096) throw new Error('Invalid workflow version history');
    for (const revision of row.revisions) hex(revision, 'history revision');
    if (new Set(row.revisions).size !== row.revisions.length || !row.revisions.includes(row.revision)) throw new Error('Invalid head or duplicate version history');
  }
  return row;
}
export function projectWorkflowPage(body: unknown, offset: number, limit: number): ObjectValue {
  const value = object(body);
  if (!Array.isArray(value.workflows) || value.workflows.length > limit || value.next_offset !== offset + value.workflows.length || typeof value.has_more !== 'boolean') throw new Error('Invalid workflow page');
  const result: ObjectValue = { workflows: [], next_offset: offset, has_more: value.has_more, details_truncated: false };
  const rows = result.workflows as ObjectValue[];
  for (const raw of value.workflows) {
    const row = record(raw, false);
    const item: ObjectValue = { workflow_id: row.workflow_id, head_revision: row.revision, updated_at: row.updated_at, title: summaryText(row.title, 'title', 1024, result) };
    rows.push(item); result.next_offset = offset + rows.length;
    if (Buffer.byteLength(JSON.stringify(result)) > PROJECTION_BYTES) { rows.pop(); result.next_offset = offset + rows.length; result.has_more = true; result.details_truncated = true; break; }
  }
  return result;
}
export function projectWorkflowDocument(body: unknown, artifact?: DraftArtifact, versions = { offset: 0, limit: 20 }): ObjectValue {
  const value = object(body); documentDraft(value); hex(value.revision, 'document_revision');
  if (typeof value.valid !== 'boolean' || typeof value.run_supported !== 'boolean' || !(value.issues === null || Array.isArray(value.issues))) throw new Error('Invalid workflow document validation fields');
  const issues = value.issues ?? [];
  const result: ObjectValue = { document_revision: value.revision, valid: value.valid, run_supported: value.run_supported, issue_count: issues.length, issues: [], issues_truncated: issues.length > 20, details_truncated: false };
  result.execution_note = summaryText(value.execution_note, 'execution_note', 1024, result);
  for (const raw of issues.slice(0, 20)) {
    const issue = object(raw), summary: ObjectValue = {};
    for (const key of ['severity', 'code', 'path', 'message']) if (key in issue) summary[key] = summaryText(issue[key], `issue ${key}`, key === 'message' ? 512 : 128, result);
    for (const key of ['line', 'column']) if (key in issue) { if (!Number.isSafeInteger(issue[key]) || Number(issue[key]) < 0) throw new Error('Invalid issue location'); summary[key] = issue[key]; }
    (result.issues as ObjectValue[]).push(summary);
  }
  if (value.workflow !== undefined) {
    const workflow = record(value.workflow, true), revisions = workflow.revisions as string[];
    if (!revisions.includes(value.revision)) throw new Error('Document revision missing from history');
    Object.assign(result, { workflow_id: workflow.workflow_id, head_revision: workflow.revision, version_count: revisions.length,
      versions: revisions.toReversed().slice(versions.offset, versions.offset + versions.limit), versions_next_offset: Math.min(versions.offset + versions.limit, Math.max(versions.offset, revisions.length)), versions_has_more: versions.offset + versions.limit < revisions.length });
  }
  if (artifact) Object.assign(result, artifact);
  return result;
}
export function projectWorkflowSave(body: unknown, input: WorkflowSaveInput): ObjectValue {
  const value = object(body);
  const result = projectWorkflowDocument(value);
  const workflow = record(value.workflow, true);
  if (!value.valid || value.revision !== input.draft_revision || workflow.revision !== input.draft_revision || (input.workflow_id !== undefined && workflow.workflow_id !== input.workflow_id)) throw new Error('Save response identity differs from requested workflow/revision');
  const saved: ObjectValue = { workflow_id: workflow.workflow_id, saved_revision: value.revision, head_revision: workflow.revision, version_count: (workflow.revisions as string[]).length, mutation: 'confirmed' };
  for (const key of ['valid', 'run_supported', 'execution_note', 'issue_count', 'issues', 'issues_truncated', 'details_truncated']) saved[key] = result[key];
  return saved;
}

export function projectWorkflowDelete(body: unknown, input: WorkflowDeleteInput): ObjectValue {
  const value = object(body);
  if (value.deleted !== true || value.workflow_id !== input.workflow_id || value.deleted_revision !== input.expected_revision) throw new Error('Delete response identity differs from requested workflow/revision');
  return { workflow_id: value.workflow_id, deleted_revision: value.deleted_revision, deleted: true, mutation: 'confirmed' };
}
