import { spawn } from "node:child_process";
import { access, mkdir, readdir, readFile, writeFile } from "node:fs/promises";
import { delimiter, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type { RunRequest, RunResult } from "./types";

const moduleDir = dirname(fileURLToPath(import.meta.url));
const runnerRoot = resolve(moduleDir, "..");
const runnerNodeModules = resolve(runnerRoot, "node_modules");

async function main() {
  const requestPath = process.argv[2];
  if (!requestPath) {
    throw new Error("usage: tsx src/cli.ts <request.json>");
  }

  const request = JSON.parse(stripBOM(await readFile(requestPath, "utf8"))) as RunRequest;
  await mkdir(request.outputDir, { recursive: true });

  const playwrightCLI = resolve(runnerRoot, "node_modules", "@playwright", "test", "cli.js");
  const generatedSpecs = await findGeneratedSpecs(join(request.projectRoot, "e2e", "specs"));
  const reportPath = join(request.outputDir, "playwright-report.json");
  const testResultsDir = join(request.outputDir, "test-results");

  const result: RunResult = {
    exitCode: 0,
    passed: generatedSpecs.length > 0,
    resultPath: join(request.outputDir, "result.json"),
    screenshotPaths: [],
    mode: "placeholder"
  };
  let note = "Placeholder mode: install runner dependencies to execute real Playwright tests.";

  if (generatedSpecs.length === 0) {
    result.exitCode = 1;
  }

  if (generatedSpecs.length > 0 && await exists(playwrightCLI)) {
    try {
      const run = await runPlaywright({
        cliPath: playwrightCLI,
        projectRoot: request.projectRoot,
        config: request.playwrightConfig || "e2e/playwright.config.ts",
        outputDir: request.outputDir,
        reportPath,
        specPattern: request.specPattern,
        testResultsDir,
        headed: request.headed || false,
        slowMoMs: request.slowMoMs || 0
      });
      result.mode = "playwright";
      result.exitCode = run.exitCode;
      result.passed = run.exitCode === 0;
      result.stdoutPath = run.stdoutPath;
      result.stderrPath = run.stderrPath;
      result.logPath = run.stderrPath;
      result.screenshotPaths = run.screenshotPaths;
      result.tracePath = run.tracePath;
      note = "Executed via local @playwright/test CLI.";
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (message.includes("EPERM")) {
        note = "Placeholder mode: Playwright subprocess was blocked by the current sandbox.";
      } else {
        throw error;
      }
    }
  }

  await mkdir(dirname(result.resultPath), { recursive: true });
  await writeFile(result.resultPath, JSON.stringify({
    status: result.passed ? "passed" : "failed",
    mode: result.mode,
    projectRoot: request.projectRoot,
    specPattern: request.specPattern,
    generatedSpecs,
    generatedSpecExists: generatedSpecs.length > 0,
    reportPath: await exists(reportPath) ? reportPath : undefined,
    tracePath: result.tracePath,
    screenshotPaths: result.screenshotPaths,
    stdoutPath: result.stdoutPath,
    stderrPath: result.stderrPath,
    note
  }, null, 2));

  process.stdout.write(JSON.stringify(result, null, 2));
}

async function exists(path: string) {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}

function stripBOM(text: string) {
  return text.charCodeAt(0) === 0xfeff ? text.slice(1) : text;
}

async function findGeneratedSpecs(root: string) {
  const specs: string[] = [];

  async function walk(dir: string): Promise<void> {
    let entries;
    try {
      entries = await readdir(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) {
        await walk(path);
        continue;
      }
      if (entry.isFile() && entry.name.endsWith(".spec.ts")) {
        specs.push(path);
      }
    }
  }

  await walk(root);
  specs.sort();
  return specs;
}

async function collectFiles(root: string, suffixes: string[]) {
  const found: string[] = [];

  async function walk(dir: string): Promise<void> {
    let entries;
    try {
      entries = await readdir(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) {
        await walk(path);
        continue;
      }
      if (entry.isFile() && suffixes.some(suffix => entry.name.endsWith(suffix))) {
        found.push(path);
      }
    }
  }

  await walk(root);
  found.sort();
  return found;
}

function runPlaywright({
  cliPath,
  projectRoot,
  config,
  outputDir,
  reportPath,
  specPattern,
  testResultsDir,
  headed,
  slowMoMs
}: {
  cliPath: string;
  projectRoot: string;
  config: string;
  outputDir: string;
  reportPath: string;
  specPattern?: string;
  testResultsDir: string;
  headed?: boolean;
  slowMoMs?: number;
}) {
  return new Promise<{
    exitCode: number;
    stdoutPath: string;
    stderrPath: string;
    screenshotPaths: string[];
    tracePath?: string;
  }>((resolveRun, rejectRun) => {
    const args = [
      cliPath,
      "test",
      "--config",
      config,
      "--reporter",
      "json",
      "--output",
      testResultsDir
    ];
    if (specPattern && !specPattern.includes("*")) {
      args.push(specPattern);
    }
    if (headed) {
      args.push("--headed");
    }

    const env: Record<string, string> = {
      ...process.env,
      NODE_PATH: [runnerNodeModules, process.env.NODE_PATH].filter(Boolean).join(delimiter),
      PLAYWRIGHT_JSON_OUTPUT_NAME: reportPath,
      PLAYWRIGHT_SLOW_MO: String(Math.max(0, slowMoMs || 0)),
      NO_COLOR: "1"
    };
    if (!headed) {
      env.CI = "1";
    }

    const child = spawn(process.execPath, args, {
      cwd: projectRoot,
      env,
      stdio: ["ignore", "pipe", "pipe"]
    });

    let stdout = "";
    let stderr = "";
    child.stdout.on("data", chunk => {
      stdout += chunk.toString();
    });
    child.stderr.on("data", chunk => {
      stderr += chunk.toString();
    });
    child.on("error", rejectRun);
    child.on("close", async code => {
      const stdoutPath = join(outputDir, "playwright-stdout.json");
      const stderrPath = join(outputDir, "playwright-stderr.log");
      await writeFile(stdoutPath, stdout);
      await writeFile(stderrPath, stderr);
      const tracePaths = await collectFiles(testResultsDir, [".zip"]);
      const screenshotPaths = await collectFiles(testResultsDir, [".png", ".jpg", ".jpeg"]);
      resolveRun({
        exitCode: code ?? 1,
        stdoutPath,
        stderrPath,
        screenshotPaths,
        tracePath: tracePaths[0]
      });
    });
  });
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
