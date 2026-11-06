import assert from 'node:assert/strict';
import { cp, mkdir, readFile, rm, symlink } from 'node:fs/promises';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { Context } from '@deepseek-ai/cordis';
import { ToolRuntime } from '@deepseek-ai/dsh-tools';
import * as NativeExecutor from '@deepseek-ai/dsh-tool-executor-native';
import { SkillRegistry } from '@deepseek-ai/dsh-skill';
import SystemPrompt from '@deepseek-ai/dsh-system-prompt';
import { manifest, packageRoot, pack, workspace } from './support.mjs';

const work = await workspace('package');
try {
  const tarball = await pack(work);
  const listing = await work.run('contents', 'tar', ['-tzf', tarball]);
  const entries = listing.stdout.trim().split('\n');
  for (const required of ['package.json', 'cordis.patch.yml', 'lib/index.js', 'lib/index.d.ts', 'assets/SKILL.md', 'README.md']) {
    assert.ok(entries.includes(`package/${required}`), `missing packed ${required}`);
  }
  assert.ok(entries.every((name) => /^package\/(lib\/|assets\/|package.json$|cordis.patch.yml$|README.md$|LICENSE(?:\.md)?$)/.test(name)), 'unexpected file in package');
  const unpack = join(work.dir, 'unpacked');
  await mkdir(unpack);
  await work.run('extract', 'tar', ['-xzf', tarball, '-C', unpack]);
  const extracted = join(unpack, 'package');
  // Package tests borrow development dependencies. CLI smoke separately installs the actual tgz.
  await symlink(join(packageRoot, 'node_modules'), join(extracted, 'node_modules'), 'dir');
  const packedManifest = JSON.parse(await readFile(join(extracted, 'package.json'), 'utf8'));
  assert.equal(packedManifest.engines.dsh, '0.2.2-rc.1');
  assert.equal(packedManifest.dsh.bundle.patch, './cordis.patch.yml');
  assert.equal(packedManifest.exports['.'].default, './lib/index.js');
  const ctx = new Context();
  try {
    await ctx.plugin(SystemPrompt);
    await ctx.plugin(ToolRuntime, {});
    await ctx.plugin(NativeExecutor);
    await ctx.plugin(SkillRegistry);
    const host = await import(pathToFileURL(join(extracted, 'lib/index.js')).href);
    await ctx.plugin(host, {});
    const call = async (id, args) => {
      const batch = await ctx.tools.execute({
        calls: [{ type: 'tool-call', id, name: 'runcradle_probe', arguments: JSON.stringify(args) }],
        signal: new AbortController().signal,
      });
      assert.equal(batch.results.length, 1);
      assert.equal(batch.results[0].callId, id);
      return batch.results[0].result;
    };
    const result = await call('packed', { echo: '解包验证' });
    assert.equal(result.isError, false);
    assert.deepEqual(result.value, { plugin: manifest.name, version: manifest.version, echo: '解包验证' });
    assert.match((await ctx.skills.get('runcradle-entry')).content, /runcradle_probe/);
    await rm(join(extracted, 'assets/SKILL.md'));
    await assert.rejects(() => ctx.skills.get('runcradle-entry'), /ENOENT/);
    assert.equal((await call('missing-skill', {})).isError, false);
  } finally {
    await ctx.fiber.dispose();
  }
  const broken = join(work.dir, 'missing-entry');
  await cp(extracted, broken, { recursive: true, verbatimSymlinks: true });
  await rm(join(broken, 'lib/index.js'));
  await assert.rejects(() => import(pathToFileURL(join(broken, 'lib/index.js')).href), /ERR_MODULE_NOT_FOUND|Cannot find module/);
  work.records.push({ label: 'packed-contract-and-negative-fixtures', passed: true });
  await work.save();
  console.log('PASS: packed files, external-directory import/call/Skill, missing Skill and missing entry diagnostics');
} catch (error) {
  if (work.owner.interrupted) await work.owner.finished;
  work.records.push({ label: 'failure', passed: false, error: error.stack });
  await work.save();
  throw error;
} finally {
  work.owner.close();
}
