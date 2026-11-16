import { realpath, lstat } from 'node:fs/promises';
import { resolve, basename, dirname } from 'node:path';
import type { Access } from '../runs/access.js';
import { sameFile, checkPath, decodeUTF8, digest, inputPath, readBounded, publishFile } from '../shared/artifacts.js';
import { BODY_BYTES, keys, object, parseDraft, text, type Draft, type DraftInput, type DraftArtifact, safePath, hex } from './contract.js';

function checkStrings(value: unknown): void {
  if (typeof value === 'string') text(value, 'edit text', BODY_BYTES, true);
  else if (Array.isArray(value)) value.forEach(checkStrings);
  else if (value !== null && typeof value === 'object') for (const [key, child] of Object.entries(value)) { text(key, 'edit key', BODY_BYTES, true); checkStrings(child); }
}
/** Validate the wire shape, then embed the original object text, never rounded JS numbers. */
export function validateEdit(raw: string, workbench: boolean): string {
  text(raw, 'edit JSON');
  const value = object(JSON.parse(raw)); checkStrings(value);
  if (!workbench) {
    keys(value, ['process', 'field', 'value', 'remove']);
    text(value.process, 'process');
    if (!['command', 'entrypoint', 'working_dir', 'environment', 'depends_on'].includes(String(value.field))) throw new Error('Unknown edit field');
    if (!('value' in value) || ('remove' in value && typeof value.remove !== 'boolean')) throw new Error('Edit requires value and optional boolean remove');
  } else {
    keys(value, ['action', 'process', 'new_name', 'label', 'upstream', 'command_mode', 'command', 'entrypoint', 'working_dir', 'env_items', 'depends_on', 'restart', 'exit_on_end', 'exit_on_skipped', 'input_spec', 'input_name', 'env_key', 'preset_spec', 'preset_id', 'title', 'description']);
    if (!['add_process', 'delete_process', 'rename_process', 'update_process', 'bind_input', 'unbind_input', 'save_preset', 'delete_preset', 'update_meta'].includes(String(value.action))) throw new Error('Unknown workbench action');
    if (value.input_spec != null) {
      const spec = object(value.input_spec); keys(spec, ['name', 'label', 'type', 'default', 'enum', 'bindings']);
      if (Array.isArray(spec.bindings)) for (const binding of spec.bindings) keys(object(binding), ['process', 'field', 'key']);
    }
    if (value.preset_spec != null) keys(object(value.preset_spec), ['id', 'label', 'values']);
    for (const [field, allowed] of [['env_items', ['key', 'value']], ['depends_on', ['from', 'condition']]] as const) if (Array.isArray(value[field])) for (const row of value[field]) keys(object(row), [...allowed]);
  }
  return raw;
}
export function boundedBody(body: string): string {
  if (Buffer.byteLength(body) > BODY_BYTES) throw new Error('HTTP body exceeds 8 MiB');
  return body;
}
export function buildValidateBody(draft: Draft, editJson?: string, workbenchEditJson?: string): string {
  if (editJson !== undefined && workbenchEditJson !== undefined) throw new Error('Edit forms are mutually exclusive');
  const base = JSON.stringify(parseDraft(draft));
  const extra = editJson !== undefined ? `,"edit":${validateEdit(editJson, false)}` : workbenchEditJson !== undefined ? `,"workbench_edit":${validateEdit(workbenchEditJson, true)}` : '';
  return boundedBody(base.slice(0, -1) + extra + '}');
}

export type DraftSnapshot = { draft: Draft; bytes: number; sha256: string };

export async function loadDraft(access: Access, input: DraftInput, signal: AbortSignal): Promise<DraftSnapshot> {
  await access.recheck(); signal.throwIfAborted();
  let raw: Buffer;
  if (input.draft !== undefined) raw = Buffer.from(boundedBody(JSON.stringify(parseDraft(input.draft))));
  else {
    safePath(input.draft_file, 'draft_file'); hex(input.draft_sha256, 'draft_sha256');
    const path = await inputPath(access.workspace, input.draft_file);
    const identity = await lstat(path, { bigint: true });
    raw = await readBounded(path, BODY_BYTES, signal);
    await access.recheck();
    if (!sameFile(identity, await lstat(path, { bigint: true })) || await inputPath(access.workspace, input.draft_file) !== path || digest(raw) !== input.draft_sha256) throw new Error('Draft file identity or SHA-256 differs from the authorized input');
  }
  const draft = parseDraft(JSON.parse(decodeUTF8(raw)));
  await checkLogicalPaths(access, draft);
  signal.throwIfAborted();
  return Object.freeze({ draft, bytes: raw.length, sha256: digest(raw) });
}
export async function checkLogicalPaths(access: Access, draft: Draft): Promise<void> {
  for (const name of [draft.entry, draft.ui_path]) {
    if (!name) continue;
    let path = resolve(access.workspace, name); checkPath(access.workspace, path);
    // Logical files need not exist; resolve the nearest existing ancestor to catch private aliases.
    const suffix: string[] = [];
    for (;;) {
      try { const canonical = await realpath(path); checkPath(access.workspace, resolve(canonical, ...suffix)); break; }
      catch (error) { if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error; suffix.unshift(basename(path)); path = dirname(path); }
    }
  }
}
export async function publishDraft(access: Access, output: string, draft: Draft, signal: AbortSignal): Promise<DraftArtifact> {
  safePath(output, 'output_file', true);
  const parsed = parseDraft(draft); await checkLogicalPaths(access, parsed);
  const raw = Buffer.from(boundedBody(JSON.stringify(parsed)));
  await publishFile(access.workspace, output, raw, signal, access.recheck);
  return { draft_file: output, draft_bytes: raw.length, draft_sha256: digest(raw) };
}
