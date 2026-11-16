import { isAbsolute } from 'node:path';
import { object, type ObjectValue } from '../runs/contract.js';
export { object, type ObjectValue } from '../runs/contract.js';

export const BODY_BYTES = 8 * 1024 * 1024;
export const SOURCE_BYTES = 1024 * 1024;
export const UI_BYTES = 256 * 1024;
export type WorkflowOperation = 'workflows' | 'workflow_get' | 'workflow_import' | 'workflow_validate' | 'workflow_save' | 'workflow_delete';
export type Mutation = 'not_attempted' | 'not_saved' | 'not_deleted' | 'confirmed' | 'unknown';
export type Draft = Readonly<{ format: 'yaml' | 'json'; source: string; ui_source?: string; entry?: string; ui_path?: string }>;
export type DraftInput = { draft: Draft; draft_file?: never; draft_sha256?: never } | { draft?: never; draft_file: string; draft_sha256: string };
export type DraftArtifact = { draft_file: string; draft_bytes: number; draft_sha256: string };
type Common = { workspace?: string };
export type WorkflowSaveInput = Common & DraftInput & { operation: 'workflow_save'; draft_revision: string; workflow_id?: string; expected_revision?: string };
export type WorkflowDeleteInput = Common & { operation: 'workflow_delete'; workflow_id: string; expected_revision: string };
export type WorkflowInput = (Common & { operation: 'workflows'; offset: number; limit: number }) |
  (Common & { operation: 'workflow_get'; workflow_id: string; revision?: string; output_file: string; versions_offset: number; versions_limit: number }) |
  (Common & { operation: 'workflow_import'; file: string; ui?: string; output_file: string }) |
  (Common & DraftInput & { operation: 'workflow_validate'; edit_json?: string; workbench_edit_json?: string; output_file: string }) | WorkflowSaveInput | WorkflowDeleteInput;

export function text(value: unknown, name: string, max = BODY_BYTES, empty = false): asserts value is string {
  if (typeof value !== 'string' || (!empty && value.length === 0) || /[\uD800-\uDFFF]/u.test(value) || Buffer.byteLength(value) > max) throw new Error(`${name} must be well-formed text within ${max} UTF-8 bytes`);
}
export function hex(value: unknown, name: string): asserts value is string {
  if (typeof value !== 'string' || !/^[a-f0-9]{64}$/.test(value)) throw new Error(`${name} must contain 64 lowercase hexadecimal characters`);
}
export function keys(value: ObjectValue, allowed: string[]): void {
  for (const key of Object.keys(value)) if (!allowed.includes(key)) throw new Error(`Unknown field: ${key}`);
}
export function safePath(value: unknown, name: string, output = false): asserts value is string {
  text(value, name, output ? 512 : 4096);
  if (value.includes('\0') || value.split('/').some((part) => ['..', '.runcradle', '.git', '.agents', '.codex', '.aws'].includes(part)) || (output && (isAbsolute(value) || value.endsWith('/') || value.split('/').some((part) => part === '' || part === '.')))) throw new Error(`${name} must stay in the workspace outside protected paths${output ? ' and name a new relative file' : ''}`);
}
export function parseDraft(raw: unknown): Draft {
  const value = object(raw);
  keys(value, ['format', 'source', 'ui_source', 'entry', 'ui_path']);
  if (value.format !== 'yaml' && value.format !== 'json') throw new Error('Draft format must be yaml or json');
  text(value.source, 'source', SOURCE_BYTES, true);
  if ('ui_source' in value) text(value.ui_source, 'ui_source', UI_BYTES, true);
  for (const key of ['entry', 'ui_path']) if (key in value && value[key] !== '') safePath(value[key], key);
  for (const key of ['entry', 'ui_path']) if (key in value) text(value[key], key, 4096, true);
  return Object.freeze({ ...value }) as Draft;
}
const draftFields = ['draft', 'draft_file', 'draft_sha256'];
const fields: Record<WorkflowOperation, string[]> = {
  workflows: ['offset', 'limit'], workflow_get: ['workflow_id', 'revision', 'output_file', 'versions_offset', 'versions_limit'],
  workflow_import: ['file', 'ui', 'output_file'], workflow_validate: [...draftFields, 'edit_json', 'workbench_edit_json', 'output_file'],
  workflow_save: [...draftFields, 'draft_revision', 'workflow_id', 'expected_revision'],
  workflow_delete: ['workflow_id', 'expected_revision'],
};
export function parseWorkflowInput(operation: WorkflowOperation, raw: unknown): WorkflowInput {
  const value = object(raw); keys(value, ['workspace', ...fields[operation]]);
  const input: ObjectValue = { ...value, operation };
  if ('workspace' in input) { text(input.workspace, 'workspace', 4096); if ((input.workspace as string).includes('\0')) throw new Error('Invalid workspace'); }
  for (const key of ['workflow_id', 'revision', 'draft_revision', 'expected_revision', 'draft_sha256']) if (key in input) hex(input[key], key);
  for (const key of ['file', 'ui', 'draft_file']) if (key in input) safePath(input[key], key);
  if (['workflow_get', 'workflow_import', 'workflow_validate'].includes(operation)) safePath(input.output_file, 'output_file', true);
  if (operation === 'workflow_get') hex(input.workflow_id, 'workflow_id');
  if (operation === 'workflow_delete') { hex(input.workflow_id, 'workflow_id'); hex(input.expected_revision, 'expected_revision'); }
  if (operation === 'workflow_import') safePath(input.file, 'file');
  if (operation === 'workflow_validate' || operation === 'workflow_save') {
    if (('draft' in input) === ('draft_file' in input) || ('draft_file' in input) !== ('draft_sha256' in input)) throw new Error('Choose draft or draft_file + draft_sha256');
    if ('draft' in input) input.draft = parseDraft(input.draft) as ObjectValue;
  }
  if (operation === 'workflow_save') {
    hex(input.draft_revision, 'draft_revision');
    if (('workflow_id' in input) !== ('expected_revision' in input)) throw new Error('workflow_id and expected_revision must appear together');
  }
  if (operation === 'workflow_validate') {
    if ('edit_json' in input && 'workbench_edit_json' in input) throw new Error('edit_json and workbench_edit_json are mutually exclusive');
    for (const key of ['edit_json', 'workbench_edit_json']) if (key in input) text(input[key], key);
  }
  for (const key of operation === 'workflows' ? ['offset', 'limit'] : operation === 'workflow_get' ? ['versions_offset', 'versions_limit'] : []) {
    const limit = key.endsWith('limit'); input[key] ??= limit ? 20 : 0;
    if (!Number.isSafeInteger(input[key]) || Number(input[key]) < (limit ? 1 : 0) || Number(input[key]) > (limit ? 100 : Number.MAX_SAFE_INTEGER - 100)) throw new Error(`Invalid ${key}`);
  }
  return input as unknown as WorkflowInput;
}
export function checkWorkflowProtocol(value: ObjectValue, deleting = false): void {
  if (value.api_version !== 'v1' || value.contract_version !== '1.0.0' || !Array.isArray(value.features) || ['workflow_library', 'cas_save', 'structured_edit'].some((f) => !(value.features as unknown[]).includes(f))) throw new Error('RunCradle requires v1 / 1.0.0 with workflow_library, cas_save, structured_edit');
  if (deleting && !(value.features as unknown[]).includes('workflow_delete')) throw new Error('RunCradle CLI and service must support workflow_delete');
}
