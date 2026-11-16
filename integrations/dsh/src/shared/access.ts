import { lstat, realpath, stat } from 'node:fs/promises';
import { isAbsolute, relative, resolve, sep } from 'node:path';
import type { Context } from '@deepseek-ai/cordis';
import type { ToolRunContext } from '@deepseek-ai/dsh-tools';
import type {} from '@deepseek-ai/dsh-working-directory';
import type {} from '@deepseek-ai/dsh-sandbox-policy';
import type {} from '@deepseek-ai/dsh-user-approval';
import { checkPath } from './artifacts.js';
import { dirname } from 'node:path';

export interface Access {
  workspace: string;
  recheck(): Promise<void>;
}

export interface AccessRequest {
  toolName: string;
  workspace?: string;
  inputFiles: string[];
  outputFiles: string[];
  requiresRestrictedApproval: boolean;
  reason: string | ((workspace: string) => string);
  forbidPrivatePaths: boolean;
}

function contains(parent: string, child: string): boolean {
  const rel = relative(parent, child);
  return rel === '' || (rel !== '..' && !rel.startsWith(`..${sep}`) && !isAbsolute(rel));
}

async function identity(path: string, directory: boolean): Promise<string> {
  const info = await stat(path, { bigint: true });
  if (directory ? !info.isDirectory() : !info.isFile()) throw new Error(directory ? 'Workspace must be a directory' : 'Inputs must be ordinary files');
  return directory ? `${info.dev}:${info.ino}` : `${info.dev}:${info.ino}:${info.size}:${info.mtimeNs}:${info.ctimeNs}`;
}

/** Metadata checks precede approval; no contents, processes or files are created. */
export async function resolveAccess(ctx: Context, exec: ToolRunContext, request: AccessRequest): Promise<Access> {
  exec.signal.throwIfAborted();
  if (!exec.agent) throw new Error('RunCradle business tools require an Agent context');
  const agent = exec.agent;
  const policyService = ctx.get('sandboxPolicy');
  if (!policyService) throw new Error('RunCradle requires the sandbox policy service');
  const cwd = ctx.workingDirectory.get(agent.session);
  const requested = resolve(cwd, request.workspace ?? '.');
  const workspace = await realpath(requested);
  const policy = policyService.resolve({ session: agent.session });
  const policyIdentity = JSON.stringify(policy);
  const roots: string[] = [];
  if (policy.mode !== 'danger-full-access') {
    roots.push(await realpath(policy.workspaceRoot));
    for (const root of policy.additionalWritableRoots ?? []) {
      if (!isAbsolute(root) || !(await lstat(root)).isDirectory()) throw new Error('Invalid additional writable root');
      roots.push(await realpath(root));
    }
  }
  const workspaceIdentity = await identity(workspace, true);
  const files = [] as { path: string; canonical: string; identity: string }[];
  for (const file of request.inputFiles) {
    if (file === undefined) continue;
    const path = resolve(workspace, file);
    const canonical = await realpath(path);
    if (!contains(workspace, path) || !contains(workspace, canonical)) throw new Error('Input must remain inside the workspace');
    if (request.forbidPrivatePaths) { checkPath(workspace, path); checkPath(workspace, canonical); }
    files.push({ path, canonical, identity: await identity(canonical, false) });
  }
  const parents: { path: string; identity: string }[] = [];
  for (const output of request.outputFiles) {
    const path = resolve(workspace, output);
    checkPath(workspace, path);
    let parent = workspace;
    for (const component of relative(workspace, dirname(path)).split(sep).filter(Boolean)) {
      parent = resolve(parent, component);
      const info = await lstat(parent);
      if (!info.isDirectory() || info.isSymbolicLink()) throw new Error('Output parent must be an existing directory without symlinks');
      parents.push({ path: parent, identity: await identity(parent, true) });
    }
    try { await lstat(path); throw new Error('Output already exists'); }
    catch (error) { if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error; }
  }
  const needsApproval = policy.mode !== 'danger-full-access' && (
    request.requiresRestrictedApproval || policy.mode !== 'workspace-write' || !roots.some((root) => contains(root, workspace))
  );
  const recheck = async () => {
    exec.signal.throwIfAborted();
    if (!ctx.get('sandboxPolicy') || JSON.stringify(ctx.get('sandboxPolicy')!.resolve({ session: agent.session })) !== policyIdentity || ctx.workingDirectory.get(agent.session) !== cwd) throw new Error('Session directory or policy changed; invoke again');
    if (await realpath(requested) !== workspace || await identity(workspace, true) !== workspaceIdentity) throw new Error('Workspace identity changed; invoke again');
    for (const file of files) {
      if (await realpath(file.path) !== file.canonical || await identity(file.canonical, false) !== file.identity) throw new Error('Input identity changed; invoke again');
    }
    for (const parent of parents) {
      const info = await lstat(parent.path);
      if (!info.isDirectory() || info.isSymbolicLink() || await identity(parent.path, true) !== parent.identity) throw new Error('Output parent identity changed; invoke again');
    }
    exec.signal.throwIfAborted();
  };
  if (needsApproval) {
    const approval = ctx.get('approval');
    if (!approval) throw new Error('RunCradle operation requires DSH approval, but approval service is unavailable');
    const reason = typeof request.reason === 'function' ? request.reason(workspace) : `${request.reason}\nWorkspace: ${workspace}`;
    const answer = await approval.request({ agent, callId: exec.callId, toolName: request.toolName, signal: exec.signal, reason });
    if (answer !== 'allowed-once') throw new Error(`RunCradle approval was ${answer ?? 'unavailable'}`);
  }
  await recheck();
  return { workspace, recheck };
}
