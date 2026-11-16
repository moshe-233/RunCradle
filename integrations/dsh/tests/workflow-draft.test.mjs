import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtemp, mkdir, readFile, readdir, writeFile, rm, symlink, rename, stat, open } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
const draftModule = await import('../lib/workflows/draft.js').catch(() => ({}));
const sha = (v) => createHash('sha256').update(v).digest('hex');
const signal = () => new AbortController().signal;
async function setup(t) { const workspace = await mkdtemp(join(tmpdir(), 'workflow-draft-')); t.after(() => rm(workspace, { recursive: true, force: true })); return { workspace, async recheck() {} }; }
const draft = { format: 'yaml', source: '# original\r\nprocesses: {}\r\n', entry: 'flow.yaml' };
test('round-trips large draft beyond tool budget and across module reuse', async (t) => {
  assert.equal(typeof draftModule.publishDraft, 'function');
  const access = await setup(t); const large = { ...draft, source: '#'.repeat(1000000) + draft.source, ui_source: '{"n":9223372036854775807}', ui_path: 'ui.json' };
  const artifact = await draftModule.publishDraft(access, 'draft.json', large, signal());
  const bytes = await readFile(join(access.workspace, 'draft.json'));
  assert.equal(artifact.draft_sha256, sha(bytes)); assert.equal(artifact.draft_bytes, bytes.length); assert.equal((await stat(join(access.workspace, 'draft.json'))).mode & 0o777, 0o600);
  const read = await draftModule.loadDraft(access, artifact, signal()); assert.deepEqual(read.draft, large); assert.ok(Object.isFrozen(read.draft));
  assert.deepEqual(await readdir(access.workspace), ['draft.json']);
});
test('exclusive publish never replaces occupied destinations or follows parent symlinks', async (t) => {
  assert.equal(typeof draftModule.publishDraft, 'function');
  const a = await setup(t); await mkdir(join(a.workspace, 'parent')); await writeFile(join(a.workspace, 'keep'), 'mine');
  await symlink('keep', join(a.workspace, 'alias')); await symlink('parent', join(a.workspace, 'parent-alias'));
  for (const name of ['keep', 'alias', 'missing/out', 'parent-alias/out']) await assert.rejects(draftModule.publishDraft(a, name, draft, signal()));
  assert.equal(await readFile(join(a.workspace, 'keep'), 'utf8'), 'mine');
  assert.deepEqual((await readdir(a.workspace)).sort(), ['alias', 'keep', 'parent', 'parent-alias']);
});
test('rejects substituted, private, oversized, nonregular and invalid UTF-8 draft inputs', async (t) => {
  assert.equal(typeof draftModule.loadDraft, 'function');
  const a = await setup(t); await mkdir(join(a.workspace, '.runcradle')); await mkdir(join(a.workspace, '.git'));
  await writeFile(join(a.workspace, '.runcradle/secret'), 'secret'); await symlink('.runcradle/secret', join(a.workspace, 'alias'));
  execFileSync('mkfifo', [join(a.workspace, 'fifo')]);
  const cases = [['bad', Buffer.from([0xc3, 0x28])], ['large', Buffer.alloc(8 * 1024 * 1024 + 1)], ['valid', Buffer.from(JSON.stringify(draft))]];
  for (const [name, bytes] of cases) await writeFile(join(a.workspace, name), bytes);
  for (const name of ['.runcradle/secret', '.git', 'alias', 'fifo', 'bad', 'large', 'valid']) await assert.rejects(draftModule.loadDraft(a, { draft_file: name, draft_sha256: 'a'.repeat(64) }, signal()));
  const bytes = cases[2][1]; let checks = 0;
  a.recheck = async () => { if (++checks === 2) await writeFile(join(a.workspace, 'valid'), JSON.stringify({ ...draft, source: 'replacement' })); };
  await assert.rejects(draftModule.loadDraft(a, { draft_file: 'valid', draft_sha256: sha(bytes) }, signal()));
});
test('parent replacement and competing output creation do not overwrite other objects', async (t) => {
  assert.equal(typeof draftModule.publishDraft, 'function');
  for (const action of ['replace', 'occupy']) {
    const a = await setup(t); await mkdir(join(a.workspace, 'p')); let checks = 0;
    a.recheck = async () => { if (++checks === 2) { if (action === 'replace') { await rename(join(a.workspace, 'p'), join(a.workspace, 'moved')); await mkdir(join(a.workspace, 'p')); } await writeFile(join(a.workspace, 'p/out'), 'mine'); } };
    await assert.rejects(draftModule.publishDraft(a, 'p/out', draft, signal()));
    assert.equal(await readFile(join(a.workspace, 'p/out'), 'utf8'), 'mine');
  }
});
test('publication sync failure reports uncertain artifact and preserves the published draft', async (t) => {
  assert.equal(typeof draftModule.publishDraft, 'function');
  const a = await setup(t); const sample = await open(a.workspace); const proto = Object.getPrototypeOf(sample); const sync = proto.sync;
  t.mock.method(proto, 'sync', async function () { if ((await this.stat()).isDirectory()) throw new Error('injected directory sync failure'); return sync.call(this); });
  await sample.close();
  await assert.rejects(draftModule.publishDraft(a, 'out', draft, signal()), (e) => e.artifactState === 'unknown');
  assert.deepEqual(JSON.parse(await readFile(join(a.workspace, 'out'), 'utf8')), draft);
});
