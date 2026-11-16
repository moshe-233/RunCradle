import { relative } from 'node:path';
import type { Context } from '@deepseek-ai/cordis';
import { defineTool, type ParameterSchemaSpec, type ToolRunContext } from '@deepseek-ai/dsh-tools';
import { resolveAccess } from '../shared/access.js';
import { APIRequestError, requestJSON, type APIRequest } from '../shared/api.js';
import { ArtifactError, decodeUTF8, inputPath, readBounded } from '../shared/artifacts.js';
import { CliRunner, ClientError } from '../runs/runner.js';
import { OUTPUT_BYTES } from '../runs/contract.js';
import { parseWorkflowInput, checkWorkflowProtocol, type ObjectValue, type WorkflowInput, type WorkflowOperation, type Mutation } from './contract.js';
import { buildValidateBody, boundedBody, loadDraft, publishDraft, validateEdit, checkLogicalPaths } from './draft.js';
import { documentDraft, projectWorkflowDocument, projectWorkflowPage, projectWorkflowSave, projectWorkflowDelete } from './results.js';

const string = (description: string) => ({ type: 'string' as const, description });
const workspace = string('Workspace relative to Agent working directory or absolute; defaults to current directory.');
const identity = string('64 lowercase hexadecimal characters; preserve exactly.');
const output = { ...string('New relative JSON Draft file inside workspace, at most 512 UTF-8 bytes. Parent must exist without symlinks. Never overwrites. Retained across calls and sessions.'), required: true as const };
const offset = { type: 'integer' as const, description: 'Nonnegative offset, default 0. Pagination is dynamic, not a cross-call snapshot.' };
const limit = { type: 'integer' as const, description: 'Page size 1..100; default 20.' };
const draft: ParameterSchemaSpec = {
  draft: { type: 'object', additionalProperties: false, description: 'Exact Draft; choose this or draft_file + draft_sha256.', properties: {
    format: { type: 'string', enum: ['yaml', 'json'], required: true }, source: { ...string('Exact native text, at most 1 MiB.'), required: true },
    ui_source: string('Exact optional UI JSON text, at most 256 KiB.'), entry: string('Logical workflow path, default process-compose.<format>.'), ui_path: string('Logical UI path, default ui.json when UI text exists.'),
  } },
  draft_file: string('Workspace JSON Draft file, at most 8 MiB. Requires matching draft_sha256; read after authorization and frozen for this call.'), draft_sha256: identity,
};
export const workflowParameters: Record<WorkflowOperation, ParameterSchemaSpec> = {
  workflows: { workspace, offset, limit },
  workflow_get: { workspace, workflow_id: { ...identity, required: true }, revision: identity, output_file: output, versions_offset: offset, versions_limit: limit },
  workflow_import: { workspace, file: { ...string('Native workflow file inside workspace.'), required: true }, ui: string('Optional UI file inside workspace.'), output_file: output },
  workflow_validate: { workspace, ...draft, output_file: output, edit_json: string('One native Edit JSON object: process, field, value, remove?. Preserve integer tokens; mutually exclusive with workbench_edit_json.'), workbench_edit_json: string('One WorkbenchEdit JSON object from public OpenAPI; preserve integer tokens. Mutually exclusive with edit_json.') },
  workflow_save: { workspace, ...draft, draft_revision: { ...identity, required: true }, workflow_id: identity, expected_revision: identity },
  workflow_delete: { workspace, workflow_id: { ...identity, required: true }, expected_revision: { ...identity, required: true } },
};
const descriptions: Record<WorkflowOperation, string> = {
  workflows: 'List saved workspace workflows across sessions through a prestarted RunCradle service. Bounded summaries, next_offset and has_more. Requires Agent policy; read-only sessions need approval for temporary files.',
  workflow_get: 'Read head or an exact historical revision into a new output_file. Separates document_revision from head_revision and pages unique first-seen versions. Retain draft_file/hash and workflow identity to continue editing.',
  workflow_import: 'Import existing native/UI files into an unsaved Draft at a new output_file. Does not add a library record or modify source files. Static invalidity may still return ok=true, valid=false for repair.',
  workflow_validate: 'Validate an exact Draft, optionally apply one native or workbench edit, and publish the resulting Draft to a new output_file. Invalid raw drafts remain editable; rejected edits create no artifact. valid is distinct from engine support and execution success.',
  workflow_save: 'Create or CAS-update a saved workflow from a finished Draft. Requires draft_revision from get/import/validate; update requires workflow_id and expected_revision together. Prevalidates the same frozen input. Restricted sessions require approval. Never auto-retries unknown writes or auto-merges conflicts. Returns saved identity, not execution success.',
  workflow_delete: 'Delete one saved workflow library entry using workflow_id and the head_revision from a fresh list/get as expected_revision. Retains source files, immutable versions and existing runs. Restricted sessions require approval. Never removes internal files directly or retries an uncertain deletion. On conflict, list/get again and resolve explicitly.',
};

