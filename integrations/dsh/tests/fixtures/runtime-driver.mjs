import { randomUUID } from 'node:crypto';
import { createInterface } from 'node:readline';

export const name = 'runcradle-runtime-test-driver';
export const inject = ['tools', 'agents', 'agentLoop', 'sandboxPolicy', 'approval', 'workingDirectory', 'subprocess', 'loader'];
export function apply(ctx) {
  const handles = new Map();
  const controllers = new Map();
  const input = createInterface({ input: process.stdin });
  const send = (value) => process.stdout.write('@@RUNCRADLE_RUNTIME@@' + JSON.stringify(value) + '\n');
  async function dispatch(request) {
    const { method, params: p = {} } = request;
    if (method === 'create') {
      const handle = await ctx.agents.create({ sessionId: randomUUID(), meta: { cwd: p.cwd } });
      handle.agent.session.append('sandbox/mode', { mode: p.mode ?? 'danger-full-access' });
      let approvals = 0;
      handle.agent.ctx.on('approval/request', async (_request, next) => { approvals++; if (p.approval === 'pending') return new Promise(() => {}); return p.approval ?? next(); });
      handles.set(p.key, { handle, turn: 0, approvals: () => approvals });
      return ctx.tools.schemas(handle.agent).filter((tool) => tool.name.startsWith('runcradle_')).map((tool) => tool.name);
    }
    if (method === 'cancel') { controllers.get(p.key)?.abort(); return true; }
    if (method === 'dispose') { await handles.get(p.key).handle.dispose(); handles.delete(p.key); return true; }
    if (method === 'configure') {
      const entry = [...ctx.loader.entries()].find((item) => item.options.id === 'runcradle-host');
      await entry.fiber.update(p.config); await entry.fiber.await();
      return ctx.tools.schemas().filter((tool) => tool.name.startsWith('runcradle_')).map((tool) => tool.name);
    }
    if (method === 'call') {
      const item = handles.get(p.key); const agent = item.handle.agent;
      const turn = ++item.turn;
      agent.session.append('turn/start', { turn });
      const controller = new AbortController(); controllers.set(p.key, controller);
      const timer = p.cancel_ms === undefined ? undefined : setTimeout(() => controller.abort(), p.cancel_ms);
      try {
        const batch = await ctx.tools.execute({ agent, calls: [{ type: 'tool-call', id: randomUUID(), name: p.name, arguments: JSON.stringify(p.args ?? {}) }], signal: controller.signal });
        return { result: batch.results[0].result, approvals: item.approvals() };
      } finally { controllers.delete(p.key); clearTimeout(timer); agent.session.append('turn/end', { turn, reason: { kind: 'completed' } }); }
    }
    throw new Error(`Unknown method: ${method}`);
  }
  const pending = new Set();
  input.on('line', (line) => {
    const work = (async () => {
      let request;
      try { request = JSON.parse(line); send({ id: request.id, result: await dispatch(request) }); }
      catch (error) { send({ id: request?.id, error: error.stack }); }
    })();
    pending.add(work); void work.finally(() => pending.delete(work));
  });
  ctx.effect(() => async () => {
    input.close(); await Promise.allSettled([...pending]);
    for (const item of handles.values()) await item.handle.dispose();
    handles.clear(); send({ disposed: true });
  });
  send({ ready: true });
}
