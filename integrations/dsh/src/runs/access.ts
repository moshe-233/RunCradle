import type { Context } from '@deepseek-ai/cordis';
import type { ToolRunContext } from '@deepseek-ai/dsh-tools';
import type { Input, Operation } from './contract.js';
import { resolveAccess, type Access } from '../shared/access.js';
export type { Access } from '../shared/access.js';

/** Keep the established run permission and approval wording unchanged. */
export function authorize(ctx: Context, exec: ToolRunContext, operation: Operation, input: Input): Promise<Access> {
  const mutation = operation === 'run' || operation === 'stop';
  const effect = mutation
    ? 'Control workflow execution in an external RunCradle host using its host-user permissions; this does not sandbox the workflow.'
    : 'Query RunCradle history; inspection may expire retained diagnostics and online queries create temporary response files.';
  return resolveAccess(ctx, exec, {
    toolName: `runcradle_${operation}`, workspace: input.workspace,
    inputFiles: [input.file, input.ui, input.values_file].filter((v): v is string => v !== undefined), outputFiles: [],
    requiresRestrictedApproval: mutation, forbidPrivatePaths: false,
    reason: (workspace) => `${effect}\nWorkspace: ${workspace}\nArguments: ${JSON.stringify(input)}`,
  });
}
