import { spawn } from 'node:child_process';

/** Test boundary for consumer framing; real DSH provider is exercised by test:runtime. */
export function localSubprocess() {
  const specs = [];
  return {
    specs,
    async resolveExecutable(path, _env, signal) { signal?.throwIfAborted(); return path; },
    spawn(spec) {
      specs.push(spec);
      const child = spawn(spec.argv[0], spec.argv.slice(1), { cwd: spec.cwd, stdio: ['ignore', 'pipe', 'pipe'] });
      const streams = { stdout: Buffer.alloc(0), stderr: Buffer.alloc(0) };
      const totals = { stdout: 0, stderr: 0 };
      for (const key of ['stdout', 'stderr']) child[key].on('data', (data) => {
        totals[key] += data.length;
        streams[key] = Buffer.concat([streams[key], data]).subarray(-spec.stdio[key].maxBytes);
      });
      let kill;
      const terminate = () => { child.kill('SIGTERM'); kill ??= setTimeout(() => child.kill('SIGKILL'), spec.graceMs); };
      const done = new Promise((resolve, reject) => {
        child.once('error', reject);
        child.once('close', (exitCode, signal) => { clearTimeout(kill); spec.signal?.removeEventListener('abort', terminate); resolve({ exitCode, signal }); });
      });
      spec.signal?.addEventListener('abort', terminate, { once: true });
      return {
        done, terminate,
        collected: Object.fromEntries(['stdout', 'stderr'].map((key) => [key, { readFrom() { return { text: streams[key].toString(), nextOffset: totals[key], lossy: totals[key] > streams[key].length }; } }])),
        async waitForExit(signal) {
          if (signal?.aborted) return false;
          let abort;
          try { return await Promise.race([done.then(() => true), new Promise((resolve) => { abort = () => resolve(false); signal?.addEventListener('abort', abort, { once: true }); })]); }
          finally { if (abort) signal?.removeEventListener('abort', abort); }
        },
      };
    },
  };
}
