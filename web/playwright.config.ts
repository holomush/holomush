import { defineConfig } from "@playwright/test";

// Quarantined specs carry { tag: ['@quarantine', '@holomush-xxxx'] }.
// They are excluded from normal runs and only execute when
// HOLOMUSH_RUN_QUARANTINED=1, matching the Go/Ginkgo quarantine pattern.
const grepInvert = process.env.HOLOMUSH_RUN_QUARANTINED === "1"
  ? undefined
  : /@quarantine/;

export default defineConfig({
  testDir: "./e2e",
  timeout: 30000,
  grepInvert,
  use: {
    baseURL: process.env.PLAYWRIGHT_BASE_URL || "http://localhost:8080",
    headless: true,
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
  },
  reporter: [
    ["line"],
    ["json", { outputFile: "test-results/report.json" }],
    ["./e2e/helpers/summary-reporter.ts"],
  ],
  projects: [
    {
      name: "chromium",
      use: { browserName: "chromium" },
    },
  ],
});
