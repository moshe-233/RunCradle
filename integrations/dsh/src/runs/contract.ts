export type Operation = 'run' | 'runs' | 'inspect' | 'stop';
export type Json = null | boolean | number | string | Json[] | { [key: string]: Json };
export type ObjectValue = { [key: string]: Json };
export const OUTPUT_BYTES = 64 * 1024;
export const RESPONSE_BYTES = 32 * 1024 * 1024;

export interface Input {
  workspace?: string;
  request_id?: string;
  run_id?: string;
  targets?: string[];
  file?: string;
  workflow_id?: string;
  revision?: string;
  ui?: string;
  preset?: string;
  values_file?: string;
  timeout_ms?: number;
  retain_evidence?: boolean;
  offset?: number;
  limit?: number;
  node?: string;
  nodes_after?: number;
  events_after?: string;
}

export function object(value: unknown): ObjectValue {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new Error('Expected one JSON object');
  return value as ObjectValue;
}

const fields: Record<Operation, string[]> = {
  run: ['request_id', 'targets', 'file', 'workflow_id', 'revision', 'ui', 'preset', 'values_file', 'timeout_ms', 'retain_evidence'],
  runs: ['workflow_id', 'offset', 'limit'],
  inspect: ['run_id', 'request_id', 'node', 'nodes_after', 'events_after', 'limit'],
  stop: ['run_id'],
};

function integer(value: unknown, min: number, max: number, name: string): void {
  if (!Number.isSafeInteger(value) || (value as number) < min || (value as number) > max) throw new Error(`${name} must be an integer in ${min}..${max}`);
}

export function parseInput(operation: Operation, value: unknown): Input {
  const source = object(value);
  for (const [key, item] of Object.entries(source)) {
    if (key !== 'workspace' && !fields[operation].includes(key)) throw new Error(`Unknown argument: ${key}`);
    if (['timeout_ms', 'limit', 'offset', 'nodes_after', 'targets', 'retain_evidence'].includes(key)) continue;
    if (typeof item !== 'string' || item.length === 0 || item.includes('\0') || /[\uD800-\uDFFF]/u.test(item)) throw new Error(`${key} must be a nonempty well-formed Unicode string without NUL`);
  }
  const input = { ...source } as unknown as Input;
  if (input.request_id !== undefined && (Buffer.byteLength(input.request_id) > 128 || /\p{Cc}/u.test(input.request_id))) throw new Error('request_id must be 1..128 UTF-8 bytes without control characters');
  if (input.run_id !== undefined && !/^[a-f0-9]{32}$/.test(input.run_id)) throw new Error('run_id must contain 32 lowercase hexadecimal characters');
  if (input.limit !== undefined) integer(input.limit, 1, 100, 'limit');
  if (input.offset !== undefined) integer(input.offset, 0, Number.MAX_SAFE_INTEGER, 'offset');
  if (input.nodes_after !== undefined) integer(input.nodes_after, 0, Number.MAX_SAFE_INTEGER, 'nodes_after');
  if (input.events_after !== undefined && (!/^\d+$/.test(input.events_after) || input.events_after.length > 20 || BigInt(input.events_after) > 18446744073709551615n)) throw new Error('events_after must be a decimal uint64 string');
  if (operation === 'run') {
    if (!input.request_id) throw new Error('request_id is required before submission');
    if (!Array.isArray(input.targets) || input.targets.length === 0 || input.targets.some((v) => typeof v !== 'string' || v.length === 0 || v.includes('\0')) || new Set(input.targets).size !== input.targets.length) throw new Error('targets must be a nonempty array of distinct names');
    if (Boolean(input.file) === Boolean(input.workflow_id) || (input.workflow_id && !input.revision) || (input.file && input.revision) || (!input.file && input.ui)) throw new Error('Choose file with optional ui, or workflow_id with revision');
    input.timeout_ms ??= 300000;
    integer(input.timeout_ms, 1, 3600000, 'timeout_ms');
    if (input.retain_evidence !== undefined && typeof input.retain_evidence !== 'boolean') throw new Error('retain_evidence must be boolean');
    input.targets = [...input.targets];
  }
  if (operation === 'inspect') {
    if (Boolean(input.run_id) === Boolean(input.request_id)) throw new Error('Choose run_id or request_id');
    if (['node', 'nodes_after', 'events_after'].filter((key) => key in source).length > 1) throw new Error('Inspection views are mutually exclusive');
  }
  if (operation === 'stop' && !input.run_id) throw new Error('run_id is required');
  if (operation === 'runs' || operation === 'inspect') input.limit ??= 20;
  if (operation === 'runs') input.offset ??= 0;
  return input;
}

