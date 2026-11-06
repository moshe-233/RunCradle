import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import type { Context } from '@deepseek-ai/cordis';
import { BUNDLED_SKILL_RANK, type SkillCandidate } from '@deepseek-ai/dsh-skill';

const bodyUrl = new URL('../assets/SKILL.md', import.meta.url);
const candidate: SkillCandidate = {
  name: 'runcradle-entry',
  description: 'Check the RunCradle DSH plugin entry, use its diagnostic probe, and understand the currently available capabilities.',
  invocation: { modelInvocable: true, userInvocable: true },
  provider: 'runcradle-entry',
  source: 'bundled',
  resourceBase: { kind: 'directory', path: fileURLToPath(new URL('../assets/', import.meta.url)) },
  rank: BUNDLED_SKILL_RANK,
  locator: bodyUrl,
};

/** Call inside the optional skills injection so its effects belong to that scope. */
export function registerEntrySkill(ctx: Context): void {
  ctx.skills.registerProvider((control) => ({
    name: candidate.provider,
    async list(options) {
      control.signal.throwIfAborted();
      options.signal?.throwIfAborted();
      return [candidate];
    },
    async get(_candidate, options) {
      const signal = options.signal
        ? AbortSignal.any([control.signal, options.signal])
        : control.signal;
      signal.throwIfAborted();
      const content = await readFile(bodyUrl, { encoding: 'utf8', signal });
      return {
        name: candidate.name,
        description: candidate.description,
        invocation: candidate.invocation,
        provider: candidate.provider,
        source: candidate.source,
        resourceBase: candidate.resourceBase,
        content,
      };
    },
  }));
}
