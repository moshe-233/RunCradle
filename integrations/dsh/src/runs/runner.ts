import type { SubprocessRuntime, SubprocessHandle } from '@deepseek-ai/dsh-subprocess';
import { parseReceipt, type ObjectValue } from './contract.js';

export class ClientError extends Error {
  constructor(public code: string, message: string, public clientCleanup: 'confirmed' | 'unknown' = 'confirmed', public receipt?: ObjectValue) { super(message); }
}

/** Bound even providers that fail to honour their signal contract. */
async function bounded<T>(promise: Promise<T>, signal: AbortSignal): Promise<T> {
  signal.throwIfAborted();
  let abort!: () => void;
  try {
    return await Promise.race([promise, new Promise<never>((_, reject) => {
      abort = () => reject(signal.reason);
      signal.addEventListener('abort', abort, { once: true });
    })]);
  } finally { signal.removeEventListener('abort', abort); }
}

export class CliRunner {
  private lifetime = new AbortController();
  private pending = new Set<Promise<unknown>>();
  constructor(private provider: Pick<SubprocessRuntime, 'resolveExecutable' | 'spawn'>, private executable: string) {}

  run(args: string[], cwd: string, signal: AbortSignal): Promise<{ exitCode: number; receipt: ObjectValue }> {
    const call = this.execute(args, cwd, signal);
    this.pending.add(call);
    void call.finally(() => this.pending.delete(call)).catch(() => {});
    return call;
  }

  async dispose(): Promise<void> {
    this.lifetime.abort(new Error('RunCradle client disposed'));
    await Promise.allSettled([...this.pending]);
  }

  private async execute(args: string[], cwd: string, caller: AbortSignal) {
    const deadline = new AbortController();
    const timer = setTimeout(() => deadline.abort(new Error('CLI deadline exceeded')), 60000);
    const signal = AbortSignal.any([caller, this.lifetime.signal, deadline.signal]);
    let handle: SubprocessHandle | undefined;
    let cleanup: 'confirmed' | 'unknown' = 'confirmed';
    try {
      signal.throwIfAborted();
      const executable = await bounded(this.provider.resolveExecutable(this.executable, undefined, signal), signal);
      signal.throwIfAborted();
      handle = this.provider.spawn({ argv: [executable, ...args], cwd, signal, graceMs: 3000,
        stdio: { stdin: 'ignore', stdout: { maxBytes: 4096 }, stderr: { maxBytes: 8192 } } });
      cleanup = 'unknown';
      const result = await bounded(handle.done, signal);
      // A command outcome alone does not prove that the managed range is empty.
      if (!await bounded(handle.waitForExit(signal), signal)) throw new Error('Managed range did not exit');
      cleanup = 'confirmed';
      if (result.exitCode === null || result.signal) throw new ClientError('protocol_error', `CLI exited by signal ${result.signal}`, cleanup);
      const output = handle.collected.stdout?.readFrom(0);
      if (!output || output.lossy || Buffer.byteLength(output.text) > 4096) throw new ClientError('protocol_error', 'CLI stdout was missing or truncated', cleanup);
      let receipt: ObjectValue;
      try { receipt = parseReceipt(output.text); }
      catch { throw new ClientError('protocol_error', 'CLI stdout was not a valid JSON receipt', cleanup); }
      return { exitCode: result.exitCode, receipt };
    } catch (error) {
      if (handle && cleanup !== 'confirmed') {
        try {
          handle.terminate();
          const bound = new AbortController();
          const end = setTimeout(() => bound.abort(), 8000); // 3 s grace + 5 s observation.
          try { if (await bounded(handle.waitForExit(bound.signal), bound.signal)) cleanup = 'confirmed'; }
          finally { clearTimeout(end); }
        } catch { cleanup = 'unknown'; }
      }
      if (signal.aborted) throw new ClientError(deadline.signal.aborted ? 'client_timeout' : 'cancelled', deadline.signal.aborted ? 'CLI client exceeded 60 seconds' : 'CLI client cancelled or disposed', cleanup);
      if (error instanceof ClientError) { error.clientCleanup = cleanup; throw error; }
      throw new ClientError('client_failed', error instanceof Error ? error.message : String(error), cleanup);
    } finally { clearTimeout(timer); }
  }
}
