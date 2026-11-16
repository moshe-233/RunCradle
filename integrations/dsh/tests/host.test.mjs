import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { setTimeout as delay } from 'node:timers/promises';
import test from 'node:test';
import { Context } from '@deepseek-ai/cordis';
import { ToolRuntime, defineTool } from '@deepseek-ai/dsh-tools';
import * as NativeExecutor from '@deepseek-ai/dsh-tool-executor-native';
import { SkillRegistry } from '@deepseek-ai/dsh-skill';
import SystemPrompt from '@deepseek-ai/dsh-system-prompt';
import * as host from '../lib/index.js';

const { version } = JSON.parse(await readFile(new URL('../package.json', import.meta.url), 'utf8'));

async function eventually(check) {
  const deadline = Date.now() + 2000;
  while (!(await check())) {
    assert.ok(Date.now() < deadline, 'lifecycle did not settle');
    await delay(10);
  }
}

async function fixture(t, { tools = true, skills = false } = {}) {
  const ctx = new Context();
  t.after(() => ctx.fiber.dispose());
  await ctx.plugin(SystemPrompt);
  if (tools) await ctx.plugin(ToolRuntime, {});
  await ctx.plugin(NativeExecutor);
  if (skills) await ctx.plugin(SkillRegistry);
  return ctx;
}

async function call(ctx, args = {}, signal = new AbortController().signal, name = 'runcradle_probe') {
  const batch = await ctx.tools.execute({
    calls: [{ type: 'tool-call', id: 'probe-test', name, arguments: JSON.stringify(args) }],
    signal,
  });
  assert.equal(batch.results.length, 1);
  assert.equal(batch.results[0].callId, 'probe-test');
  return batch.results[0].result;
}

test('probe returns installed version and preserves absent, ordinary and Unicode echo', async (t) => {
  const ctx = await fixture(t);
  await ctx.plugin(host, {});
  assert.equal(ctx.tools.schemas().filter((tool) => tool.name === 'runcradle_probe').length, 1);
  for (const echo of [undefined, '', 'entry ready', '你好，RunCradle']) {
    const result = await call(ctx, echo === undefined ? {} : { echo });
    assert.equal(result.isError, false);
    assert.deepEqual(result.value, { plugin: 'dsh-runcradle', version, echo: echo ?? '' });
    assert.deepEqual(JSON.parse(result.content[0].text), result.value);
  }
});

test('invalid argument types become pipeline errors', async (t) => {
  const ctx = await fixture(t);
  await ctx.plugin(host, {});
  for (const args of [{ echo: 3 }, { echo: null }, [], null]) {
    const result = await call(ctx, args);
    assert.equal(result.isError, true);
    assert.equal(result.error.info.code, 'INVALID_ARGS');
    assert.equal(result.value, undefined);
  }
});

test('pre-cancelled dispatch never reports a successful probe', async (t) => {
  const ctx = await fixture(t);
  await ctx.plugin(host, {});
  const result = await call(ctx, {}, AbortSignal.abort());
  assert.equal(result.isError, true);
  assert.equal(result.error.info.code, 'ABORTED_BEFORE_DISPATCH');
});

test('real pipeline rejects an invalid output fixture', async (t) => {
  const ctx = await fixture(t);
  await ctx.inject(['tools'], (inner) => {
    inner.tools.register(defineTool({
      name: 'invalid_output_fixture', description: 'Test only', parameters: {},
      output: { schema: { type: 'string' }, render: () => [] },
      execute: async () => 42,
    }));
  });
  const result = await call(ctx, {}, new AbortController().signal, 'invalid_output_fixture');
  assert.equal(result.isError, true);
  assert.equal(result.value, undefined);
  assert.equal(result.error.info.code, 'INVALID_TOOL_OUTPUT');
});

test('default Skill is discoverable and readable; false config omits it', async (t) => {
  const ctx = await fixture(t, { skills: true });
  const plugin = await ctx.plugin(host, {});
  await eventually(async () => (await ctx.skills.list()).length === 1);
  const skill = await ctx.skills.get('runcradle');
  assert.ok(skill, 'RunCradle must be discoverable by its public skill name');
  assert.equal(skill.provider, 'runcradle');
  assert.equal(skill.source, 'bundled');
  assert.match(skill.content, /runcradle_probe/);
  assert.match(skill.content, /runcradle_run/);
  assert.match(skill.content, /runcradle_inspect/);
  assert.equal(skill.invocation.modelInvocable, true);
  await plugin.dispose();
  assert.deepEqual(await ctx.skills.list(), []);
  await ctx.plugin(host, { enableSkill: false });
  assert.deepEqual(await ctx.skills.list(), []);
  assert.equal((await call(ctx)).isError, false);
});

test('invalid config fails before registering contributions', async (t) => {
  const ctx = await fixture(t, { skills: true });
  await assert.rejects(async () => { await ctx.plugin(host, { enableSkill: 'yes' }); }, /boolean/i);
  assert.deepEqual(ctx.tools.schemas(), []);
  assert.deepEqual(await ctx.skills.list(), []);
});