export function commandFor(operation: Exclude<Operation, 'runs'>, input: Input, workspace: string): string[] {
  const args = ['workflow', operation, '--workspace', workspace];
  if (operation === 'run') {
    args.push('--request-id', input.request_id!, '--detach', '--timeout-ms', String(input.timeout_ms));
    if (input.file) args.push('--file', input.file);
    else args.push('--workflow-id', input.workflow_id!, '--revision', input.revision!);
    if (input.ui) args.push('--ui', input.ui);
    if (input.preset) args.push('--preset', input.preset);
    if (input.values_file) args.push('--values', input.values_file);
    for (const target of input.targets!) args.push('--target', target);
    if (input.retain_evidence) args.push('--retain-evidence');
  } else {
    args.push(input.run_id ? '--run-id' : '--request-id', input.run_id ?? input.request_id!);
    if (operation === 'inspect') {
      args.push('--limit', String(input.limit));
      if (input.node !== undefined) args.push('--node', input.node);
      if (input.nodes_after !== undefined) args.push('--nodes-after', String(input.nodes_after));
      if (input.events_after !== undefined) args.push('--events-after', input.events_after);
    }
  }
  return args;
}

/** Recover cursor tokens before any Number rounding can affect the wire identity. */
export function parseReceipt(raw: string): ObjectValue {
  const tokens = new WeakMap<object, Map<string, string>>();
  const parse = JSON.parse as (text: string, reviver: (this: object, key: string, value: Json, context: { source?: string }) => Json) => Json;
  const result = object(parse(raw, function (key, value, context) {
    if (typeof value === 'number' && context.source !== undefined) {
      let entries = tokens.get(this);
      if (!entries) tokens.set(this, entries = new Map());
      entries.set(key, context.source);
    }
    return value;
  }));
  const cursor = (parent: ObjectValue, key: string) => {
    if (!(key in parent)) return;
    const text = tokens.get(parent)?.get(key);
    if (!text || !/^\d+$/.test(text) || text.length > 20 || BigInt(text) > 18446744073709551615n) throw new Error(`Invalid uint64 ${key}`);
    parent[key] = text;
  };
  cursor(result, 'next_seq');
  if (Array.isArray(result.events)) for (const event of result.events) cursor(object(event), 'seq');
  return result;
}

export function checkProtocol(value: ObjectValue, operation: Operation): void {
  if (value.api_version !== 'v1' || value.contract_version !== '1.0.0') throw new Error('Unsupported RunCradle API/contract; require v1 / 1.0.0');
  const needed: Record<Operation, string[]> = { run: ['idempotent_run', 'detached_submit'], runs: ['workflow_library'], inspect: ['offline_inspect', 'event_resume'], stop: ['explicit_stop'] };
  if (!Array.isArray(value.features) || needed[operation].some((feature) => !(value.features as Json[]).includes(feature))) throw new Error('RunCradle protocol lacks required capabilities');
}

const summaryFields = ['run_id', 'workflow_id', 'workspace_id', 'revision', 'started_at', 'ended_at', 'final', 'stop_requested', 'reason', 'task_result', 'cleanup', 'io_status', 'history_integrity', 'engine_exit_code'];

