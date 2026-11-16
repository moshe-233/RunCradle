import { readFileSync } from 'node:fs';
import type { Context } from '@deepseek-ai/cordis';
import type { Config as HostConfig } from './config.js';
import { createProbe } from './tools/probe.js';
import { registerEntrySkill } from './skill.js';
import { isAbsolute } from 'node:path';
import { registerRunTools } from './runs/tools.js';
import { registerWorkflowTools } from './workflows/tools.js';
import { registerDemoPanel } from './panel/host.js';

export { Config } from './config.js';
export const name = 'runcradle-host';
export const inject = ['tools'];

const manifest: { version: string } = JSON.parse(
  readFileSync(new URL('../package.json', import.meta.url), 'utf8'),
);

/** Compose contributions; Cordis owns registration, optional injection and disposal. */
export function apply(ctx: Context, config: HostConfig): void {
  if (config.runcradleExecutable !== undefined && (!isAbsolute(config.runcradleExecutable) || config.runcradleExecutable.includes('\0'))) throw new Error('runcradleExecutable must be an absolute local path');
  if (config.demoWorkspace !== undefined && (!config.runcradleExecutable || !isAbsolute(config.demoWorkspace) || config.demoWorkspace.includes('\0'))) throw new Error('demoWorkspace requires an absolute workspace and runcradleExecutable');
  if (config.demoWorkbenchUrl !== undefined && !config.demoWorkspace) throw new Error('demoWorkbenchUrl requires demoWorkspace');
  ctx.tools.register(createProbe(manifest.version));
  if (config.runcradleExecutable) {
    ctx.inject(['subprocess', 'workingDirectory'], (inner) => {
      registerRunTools(inner, config.runcradleExecutable!);
      registerWorkflowTools(inner, config.runcradleExecutable!);
    });
  }
  if (config.enableSkill) {
    ctx.inject(['skills'], (inner) => registerEntrySkill(inner));
  }
  if (config.demoWorkbenchUrl && config.demoWorkspace) {
    ctx.inject(['connection'], (inner) => registerDemoPanel(inner, config.demoWorkbenchUrl!, config.demoWorkspace!));
  }
}