test('missing, late, withdrawn and restored skills service does not disable tools', async (t) => {
  const ctx = await fixture(t);
  const plugin = await ctx.plugin(host, {});
  assert.equal((await call(ctx)).isError, false);
  for (let i = 0; i < 3; i++) {
    const skills = await ctx.plugin(SkillRegistry);
    await eventually(async () => (await ctx.skills.list()).length === 1);
    assert.match((await ctx.skills.get('runcradle')).content, /runcradle_probe/);
    await skills.dispose();
    assert.equal((await call(ctx)).isError, false);
  }
  await plugin.dispose();
  assert.deepEqual(ctx.tools.schemas(), []);
});

test('Host waits for tools and activates once the required service arrives', async (t) => {
  const ctx = await fixture(t, { tools: false });
  const plugin = await ctx.plugin(host, {});
  assert.equal(plugin.state, 0, 'Cordis PENDING');
  assert.ok(Object.hasOwn(plugin.inject, 'tools'));
  await ctx.plugin(ToolRuntime, {});
  await eventually(() => plugin.state === 2);
  assert.equal((await call(ctx)).isError, false);
});

test('duplicate tool registration fails without replacing the live contribution', async (t) => {
  const ctx = await fixture(t);
  const first = await ctx.plugin(host, {});
  await assert.rejects(async () => { await ctx.plugin(host, {}); }, /duplicate|already/i);
  assert.equal(ctx.tools.schemas().length, 1);
  assert.equal((await call(ctx)).isError, false);
  await first.dispose();
  assert.deepEqual(ctx.tools.schemas(), []);
});

test('three mount/dispose cycles return both registries to baseline', async (t) => {
  const ctx = await fixture(t, { skills: true });
  for (let i = 0; i < 3; i++) {
    const plugin = await ctx.plugin(host, {});
    await eventually(async () => (await ctx.skills.list()).length === 1);
    assert.equal(ctx.tools.schemas().length, 1);
    assert.equal((await call(ctx)).isError, false);
    await plugin.dispose();
    assert.deepEqual(ctx.tools.schemas(), []);
    assert.deepEqual(await ctx.skills.list(), []);
    assert.equal((await call(ctx)).error.info.code, 'UNKNOWN_TOOL');
  }
});

test('config reload toggles only the optional Skill without accumulating tools', async (t) => {
  const ctx = await fixture(t, { skills: true });
  const plugin = await ctx.plugin(host, {});
  for (const enableSkill of [false, true, false, true]) {
    await plugin.update({ enableSkill });
    await plugin.await();
    await eventually(async () => (await ctx.skills.list()).length === (enableSkill ? 1 : 0));
    assert.equal(ctx.tools.schemas().length, 1);
    assert.equal((await call(ctx)).isError, false);
  }
});

test('configured tools wait for optional services and unload without touching a workspace', async (t) => {
  const ctx = await fixture(t);
  const plugin = await ctx.plugin(host, { runcradleExecutable: '/test/runcradle' });
  assert.equal(ctx.tools.schemas().length, 1);
  ctx.provide('subprocess'); ctx.provide('workingDirectory');
  ctx.set('subprocess', { resolveExecutable() { throw new Error('must not resolve on load'); }, spawn() { throw new Error('must not spawn on load'); } });
  ctx.set('workingDirectory', { get() { throw new Error('must not read on load'); } });
  await eventually(() => ctx.tools.schemas().length === 11);
  const names = ctx.tools.schemas().map((v) => v.name).sort();
  assert.deepEqual(names, ['runcradle_inspect', 'runcradle_probe', 'runcradle_run', 'runcradle_runs', 'runcradle_stop', 'runcradle_workflow_delete', 'runcradle_workflow_get', 'runcradle_workflow_import', 'runcradle_workflow_save', 'runcradle_workflow_validate', 'runcradle_workflows']);
  const missingAgent = await call(ctx, {}, new AbortController().signal, 'runcradle_runs');
  assert.equal(missingAgent.value.ok, false);
  assert.match(missingAgent.value.error, /Agent/);
  await plugin.dispose();
  assert.equal(ctx.tools.schemas().length, 0);
  await assert.rejects(async () => { await ctx.plugin(host, { runcradleExecutable: 'relative' }); }, /absolute/);
});

test('failed business calls also fit the 64 KiB tool result budget', async (t) => {
  const ctx = await fixture(t);
  ctx.provide('subprocess'); ctx.provide('workingDirectory');
  ctx.set('subprocess', { resolveExecutable() { throw new Error('must not resolve'); }, spawn() { throw new Error('must not spawn'); } });
  ctx.set('workingDirectory', {});
  await ctx.plugin(host, { runcradleExecutable: '/test/runcradle' });
  await eventually(() => ctx.tools.schemas().length === 11);
  for (const workspace of ['a'.repeat(70000), '\u0001'.repeat(20000), '中'.repeat(30000)]) {
    const result = await call(ctx, { workspace }, new AbortController().signal, 'runcradle_runs');
    assert.equal(result.value.ok, false);
    assert.ok(Buffer.byteLength(JSON.stringify(result.value)) <= 65536);
    assert.ok(Buffer.byteLength(result.content[0].text) <= 65536);
  }
});
