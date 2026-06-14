import { defineConfig } from "@playwright/test";

// Quarantined specs carry { tag: ['@quarantine', '@holomush-xxxx'] }.
// They are excluded from normal runs and only execute when
// HOLOMUSH_RUN_QUARANTINED=1, matching the Go/Ginkgo quarantine pattern.
const grepInvert = process.env.HOLOMUSH_RUN_QUARANTINED === "1"
  ? undefined
  : /@quarantine/;

export default defineConfig({
  testDir: "./e2e",
  // Per-test budget. CI runners (Namespace + Testcontainers Cloud) deliver
  // events markedly slower than a local box under two-BrowserContext specs
  // (e.g. scenes.spec.ts multi-tab tests: a say → location → JetStream → WS →
  // DOM round-trip racing a second tab's workspace load). Doubling the budget
  // in CI tolerates that latency without masking a real failure — a genuine
  // bug still fails, just later (holomush-mwmzt). Local runs stay at 30s.
  timeout: process.env.CI ? 60000 : 30000,
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
