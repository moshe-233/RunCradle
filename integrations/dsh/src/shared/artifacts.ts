import { constants, type BigIntStats } from 'node:fs';
import { open, lstat, realpath, link, unlink, type FileHandle } from 'node:fs/promises';
import { createHash, randomUUID } from 'node:crypto';
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from 'node:path';

export function contains(parent: string, child: string): boolean {
  const rel = relative(parent, child);
  return rel === '' || (rel !== '..' && !rel.startsWith(`..${sep}`) && !isAbsolute(rel));
}
export function checkPath(workspace: string, path: string): void {
  if (!contains(workspace, path) || relative(workspace, path).split(sep).some((v) => ['.runcradle', '.git', '.agents', '.codex', '.aws'].includes(v))) throw new Error('Path must remain inside workspace outside protected paths');
}
export const digest = (bytes: Buffer | string): string => createHash('sha256').update(bytes).digest('hex');
export const sameObject = (a: BigIntStats, b: BigIntStats): boolean => a.dev === b.dev && a.ino === b.ino;
export const sameFile = (a: BigIntStats, b: BigIntStats): boolean => sameObject(a, b) && a.size === b.size && a.mtimeNs === b.mtimeNs && a.ctimeNs === b.ctimeNs;
export const fdPath = (handle: FileHandle, name: string): string => `/proc/self/fd/${handle.fd}/${name}`;

/** Linux directory-FD walk: no component may be a symlink, even inside the workspace. */
export async function pinDirectory(path: string): Promise<{ handle: FileHandle; check(): Promise<void>; close(): Promise<void> }> {
  const handles: { handle: FileHandle; path: string; identity: BigIntStats }[] = [];
  try {
    const root = await open('/', constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW);
    handles.push({ handle: root, path: '/', identity: await root.stat({ bigint: true }) });
    let visible = '/';
    for (const part of resolve(path).split('/').filter(Boolean)) {
      const handle = await open(fdPath(handles.at(-1)!.handle, part), constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW);
      visible = join(visible, part);
      handles.push({ handle, path: visible, identity: await handle.stat({ bigint: true }) });
    }
    return {
      handle: handles.at(-1)!.handle,
      async check() { for (const entry of handles) { const now = await lstat(entry.path, { bigint: true }); if (!now.isDirectory() || !sameObject(now, entry.identity)) throw new Error('Directory identity changed'); } },
      async close() { for (const entry of handles.toReversed()) await entry.handle.close(); },
    };
  } catch (error) { await Promise.allSettled(handles.map((v) => v.handle.close())); throw error; }
}
export async function inputPath(workspace: string, name: string): Promise<string> {
  const path = resolve(workspace, name); checkPath(workspace, path);
  const canonical = await realpath(path); checkPath(workspace, canonical);
  return canonical;
}
export async function readBounded(path: string, max: number, signal: AbortSignal): Promise<Buffer> {
  signal.throwIfAborted();
  const parent = await pinDirectory(dirname(path));
  try {
    await parent.check();
    const name = fdPath(parent.handle, basename(path));
    const before = await lstat(name, { bigint: true });
    if (!before.isFile() || before.size > BigInt(max)) throw new Error('Input must be an ordinary file within the byte budget');
    const file = await open(name, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
    try {
      if (!sameFile(before, await file.stat({ bigint: true }))) throw new Error('Input identity changed');
      const raw = Buffer.alloc(Number(before.size) + 1); let size = 0;
      while (size < raw.length) { signal.throwIfAborted(); const result = await file.read(raw, size, raw.length - size, size); if (!result.bytesRead) break; size += result.bytesRead; }
      if (BigInt(size) !== before.size || !sameFile(before, await file.stat({ bigint: true })) || !sameFile(before, await lstat(name, { bigint: true }))) throw new Error('Input changed while reading');
      await parent.check(); signal.throwIfAborted();
      return raw.subarray(0, size);
    } finally { await file.close(); }
  } finally { await parent.close(); }
}
export function decodeUTF8(raw: Buffer): string { return new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(raw); }

export class ArtifactError extends Error {
  constructor(message: string, public artifactPath: string, public artifactState: 'not_created' | 'unknown') { super(message); }
}
/** Publish a complete fsynced file with link(2)'s create-if-absent semantics. */
export async function publishFile(workspace: string, output: string, raw: Buffer, signal: AbortSignal, recheck: () => Promise<void>): Promise<void> {
  const target = resolve(workspace, output); checkPath(workspace, target);
  await recheck(); signal.throwIfAborted();
  const parent = await pinDirectory(dirname(target));
  let temp: FileHandle | undefined, identity: BigIntStats | undefined, linked = false;
  const tempName = fdPath(parent.handle, `.dsh-runcradle-draft-${randomUUID()}`);
  let failure: unknown;
  try {
    await parent.check();
    temp = await open(tempName, constants.O_CREAT | constants.O_EXCL | constants.O_WRONLY | constants.O_NOFOLLOW, 0o600);
    identity = await temp.stat({ bigint: true });
    await temp.writeFile(raw, { signal }); await temp.sync();
    await recheck(); await parent.check(); signal.throwIfAborted();
    if (!sameObject(identity, await lstat(tempName, { bigint: true }))) throw new Error('Temporary artifact identity changed');
    await link(tempName, fdPath(parent.handle, basename(target))); linked = true;
    if (!sameObject(identity, await lstat(target, { bigint: true }))) throw new Error('Published artifact identity changed');
    await parent.check(); signal.throwIfAborted();
    await parent.handle.sync();
  } catch (error) { failure = error; }
  finally {
    try {
      if (identity) {
        const current = await lstat(tempName, { bigint: true });
        if (!sameObject(identity, current)) throw new Error('Temporary artifact ownership changed; retained');
        await unlink(tempName);
        await parent.handle.sync();
      }
    } catch (error) { failure = new Error(`${failure instanceof Error ? failure.message + '; ' : ''}Artifact cleanup uncertain: ${error instanceof Error ? error.message : error}`); }
    try { await temp?.close(); } catch (error) { failure ??= error; }
    try { await parent.close(); } catch (error) { failure ??= error; }
  }
  if (failure) throw new ArtifactError(failure instanceof Error ? failure.message : String(failure), output, linked ? 'unknown' : 'not_created');
}
