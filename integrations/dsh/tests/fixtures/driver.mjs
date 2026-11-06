import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { createInterface } from 'node:readline';

export const name = 'runcradle-entry-test-driver';
export const inject = ['tools', 'skills', 'agents', 'agentLoop', 'systemPrompt', 'loader'];
const prefix = '@@RUNCRADLE_TEST@@';

export function apply(ctx) {
  let reloads = 0;
  const handles = new Set();
  const input = createInterface({ input: process.stdin });
  ctx.on('app-boot/config-reload', () => { reloads++; });
  const send = (value) => process.stdout.write(prefix + JSON.stringify(value) + '\n');
  async function snapshot() {
    const entry = [...ctx.loader.entries()].find((entry) => entry.options.id === 'runcradle-host' && entry.options.name === 'dsh-runcradle');
    return {
      tools: ctx.tools.schemas().filter((tool) => tool.name === 'runcradle_probe').length,
      skills: (await ctx.skills.list()).filter((skill) => skill.name === 'runcradle-entry').length,
      active: entry?.fiber?.state === 2,
      reloads,
    };
  }
  async function exercise() {
    const local = [];
    try {
      const allowed = await ctx.agents.create({ sessionId: randomUUID(), meta: { cwd: process.cwd() } });
      local.push(allowed); handles.add(allowed);
      const denied = await ctx.agents.create({
        sessionId: randomUUID(), meta: { cwd: process.cwd() },
        setup(agentCtx) { agentCtx.tools.restrict({ deny: ['runcradle_probe'] }); },
      });
      local.push(denied); handles.add(denied);
      const visible = (agent) => ctx.tools.schemas(agent).filter((tool) => tool.name === 'runcradle_probe').length;
      assert.equal(visible(allowed.agent), 1);
      assert.equal(visible(denied.agent), 0);
      const prompt = await ctx.systemPrompt.assemble({ scope: allowed.agent });
      assert.equal(prompt.tools.filter((tool) => tool.name === 'runcradle_probe').length, 1);
      const call = async (agent, args) => {
        const id = randomUUID();
        const batch = await ctx.tools.execute({
          agent,
          calls: [{ type: 'tool-call', id, name: 'runcradle_probe', arguments: JSON.stringify(args) }],
          signal: new AbortController().signal,
        });
        assert.equal(batch.results.length, 1);
        assert.equal(batch.results[0].callId, id);
        return batch.results[0].result;
      };
      for (const echo of [undefined, 'CLI entry', '你好，RunCradle']) {
        const result = await call(allowed.agent, echo === undefined ? {} : { echo });
        assert.equal(result.isError, false, JSON.stringify(result));
        assert.deepEqual(result.value, { plugin: 'dsh-runcradle', version: '0.1.0-alpha.0', echo: echo ?? '' });
      }
      const blocked = await call(denied.agent, {});
      assert.equal(blocked.isError, true);
      assert.equal(blocked.error.info.code, 'UNKNOWN_TOOL');
      assert.equal((await call(allowed.agent, {})).isError, false);
      const skill = await ctx.skills.get('runcradle-entry', { scope: allowed.agent });
      assert.match(skill.content, /runcradle_probe/);
      return { realAgents: 2, scopedVisibility: true, promptSchema: true, calls: true, denyIsolation: true, skill: true };
    } finally {
      for (const handle of local.reverse()) {
        await handle.dispose();
        handles.delete(handle);
      }
    }
  }
  let pending = Promise.resolve();
  input.on('line', (line) => {
    pending = pending.then(async () => {
      let request;
      try {
        request = JSON.parse(line);
        const result = request.method === 'snapshot' ? await snapshot()
          : request.method === 'exercise' ? await exercise()
          : await Promise.reject(new Error(`Unknown test method ${request.method}`));
        send({ id: request.id, result });
      } catch (error) {
        send({ id: request?.id, error: error.stack });
      }
    });
  });
  ctx.effect(() => async () => {
    input.close();
    await pending;
    for (const handle of handles) await handle.dispose();
    handles.clear();
    send({ disposed: true });
  });
  send({ ready: true });
}
