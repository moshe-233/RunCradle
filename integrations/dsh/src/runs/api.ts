import type { Access } from './access.js';
import type { CliRunner } from './runner.js';
import type { ObjectValue } from './contract.js';
import { requestJSON } from '../shared/api.js';

/** Keep the run API's GET signature and existing receipt/error contract. */
export async function apiRequest(runner: Pick<CliRunner, 'run'>, access: Access, resource: string, signal: AbortSignal): Promise<ObjectValue> {
  return (await requestJSON(runner, access, { method: 'GET', path: resource, mutating: false }, signal, true)).body;
}
