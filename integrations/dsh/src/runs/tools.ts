import type { Context } from '@deepseek-ai/cordis';
import { defineTool, type ParameterSchemaSpec, type ToolRunContext } from '@deepseek-ai/dsh-tools';
import { authorize } from './access.js';
import { apiRequest } from './api.js';
import { CliRunner, ClientError } from './runner.js';
import { parseInput, commandFor, checkProtocol, outcome, projectRuns, boundFailure, type Input, type ObjectValue, type Operation } from './contract.js';

const string = (description: string) => ({ type: 'string' as const, description });
const workspace = string('Optional workspace, absolute or relative to the current Agent working directory. Defaults to that directory.');
const request = string('Stable caller-supplied idempotency identity: 1..128 UTF-8 bytes, no control characters. Preserve exactly across recovery.');
const runID = string('Run identity: 32 lowercase hexadecimal characters.');
const limit = { type: 'integer' as const, description: 'Page size 1..100; default 20.' };
const parameters: Record<Operation, ParameterSchemaSpec> = {
  run: { workspace, request_id: { ...request, required: true }, targets: { type: 'array', items: { type: 'string' }, required: true, description: 'Nonempty array of distinct target names.' },
    file: string('Workflow file inside workspace; mutually exclusive with workflow_id/revision.'), workflow_id: string('Saved workflow identity; requires revision and excludes file/ui.'), revision: string('Exact saved workflow revision.'),
    ui: string('Optional UI declaration file inside workspace, only with file.'), preset: string('Optional preset name.'), values_file: string('Optional JSON values file inside workspace.'),
    timeout_ms: { type: 'integer', description: 'Workflow deadline 1..3600000 ms; default 300000. This is not the CLI client deadline.' }, retain_evidence: { type: 'boolean', description: 'Retain temporary diagnostics under the core retention policy.' } },
  runs: { workspace, workflow_id: string('Optional workflow filter.'), offset: { type: 'integer', description: 'Nonnegative page offset; default 0. Resume with returned next_offset.' }, limit },
  inspect: { workspace, run_id: runID, request_id: request, node: string('One node detail; mutually exclusive with nodes_after/events_after.'), nodes_after: { type: 'integer', description: 'Nonnegative node offset; selects node-page view even when zero.' }, events_after: string('Decimal uint64 event cursor; do not convert to JavaScript number. Mutually exclusive with node/nodes_after.'), limit },
  stop: { workspace, run_id: { ...runID, required: true } },
};
const descriptions: Record<Operation, string> = {
  run: 'Submit a workflow asynchronously to a prestarted RunCradle workspace service. Choose file or workflow_id + revision. Requires an Agent and approval in restricted sessions. Returns acceptance, not task completion. Keep workspace/request_id to inspect uncertain submissions; never replace the ID to retry. Client cancellation does not stop an accepted run.',
  runs: 'List workspace run summaries from the prestarted service, including other sessions. Returns bounded pages with next_offset. Requires Agent authorization and temporary response files; read-only sessions require approval. No offline discovery or private-storage scan.',
  inspect: 'Inspect by exactly one of run_id/request_id, including offline history. Choose summary, node detail, node page or event cursor. Query success is independent of task_result/final/cleanup. Preserves truncation and history-gap markers. Inspection may expire diagnostic files; read-only sessions require approval.',
  stop: 'Request stop by run_id in a prestarted workspace service. Requires Agent authorization and approval in restricted sessions. stop_requested does not prove terminal state or cleanup: follow with inspect. Never used automatically on client/session/plugin cancellation.',
};

export function registerRunTools(ctx: Context, executable: string): void {
  const runner = new CliRunner(ctx.subprocess, executable);
  const lifetime = new AbortController();
  const active = new Set<Promise<unknown>>();
  ctx.effect(() => async () => {
    lifetime.abort();
    await runner.dispose();
    await Promise.allSettled([...active]);
  });
  for (const operation of Object.keys(parameters) as Operation[]) {
    ctx.tools.register(defineTool({
      name: `runcradle_${operation}`, description: descriptions[operation], parameters: parameters[operation],
      output: { schema: { type: 'object', additionalProperties: true }, render: (_args, value) => [{ type: 'text', text: JSON.stringify(value) }] },
      execute(args, exec) {
        const promise = invoke(operation, args, exec);
        active.add(promise);
        void promise.finally(() => active.delete(promise)).catch(() => {});
        return promise;
      },
    }));
  }

  async function invoke(operation: Operation, args: unknown, original: ToolRunContext): Promise<ObjectValue> {
    const cancelled = new AbortController();
    const signal = AbortSignal.any([original.signal, lifetime.signal, cancelled.signal]);
    const exec = { ...original, signal };
    let finish!: () => void;
    const finished = new Promise<void>((resolve) => { finish = resolve; });
    const unbind = original.agent?.ctx.effect(() => async () => { cancelled.abort(); await finished; });
    let input: Input | undefined;
    let resolved: string | undefined;
    let dispatched = false;
    try {
      if (process.platform !== 'linux') throw new Error('RunCradle tools currently require local Linux');
      input = parseInput(operation, args);
      const access = await authorize(ctx, exec, operation, input);
      resolved = access.workspace;
      await access.recheck();
      const cli = await runner.run(['api', 'describe'], resolved, signal);
      if (cli.exitCode !== 0) throw new ClientError('protocol_error', 'CLI api describe failed', 'confirmed', cli.receipt);
      checkProtocol(cli.receipt, operation);
      if (operation !== 'inspect') checkProtocol(await apiRequest(runner, access, '/api/v1/protocol', signal), operation);
      await access.recheck();
      if (operation === 'runs') {
        const query = new URLSearchParams({ offset: String(input.offset), limit: String(input.limit) });
        if (input.workflow_id) query.set('workflow_id', input.workflow_id);
        const result = projectRuns(await apiRequest(runner, access, `/api/v1/runs?${query}`, signal), input.offset!, input.limit!);
        return outcome(operation, resolved, 0, result);
      }
      dispatched = true;
      const result = await runner.run(commandFor(operation, input, resolved), resolved, signal);
      return outcome(operation, resolved, result.exitCode, result.receipt, input);
    } catch (error) {
      const value: ObjectValue = { operation, ok: false, code: error instanceof ClientError ? error.code : 'invalid_operation', error: (error instanceof Error ? error.message : String(error)).slice(0, 2048), client_cleanup: error instanceof ClientError ? error.clientCleanup : 'confirmed' };
      if (resolved) value.workspace = resolved;
      else if (input?.workspace) value.workspace = input.workspace;
      if (input?.request_id) value.request_id = input.request_id;
      if (input?.run_id) value.run_id = input.run_id;
      if (error instanceof ClientError && error.receipt) value.result = error.receipt;
      if (operation === 'run') {
        value.submission = dispatched ? 'unknown' : 'not_submitted';
        value.next_action = dispatched ? 'Inspect the original workspace and request_id before retrying. Do not change request_id.' : 'Resolve the reported error before submitting.';
      }
      return boundFailure(value);
    } finally { finish(); await unbind?.(); }
  }
}
