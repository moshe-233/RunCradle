import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createInterface } from 'node:readline';
import test from 'node:test';

const processModule = new URL('../scripts/process.mjs', import.meta.url).href;

for (const signal of ['SIGINT', 'SIGTERM']) {
  test(`${signal} drains the runner's detached child before exiting`, { timeout: 5000 }, async (t) => {
    const source = `
      import { spawn } from 'node:child_process';
      import { createProcessOwner } from ${JSON.stringify(processModule)};
      const owner = createProcessOwner({ timeoutMs: 200, onInterrupt: async () => console.log('drained') });
      const child = spawn(process.execPath, ['-e', "process.on('SIGTERM', () => {}); console.log('ready'); setInterval(() => {}, 1000)"], { detached: true, stdio: ['ignore','pipe','pipe'] });
      const settled = new Promise(resolve => child.once('close', (code, signal) => { console.log(JSON.stringify({code, signal})); resolve(); }));
      owner.track(child, settled);
      child.stdout.once('data', () => console.log('pid:' + child.pid));
    `;
    const runner = spawn(process.execPath, ['--input-type=module', '-e', source], { stdio: ['ignore', 'pipe', 'pipe'] });
    let output = '', childPid;
    const closed = new Promise((resolve) => runner.once('close', (code, signal) => resolve({ code, signal })));
    runner.stdout.on('data', (data) => { output += data; });
    const lines = createInterface({ input: runner.stdout });
    t.after(async () => {
      if (childPid) try { process.kill(-childPid, 'SIGKILL'); } catch (error) { if (error.code !== 'ESRCH') throw error; }
      if (runner.exitCode === null && runner.signalCode === null) runner.kill('SIGKILL');
      await closed;
      lines.close();
    });
    await new Promise((resolve) => lines.on('line', (line) => {
      if (line.startsWith('pid:')) { childPid = Number(line.slice(4)); resolve(); }
    }));
    runner.kill(signal);
    const result = await closed;
    assert.match(output, /drained/);
    assert.match(output, /"signal":"SIGKILL"/);
    assert.deepEqual(result, { code: signal === 'SIGINT' ? 130 : 143, signal: null });
    assert.throws(() => process.kill(childPid, 0), { code: 'ESRCH' });
  });
}
