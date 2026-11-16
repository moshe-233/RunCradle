import assert from 'node:assert/strict';
import test from 'node:test';
import { registerDemoPanel } from '../lib/panel/host.js';

test('workbench launch handoff uses only the authenticated owner API and never starts a process', async () => {
  let route;
  const ctx = { connection: { fetch: { register: value => { route = value; } } } };
  const url = `http://127.0.0.1:12345/#pair=${'a'.repeat(64)}`;
  registerDemoPanel(ctx, url, '/tmp/demo');
  assert.equal(route.path, '/api/runcradle-workbench');
  assert.deepEqual(route.methods, ['GET']);
  const response = await route.fetch(new Request('http://localhost/api/runcradle-workbench'));
  assert.equal(response.headers.get('Cache-Control'), 'no-store');
  assert.deepEqual(await response.json(), { url, workspace: '/tmp/demo' });
});

test('workbench handoff rejects remote destinations, credentials and arbitrary paths', () => {
  for (const url of ['https://example.com/', 'http://localhost:1234/', 'http://127.0.0.1:1234/private', 'http://user:pass@127.0.0.1:1234/', 'file:///etc/passwd']) {
    assert.throws(() => registerDemoPanel({}, url, '/tmp/demo'), /loopback pairing URL/);
  }
});
