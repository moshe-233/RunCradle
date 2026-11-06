import { defineTool } from '@deepseek-ai/dsh-tools';

/** Diagnose only the plugin call path, without touching a workspace or runtime. */
export function createProbe(version: string) {
  return defineTool({
    name: 'runcradle_probe',
    description: 'Check the RunCradle plugin entry and echo an optional string. This does not check or start the RunCradle workflow runtime.',
    parameters: {
      echo: { type: 'string', description: 'Optional text to return unchanged.' },
    },
    output: {
      schema: {
        type: 'object',
        additionalProperties: false,
        properties: {
          plugin: { type: 'string', const: 'dsh-runcradle', required: true },
          version: { type: 'string', required: true },
          echo: { type: 'string', required: true },
        },
      },
      render: (_args, value) => [{ type: 'text', text: JSON.stringify(value) }],
    },
    async execute(args, exec) {
      exec.signal.throwIfAborted();
      return { plugin: 'dsh-runcradle' as const, version, echo: args.echo ?? '' };
    },
  });
}
