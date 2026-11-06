import { readFileSync } from 'node:fs';
import type { Context } from '@deepseek-ai/cordis';
import type { Config as HostConfig } from './config.js';
import { createProbe } from './tools/probe.js';
import { registerEntrySkill } from './skill.js';

export { Config } from './config.js';
export const name = 'runcradle-host';
export const inject = ['tools'];

const manifest: { version: string } = JSON.parse(
  readFileSync(new URL('../package.json', import.meta.url), 'utf8'),
);

/** Compose contributions; Cordis owns registration, optional injection and disposal. */
export function apply(ctx: Context, config: HostConfig): void {
  ctx.tools.register(createProbe(manifest.version));
  if (config.enableSkill) {
    ctx.inject(['skills'], (inner) => registerEntrySkill(inner));
  }
}