export function registerWorkflowTools(ctx: Context, executable: string): void {
  const runner = new CliRunner(ctx.subprocess, executable);
  const lifetime = new AbortController(), active = new Set<Promise<unknown>>();
  ctx.effect(() => async () => { lifetime.abort(); await runner.dispose(); await Promise.allSettled([...active]); });
  for (const operation of Object.keys(workflowParameters) as WorkflowOperation[]) ctx.tools.register(defineTool({
    name: `runcradle_${operation}`, description: descriptions[operation], parameters: workflowParameters[operation],
    output: { schema: { type: 'object', additionalProperties: true }, render: (_args, value) => [{ type: 'text', text: JSON.stringify(value) }] },
    execute(args, exec) {
      const promise = invoke(operation, args, exec); active.add(promise);
      void promise.finally(() => active.delete(promise)).catch(() => {}); return promise;
    },
  }));

  async function invoke(operation: WorkflowOperation, args: unknown, original: ToolRunContext): Promise<ObjectValue> {
    const cancelled = new AbortController();
    const signal = AbortSignal.any([original.signal, lifetime.signal, cancelled.signal]);
    let finish!: () => void;
    const finished = new Promise<void>((resolve) => { finish = resolve; });
    const unbind = original.agent?.ctx.effect(() => async () => { cancelled.abort(); await finished; });
    let input: WorkflowInput | undefined, workspace: string | undefined, mutation: Mutation = 'not_attempted', result: ObjectValue = {};
    const envelope = (ok: boolean): ObjectValue => ({ operation, ...(workspace ? { workspace } : {}), ok, result, client_cleanup: 'confirmed' });
    try {
      if (process.platform !== 'linux') throw new Error('RunCradle workflow tools require local Linux');
      input = parseWorkflowInput(operation, args);
      if (input.operation === 'workflow_validate') {
        if (input.edit_json !== undefined) validateEdit(input.edit_json, false);
        if (input.workbench_edit_json !== undefined) validateEdit(input.workbench_edit_json, true);
      }
      const save = input.operation === 'workflow_save' || input.operation === 'workflow_delete';
      const inputFiles = input.operation === 'workflow_import' ? [input.file, input.ui].filter((v): v is string => v !== undefined) : 'draft_file' in input && input.draft_file ? [input.draft_file] : [];
      const outputFiles = 'output_file' in input ? [input.output_file] : [];
      const reason = input.operation === 'workflow_delete' ? `Delete RunCradle library entry ${input.workflow_id} at expected_revision ${input.expected_revision}. Source files, archived definitions and existing runs are retained.` : input.operation === 'workflow_save' ? `Change the external RunCradle workflow library (${input.workflow_id ? 'update' : 'create'}). Workflow: ${input.workflow_id ?? 'new'}; expected_revision: ${input.expected_revision ?? 'none'}; draft_revision: ${input.draft_revision}; input: ${input.draft_file ?? 'inline Draft'}; sha256: ${input.draft_sha256 ?? 'inline'}.` : 'Read RunCradle workflow definitions and create temporary protocol files and any requested persistent Draft artifact.';
      const access = await resolveAccess(ctx, { ...original, signal }, { toolName: `runcradle_${operation}`, workspace: input.workspace, inputFiles, outputFiles, requiresRestrictedApproval: save, reason, forbidPrivatePaths: true });
      workspace = access.workspace;
      const snapshot = input.operation === 'workflow_validate' || input.operation === 'workflow_save' ? await loadDraft(access, input, signal) : undefined;
      let importSnapshot: { source: string; ui_source?: string } | undefined;
      if (input.operation === 'workflow_import') {
        const filePath = await inputPath(access.workspace, input.file);
        const fileRaw = await readBounded(filePath, 1024 * 1024, signal);
        const uiRaw = input.ui ? await readBounded(await inputPath(access.workspace, input.ui), 256 * 1024, signal) : undefined;
        importSnapshot = { source: decodeUTF8(fileRaw), ...(uiRaw ? { ui_source: decodeUTF8(uiRaw) } : {}) };
        await access.recheck();
      }
      const cli = await runner.run(['api', 'describe'], workspace, signal);
      if (cli.exitCode !== 0) throw new ClientError('protocol_error', 'CLI api describe failed');
      checkWorkflowProtocol(cli.receipt, input.operation === 'workflow_delete');
      const request = (req: APIRequest) => requestJSON(runner, access, req, signal);
      checkWorkflowProtocol((await request({ method: 'GET', path: '/api/v1/protocol', mutating: false })).body, input.operation === 'workflow_delete');
      if (input.operation === 'workflows') {
        const page = await request({ method: 'GET', path: `/api/v1/workflows?offset=${input.offset}&limit=${input.limit}`, mutating: false });
        if (page.httpStatus !== 200) throw new Error('Unexpected list HTTP status');
        result = projectWorkflowPage(page.body, input.offset, input.limit);
      } else if (input.operation === 'workflow_delete') {
        try {
          const deleted = await request({ method: 'DELETE', path: `/api/v1/workflows/${input.workflow_id}`, bodyText: JSON.stringify({ expected_revision: input.expected_revision }), mutating: true });
          mutation = 'unknown';
          if (deleted.httpStatus !== 200) throw new Error('Unexpected delete HTTP status');
          result = projectWorkflowDelete(deleted.body, input); mutation = 'confirmed';
        } catch (error) {
          if (error instanceof APIRequestError) {
            mutation = error.dispatched ? 'unknown' : 'not_attempted';
            if ((error.httpStatus === 409 && error.body?.code === 'conflict') || (error.httpStatus === 404 && error.body?.code === 'not_found')) mutation = 'not_deleted';
            if (error.httpStatus === 200 && error.body) {
              try { result = projectWorkflowDelete(error.body, input); mutation = 'confirmed'; } catch { /* A mismatched receipt cannot confirm deletion. */ }
            }
          }
          throw error;
        }
      } else if (input.operation === 'workflow_save') {
        const validated = await request({ method: 'POST', path: '/api/v1/validate', bodyText: buildValidateBody(snapshot!.draft), mutating: false });
        const summary = projectWorkflowDocument(validated.body);
        if (validated.httpStatus !== 200 || summary.valid !== true || summary.document_revision !== input.draft_revision) throw new Error('Save requires a valid Draft matching draft_revision; validate the edited input first');
        await checkLogicalPaths(access, snapshot!.draft);
        const requestBody = boundedBody(JSON.stringify({ ...snapshot!.draft, ...(input.expected_revision ? { expected_revision: input.expected_revision } : {}) }));
        try {
          const saved = await request({ method: input.workflow_id ? 'PUT' : 'POST', path: `/api/v1/workflows${input.workflow_id ? '/' + input.workflow_id : ''}`, bodyText: requestBody, mutating: true });
          mutation = 'unknown';
          if (saved.httpStatus !== (input.workflow_id ? 200 : 201)) throw new Error('Unexpected save HTTP status');
          result = projectWorkflowSave(saved.body, input); mutation = 'confirmed';
        } catch (error) {
          if (error instanceof APIRequestError) {
            mutation = error.dispatched ? 'unknown' : 'not_attempted';
            if (error.httpStatus === 409 && error.body?.status === 409 && error.body?.code === 'conflict') mutation = 'not_saved';
            if (error.httpStatus === (input.workflow_id ? 200 : 201) && error.body) {
              try { result = projectWorkflowSave(error.body, input); mutation = 'confirmed'; } catch { /* Keep unknown when write identity cannot be proven. */ }
            }
          }
          throw error;
        }
      } else {
        let req: APIRequest;
        if (input.operation === 'workflow_get') req = { method: 'GET', path: `/api/v1/workflows/${input.workflow_id}${input.revision ? '?revision=' + input.revision : ''}`, mutating: false };
        else if (input.operation === 'workflow_import') req = { method: 'POST', path: '/api/v1/imports', bodyText: JSON.stringify({ file: input.file, ...(input.ui ? { ui: input.ui } : {}) }), mutating: false };
        else req = { method: 'POST', path: '/api/v1/validate', bodyText: buildValidateBody(snapshot!.draft, input.edit_json, input.workbench_edit_json), mutating: false };
        const response = await request(req);
        if (response.httpStatus !== 200) throw new Error('Unexpected document HTTP status');
        if (importSnapshot && (response.body.source !== importSnapshot.source || response.body.ui_source !== importSnapshot.ui_source)) throw new Error('Imported file changed or API returned substituted content');
        await access.recheck();
        result = projectWorkflowDocument(response.body, undefined, input.operation === 'workflow_get' ? { offset: input.versions_offset, limit: input.versions_limit } : undefined);
        if (input.operation === 'workflow_get' && (result.workflow_id !== input.workflow_id || (input.revision !== undefined && result.document_revision !== input.revision))) throw new Error('Document response identity differs from request');
        await access.recheck();
        Object.assign(result, await publishDraft(access, input.output_file, documentDraft(response.body), signal));
      }
      const value = envelope(true);
      if (Buffer.byteLength(JSON.stringify(value)) > OUTPUT_BYTES) throw new Error('Workflow result exceeds 64 KiB');
      return value;
    } catch (error) {
      // Async request catch blocks assign the mutation state before rethrowing.
      const failedMutation = mutation as Mutation;
      if (operation === 'workflow_save') {
        result.mutation = mutation;
        if (input?.operation === 'workflow_save') for (const key of ['workflow_id', 'expected_revision', 'draft_revision', 'draft_file', 'draft_sha256'] as const) if (input[key] !== undefined) result[key] = input[key]!;
        result.next_action = failedMutation === 'confirmed' ? 'Saved identity is confirmed; resolve client cleanup separately.' : failedMutation === 'not_saved' ? 'Get the current head, reconcile changes, then validate before an intentional CAS save.' : failedMutation === 'unknown' ? 'Do not automatically retry. Query the original workspace/workflow; current or historical matching content does not identify this call. New creates may have no uniquely recoverable ID.' : 'Resolve the error and validate before saving.';
      }
      if (operation === 'workflow_delete') {
        result.mutation = mutation;
        if (input?.operation === 'workflow_delete') Object.assign(result, { workflow_id: input.workflow_id, expected_revision: input.expected_revision });
        result.next_action = failedMutation === 'confirmed' ? 'Deletion is confirmed; resolve client cleanup separately.' : failedMutation === 'not_attempted' ? 'Resolve the error before deleting.' : 'Refresh the original workspace workflow list and check the same workflow_id. Do not automatically retry or replace expected_revision.';
      }
      if (error instanceof ArtifactError) { result.artifact_state = error.artifactState; result.draft_file = error.artifactPath; }
      if (error instanceof APIRequestError && error.temporaryPath) result.temporary_path = workspace ? relative(workspace, error.temporaryPath) : error.temporaryPath;
      const value = envelope(false);
      value.code = error instanceof ClientError ? error.code : 'invalid_operation';
      value.error = (error instanceof Error ? error.message : String(error)).slice(0, 1024);
      value.client_cleanup = error instanceof ClientError ? error.clientCleanup : 'confirmed';
      if (Buffer.byteLength(JSON.stringify(value)) > OUTPUT_BYTES) {
        // Preserve recovery identities and mutation classification while dropping diagnostics.
        const keep = new Set(['mutation', 'workflow_id', 'expected_revision', 'deleted_revision', 'deleted', 'draft_revision', 'draft_file', 'draft_bytes', 'draft_sha256', 'artifact_state', 'temporary_path', 'saved_revision', 'head_revision', 'version_count', 'next_action']);
        for (const key of Object.keys(result)) if (!keep.has(key)) delete result[key];
        result.details_truncated = true;
        value.error = 'Workflow operation failed; diagnostic details omitted to fit output budget.';
      }
      if (Buffer.byteLength(JSON.stringify(value)) > OUTPUT_BYTES && typeof value.workspace === 'string') { delete value.workspace; value.workspace_omitted = true; }
      if (Buffer.byteLength(JSON.stringify(value)) > OUTPUT_BYTES) { delete result.temporary_path; value.error = 'Workflow operation failed; recovery fields retained.'; }
      if (Buffer.byteLength(JSON.stringify(value)) > OUTPUT_BYTES) throw new Error('Workflow result exceeds 64 KiB after bounded error projection');
      return value;
    } finally { finish(); await unbind?.(); }
  }
}
