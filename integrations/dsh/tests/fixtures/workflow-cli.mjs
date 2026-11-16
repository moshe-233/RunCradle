import { writeFile, readFile, rename, mkdir } from 'node:fs/promises';
import { join, dirname } from 'node:path';
import { createHash } from 'node:crypto';
export const id = '1'.repeat(64), revision = 'a'.repeat(64);
export const document = { format: 'yaml', source: 'processes: {}\n', entry: 'flow.yaml', ui_path: '', revision, valid: true, issues: [], run_supported: true, execution_note: 'Static only', workflow: { workflow_id: id, revision, revisions: [revision], title: 'Flow', updated_at: '2026-11-11T00:00:00Z' } };
export function fixtureRunner(mode = 'success') {
  const calls = [];
  return { calls, async run(args, cwd) {
    calls.push(args);
    const out = args[args.indexOf('--out') + 1]; const output = join(cwd, out); const dir = dirname(output);
    if (args.includes('--input')) JSON.parse(await readFile(join(cwd, args[args.indexOf('--input') + 1]), 'utf8'));
    if (mode === 'lost') throw new Error('reply lost after submission');
    if (mode === 'directory') { await rename(dir, dir + '-moved'); await mkdir(dir); await writeFile(output, 'replacement'); return { exitCode: 0, receipt: {} }; }
    const status = typeof mode === 'number' ? mode : 201;
    const body = status >= 400 ? { status, code: status === 409 ? 'conflict' : 'invalid_request', error: 'injected failure', next_action: 'inspect' } : document;
    let raw = Buffer.from(JSON.stringify(body)); if (mode === 'invalid-utf8') raw = Buffer.from([0xc3, 0x28]);
    if (mode === 'non-json') raw = Buffer.from('not json');
    await writeFile(output, raw, { flag: 'wx', mode: 0o600 });
    if (mode === 'cleanup') await writeFile(join(dir, 'foreign'), 'keep');
    const receipt = { status: status >= 400 ? 'error' : 'ok', contract_version: '1.0.0', http_status: status, content_type: 'application/json', output: out, bytes: raw.length, sha256: createHash('sha256').update(raw).digest('hex') };
    if (mode === 'digest') receipt.sha256 = '0'.repeat(64);
    if (mode === 'oversize') receipt.bytes = 32 * 1024 * 1024 + 1;
    if (mode === 'status') receipt.http_status = 999;
    return { exitCode: status >= 400 ? 2 : 0, receipt };
  } };
}
