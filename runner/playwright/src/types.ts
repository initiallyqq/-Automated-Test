export type RunRequest = {
  projectRoot: string;
  specPattern: string;
  outputDir: string;
  playwrightConfig?: string;
  headed?: boolean;
  slowMoMs?: number;
};

export type RunResult = {
  exitCode: number;
  passed: boolean;
  resultPath: string;
  tracePath?: string;
  screenshotPaths: string[];
  mode?: string;
  logPath?: string;
  stdoutPath?: string;
  stderrPath?: string;
};
