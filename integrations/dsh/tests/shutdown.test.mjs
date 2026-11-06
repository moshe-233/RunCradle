import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createInterface } from 'node:readline';
import test from 'node:test';
import { stopOwnedProcess } from '../scripts/process.mjs';

async function childFixture(t, handler) {
  const child = spawn(process.execPath, ['-e', `process.on('SIGTERM', () => { ${handler} }); console.log('ready'); setInterval(() => {}, 1000);`], {
    detached: true, stdio: ['ignore', 'pipe', 'pipe'],
  });
  const closed = new Promise((resolve, reject) => {
    child.once('error', reject);
    child.once('close', (code, signal) => resolve({ code, signal }));
  });
  const lines = createInterface({ input: child.stdout });
  let disposed = false;
  await new Promise((resolve) => lines.on('line', (line) => {
    if (line === 'ready') resolve();
    if (line === 'disposed') disposed = true;
  }));
  t.after(async () => {
    if (child.exitCode === null && child.signalCode === null) process.kill(-child.pid, 'SIGKILL');
    await closed;
    lines.close();
  });
  return { child, closed, options: { timeoutMs: 500, completed: () => disposed } };
}

test('shutdown waits for cleanup acknowledgement and zero exit', async (t) => {
  const { child, closed, options } = await childFixture(t, "console.log('disposed'); process.exit(0)");
  assert.deepEqual(await stopOwnedProcess(child, closed, options), { code: 0, signal: null });
});

test('zero exit without completed cleanup is rejected', async (t) => {
  const { child, closed, options } = await childFixture(t, 'process.exit(0)');
  await assert.rejects(stopOwnedProcess(child, closed, options), /cleanup acknowledgement/);
});

test('a child ignoring shutdown is killed and fails verification', async (t) => {
  const { child, closed, options } = await childFixture(t, '');
  await assert.rejects(stopOwnedProcess(child, closed, options), /forced termination/);
  assert.equal(child.signalCode, 'SIGKILL');
});

test('nonzero shutdown is rejected even with a cleanup acknowledgement', async (t) => {
  const { child, closed, options } = await childFixture(t, "console.log('disposed'); process.exit(7)");
  await assert.rejects(stopOwnedProcess(child, closed, options), /exit.*7/);
});
