import type { Context } from '@deepseek-ai/cordis';

type Connection = { fetch: { register(route: { path: string; methods: ['GET']; requestBody: 'buffered'; fetch(request: Request): Promise<Response> }): () => Promise<void> } };

/** This is only a launch handoff. The original workbench owns all UI and API operations. */
export function registerDemoPanel(ctx: Context, configuredUrl: string, workspace: string): void {
  const url = new URL(configuredUrl);
  if (url.protocol !== 'http:' || url.hostname !== '127.0.0.1' || !url.port || url.username || url.password || url.pathname !== '/' || url.search || !/^#pair=[a-f0-9]{64}$/.test(url.hash)) {
    throw new Error('demoWorkbenchUrl must be a native RunCradle loopback pairing URL');
  }
  const connection = (ctx as Context & { connection: Connection }).connection;
  // The shared DSH /api carrier enforces authentication, Host/Origin checks,
  // and owner-only admission before dispatching an exact Fetch route.
  connection.fetch.register({ path: '/api/runcradle-workbench', methods: ['GET'], requestBody: 'buffered', fetch: async () =>
    Response.json({ url: url.href, workspace }, { headers: { 'Cache-Control': 'no-store', 'Referrer-Policy': 'no-referrer' } }),
  });
}
