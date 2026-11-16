import { constants, type BigIntStats } from 'node:fs';
import { mkdir, open, lstat, unlink, rmdir, type FileHandle } from 'node:fs/promises';
import { randomUUID } from 'node:crypto';
import { join } from 'node:path';
import type { Access } from './access.js';
import { pinDirectory, fdPath, sameObject, readBounded, decodeUTF8, digest } from './artifacts.js';
import { CliRunner, ClientError } from '../runs/runner.js';
import { RESPONSE_BYTES, parseReceipt, type ObjectValue } from '../runs/contract.js';

export type APIRequest = { method: 'GET' | 'POST' | 'PUT' | 'DELETE'; path: string; bodyText?: string; mutating: boolean };
export type APIJSONResult = { body: ObjectValue; httpStatus: number };
export class APIRequestError extends ClientError {
  dispatched = false;
  httpStatus?: number;
  body?: ObjectValue;
  temporaryPath?: string;
}
/** Public CLI bridge. No credential or library storage reads; only call-owned files. */
export async function requestJSON(runner: Pick<CliRunner, 'run'>, access: Access, request: APIRequest, signal: AbortSignal, legacy = false): Promise<APIJSONResult> {
  let dispatched = false, parent: Awaited<ReturnType<typeof pinDirectory>> | undefined, owned: FileHandle | undefined;
  let directoryIdentity: BigIntStats | undefined, directory: string | undefined, leaf: string | undefined;
  let body: ObjectValue | undefined, httpStatus: number | undefined, receipt: ObjectValue | undefined;
  let failure: unknown, cleanupFailure: unknown, retain = false;
  const files = new Map<string, BigIntStats>();
  try {
    await access.recheck(); signal.throwIfAborted();
    if (!['GET', 'POST', 'PUT', 'DELETE'].includes(request.method) || !request.path.startsWith('/api/v1/') || request.path.includes('#') || request.path.includes('\0')) throw new Error('Invalid API request');
    if ((request.method === 'GET') !== (request.bodyText === undefined)) throw new Error('GET has no body; POST/PUT/DELETE require a JSON object');
    if (request.bodyText !== undefined && (Buffer.byteLength(request.bodyText) > 8 * 1024 * 1024 || !request.bodyText.trimStart().startsWith('{'))) throw new Error('Invalid API body budget or shape');
    parent = await pinDirectory(access.workspace); await parent.check();
    leaf = `.dsh-runcradle-call-${randomUUID()}`; directory = join(access.workspace, leaf);
    await mkdir(fdPath(parent.handle, leaf), { mode: 0o700 });
    owned = await open(fdPath(parent.handle, leaf), constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW);
    directoryIdentity = await owned.stat({ bigint: true });
    const verify = async () => {
      await parent!.check();
      const current = await lstat(directory!, { bigint: true });
      if (!current.isDirectory() || !sameObject(current, directoryIdentity!)) throw new Error('Owned response directory identity changed');
    };
    const output = `${leaf}/response.json`;
    const args = ['api', 'request', '--workspace', access.workspace, '--path', request.path, '--out', output, '--timeout-ms', '6000'];
    if (request.method !== 'GET') {
      const input = await open(fdPath(owned, 'input.json'), constants.O_CREAT | constants.O_EXCL | constants.O_WRONLY | constants.O_NOFOLLOW, 0o600);
      try { files.set('input.json', await input.stat({ bigint: true })); await input.writeFile(request.bodyText!, { signal }); await input.sync(); }
      finally { await input.close(); }
      args.push('--method', request.method, '--input', `${leaf}/input.json`);
    }
    await access.recheck(); await verify(); signal.throwIfAborted();
    dispatched = true;
    const result = await runner.run(args, access.workspace, signal); receipt = result.receipt;
    await verify();
    try { const info = await lstat(fdPath(owned, 'response.json'), { bigint: true }); if (info.isFile()) files.set('response.json', info); }
    catch (error) { if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error; }
    if (legacy && result.exitCode !== 0) throw new ClientError(String(receipt.code ?? 'api_error'), String(receipt.error ?? 'API request failed'), 'confirmed', receipt);
    if (receipt.output === undefined && result.exitCode !== 0) throw new ClientError(String(receipt.code ?? 'api_error'), String(receipt.error ?? 'API request failed'), 'confirmed', receipt);
    const status = legacy ? Number(receipt.http_status ?? 200) : Number(receipt.http_status);
    if (!Number.isSafeInteger(status) || status < 200 || status > 599 || (!legacy && (receipt.contract_version !== '1.0.0' || typeof receipt.content_type !== 'string' || !/^application\/json(?:;|$)/i.test(receipt.content_type))) || ((result.exitCode === 0) !== (status < 400))) throw new Error('Invalid HTTP status or API receipt');
    if (receipt.output !== output || !Number.isSafeInteger(receipt.bytes) || Number(receipt.bytes) < 0 || Number(receipt.bytes) > RESPONSE_BYTES || typeof receipt.sha256 !== 'string' || !/^[a-f0-9]{64}$/.test(receipt.sha256)) throw new Error('Invalid API response receipt');
    const raw = await readBounded(join(directory, 'response.json'), RESPONSE_BYTES, signal);
    if (raw.length !== receipt.bytes || digest(raw) !== receipt.sha256) throw new Error('API response size/digest differs from receipt');
    const parsed = parseReceipt(decodeUTF8(raw));
    await verify();
    if (status >= 400 && (parsed.status !== status || typeof parsed.code !== 'string' || typeof parsed.error !== 'string' || typeof parsed.next_action !== 'string')) throw new Error('Invalid API Problem response');
    body = parsed; httpStatus = status;
    if (status >= 400) throw new ClientError(String(parsed.code), String(parsed.error), 'confirmed', receipt);
  } catch (error) { failure = error; }
  finally {
    retain = (failure instanceof ClientError && failure.clientCleanup === 'unknown') || (request.mutating && dispatched && body === undefined);
    try {
      if (owned && directoryIdentity && directory && parent && leaf) {
        if (retain) throw new Error('Response evidence retained; submission or client cleanup unconfirmed');
        for (const [name, identity] of files) {
          const current = await lstat(fdPath(owned, name), { bigint: true });
          if (!current.isFile() || !sameObject(identity, current)) throw new Error('Response file ownership changed');
          await unlink(fdPath(owned, name));
        }
        await parent.check();
        const current = await lstat(fdPath(parent.handle, leaf), { bigint: true });
        if (!current.isDirectory() || !sameObject(current, directoryIdentity)) throw new Error('Owned response directory identity changed');
        await rmdir(fdPath(parent.handle, leaf));
      }
    } catch (error) { cleanupFailure = error; }
    try { await owned?.close(); } catch (error) { cleanupFailure ??= error; }
    try { await parent?.close(); } catch (error) { cleanupFailure ??= error; }
  }
  if (failure || cleanupFailure) {
    const message = [failure, cleanupFailure].filter(Boolean).map((e) => e instanceof Error ? e.message : String(e)).join('; ');
    const error = new APIRequestError(cleanupFailure && !retain ? 'cleanup_uncertain' : failure instanceof ClientError ? failure.code : 'api_error', message,
      failure instanceof ClientError ? failure.clientCleanup : 'confirmed', receipt);
    Object.assign(error, { dispatched, body, httpStatus, temporaryPath: cleanupFailure ? directory : undefined });
    throw error;
  }
  return { body: body!, httpStatus: httpStatus! };
}