export function projectRuns(value: ObjectValue, offset: number, limit: number, budget = OUTPUT_BYTES - 4096): ObjectValue & { runs: ObjectValue[]; next_offset: number; has_more: boolean } {
  if (!Array.isArray(value.runs) || value.runs.length > limit || typeof value.has_more !== 'boolean' || value.next_offset !== offset + value.runs.length) throw new Error('Invalid run page or pagination receipt');
  const page = { runs: [] as ObjectValue[], next_offset: offset, has_more: value.has_more, details_truncated: false };
  for (const raw of value.runs) {
    const row = object(raw);
    if (typeof row.run_id !== 'string' || !/^[a-f0-9]{32}$/.test(row.run_id)) throw new Error('Invalid run identity in page');
    const summary: ObjectValue = {};
    for (const key of summaryFields) if (key in row) {
      const field = row[key];
      if (field !== null && !['string', 'boolean', 'number'].includes(typeof field)) throw new Error(`Invalid summary ${key}`);
      summary[key] = typeof field === 'string' ? [...field].slice(0, 512).join('') : field;
      if (summary[key] !== field) page.details_truncated = true;
    }
    page.runs.push(summary);
    page.next_offset++;
    if (Buffer.byteLength(JSON.stringify(page)) > budget) {
      page.runs.pop();
      page.next_offset--;
      page.has_more = true;
      page.details_truncated = true;
      if (!page.runs.length) throw new Error('One run summary exceeds output budget');
      break;
    }
  }
  return page;
}

export function outcome(operation: Operation, workspace: string, exitCode: number, result: ObjectValue, input?: Input): ObjectValue {
  if (typeof result.status !== 'string' && operation !== 'runs') throw new Error('CLI receipt is missing status');
  if (operation === 'run' && exitCode === 0 && (result.status !== 'accepted' || result.accepted !== true || typeof result.run_id !== 'string' || !/^[a-f0-9]{32}$/.test(result.run_id) || typeof result.request_id !== 'string')) throw new Error('CLI did not confirm submission identity');
  if (operation === 'stop' && exitCode === 0 && result.status !== 'stop_requested') throw new Error('CLI did not confirm stop request');
  if (exitCode === 0 && input && ((input.request_id && result.request_id !== input.request_id) || (input.run_id && result.run_id !== input.run_id))) throw new Error('CLI receipt identity differs from the requested identity');
  if (operation === 'inspect' && exitCode === 0 && !['incomplete', 'in_progress', 'cancelled', 'timed_out', 'failed', 'succeeded', 'not_started'].includes(String(result.status))) throw new Error('Invalid inspection status');
  const value: ObjectValue = { operation, workspace, ok: exitCode === 0, exit_code: exitCode, result, client_cleanup: 'confirmed' };
  if (Buffer.byteLength(JSON.stringify(value)) > OUTPUT_BYTES) throw new Error('Tool output exceeds 64 KiB');
  return value;
}

/** Never truncate recovery identities; omit oversized diagnostic fields explicitly. */
export function boundFailure(value: ObjectValue): ObjectValue {
  const fits = () => Buffer.byteLength(JSON.stringify(value)) <= OUTPUT_BYTES;
  if (fits()) return value;
  value.details_truncated = true;
  // A Linux canonical path fits well below this budget; this can only be an
  // unresolvable caller-supplied path. Submission has not happened in that case.
  if (typeof value.workspace === 'string' && Buffer.byteLength(JSON.stringify(value.workspace)) > 24 * 1024) {
    delete value.workspace;
    value.workspace_omitted = true;
  }
  if (fits()) return value;
  delete value.result;
  value.error = 'Error details exceeded the output budget; use the original call arguments to recover.';
  if (!fits()) throw new Error('Tool error output exceeds 64 KiB');
  return value;
}
