import Schema from '@deepseek-ai/schemastery';

export interface Config {
  enableSkill: boolean;
  runcradleExecutable?: string;
  demoWorkspace?: string;
  demoWorkbenchUrl?: string;
}

export const Config: Schema<Config> = Schema.object({
  enableSkill: Schema.boolean().default(true).description('Register the bundled entry guide when the skills service is available.'),
  runcradleExecutable: Schema.string().description('Absolute path to a local Linux RunCradle CLI. Connects to an already running workspace service.'),
  demoWorkspace: Schema.string().description('Opt-in demo: absolute workspace shared by Agent tools and the original RunCradle workbench.'),
  demoWorkbenchUrl: Schema.string().role('secret').description('Opt-in demo: native RunCradle loopback workbench pairing URL. Only delivered to the authenticated DSH owner.'),
});
