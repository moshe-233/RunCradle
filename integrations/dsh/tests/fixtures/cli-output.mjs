import { createHash } from 'node:crypto';
import { writeFile } from 'node:fs/promises';
const mode = process.argv[2];
if (mode === 'hang') setInterval(() => {}, 1000);
else if (mode === 'truncated') process.stdout.write('x'.repeat(5000) + '{}');
else if (mode === 'malformed') process.stdout.write('not-json');
else if (mode === 'signal') process.kill(process.pid, 'SIGTERM');
else if (mode === 'api') {
  const output = process.argv[process.argv.indexOf('--out') + 1];
  const raw = '{"runs":[],"next_offset":0,"has_more":false}';
  await writeFile(output, raw, { flag: 'wx', mode: 0o600 });
  process.stdout.write(JSON.stringify({ status: 'ok', output, bytes: Buffer.byteLength(raw), sha256: createHash('sha256').update(raw).digest('hex') }));
} else process.stdout.write('{"status":"error","code":"host_unavailable"}');
if (mode === 'error') process.exitCode = 2;
