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
  // Two projects, disjoint by spec file. Neither carries a project-level
  // `grep`/`grepInvert`: the config-level `grepInvert` above is what enforces
  // quarantine filtering, and a project-level one would SHADOW it rather than
  // compose with it, silently un-quarantining every spec in that project.
  projects: [
    {
      name: "chromium",
      use: { browserName: "chromium" },
      testIgnore: /admin-band-root-font\.spec\.ts/,
    },
    {
      // The phone band's two halves — the JS matchMedia query and the CSS
      // @media rules — are both expressed in rem, so they can only diverge
      // along ONE axis: the browser's initial font size, against which rem in
      // a media query resolves. At the default 16px, rem and px are
      // numerically identical and every other proof in this tree is blind to
      // that axis. This project exists solely to run at a different one.
      name: "chromium-large-font",
      use: {
        browserName: "chromium",
        launchOptions: { args: ["--blink-settings=defaultFontSize=20"] },
      },
      testMatch: /admin-band-root-font\.spec\.ts/,
    },
  ],
});
