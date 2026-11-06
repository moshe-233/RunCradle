import Schema from '@deepseek-ai/schemastery';

export interface Config {
  enableSkill: boolean;
}

export const Config: Schema<Config> = Schema.object({
  enableSkill: Schema.boolean().default(true).description('Register the bundled entry guide when the skills service is available.'),
});
