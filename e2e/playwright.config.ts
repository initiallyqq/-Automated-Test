import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./specs",
  timeout: 60_000,
  expect: { timeout: 15_000 },
  retries: 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: [
    ["list"],
    ["json", { outputFile: "../artifacts/playwright-report.json" }],
    ["html", { outputFolder: "../artifacts/playwright-report", open: "never" }],
  ],
  use: {
    trace: "on",
    screenshot: "on",
    video: "on",
    actionTimeout: 10_000,
    navigationTimeout: 15_000,
  },
  outputDir: "../artifacts/test-results",
});
