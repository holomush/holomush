# Phase 06.1 — API Coverage Declaration (gap-closure round, plans 06.1-07…10)

No external API integration: this round repairs in-tree meta-tests, a Go↔TS
parity guard, and a CSS-unit/JS-unit mismatch in the web client.

## Reasoning

The four plans in this gap-closure round (`06.1-07` … `06.1-10`) touch:

| Surface | What changes | External service? |
|---|---|---|
| `test/meta/web_phone_band_breakpoint_census_test.go` | source-text census over `web/src` | no |
| `test/meta/web_browser_floor_test.go` (new) | reads `web/package.json` | no |
| `internal/grpc/admin_characters_web_parity_test.go` | parses a checked-in Svelte file, reads two in-package Go symbols | no |
| `web/src/lib/hooks/mediaQuery.svelte.ts` | a media-query string constant | no |
| `web/e2e/*.spec.ts`, `web/playwright.config.ts` | Playwright specs and a browser launch flag | no — the existing pinned Playwright image, driving the existing local compose stack |
| `web/package.json`, `web/vite.config.ts` | a `browserslist` key and, conditionally, a CSS build option | no — a manifest key, not a dependency |

No SDK is adopted, no third-party API is called, no credential is introduced, and
no entry is added to `dependencies` or `devDependencies` (plan `06.1-10`'s threat
model makes a new package a STOP condition precisely because this phase carries no
RESEARCH.md package-legitimacy audit).

The only network dependency anywhere in the round is the E2E compose stack the
repo already runs — Postgres, core, gateway and the digest-pinned
`mcr.microsoft.com/playwright` image declared in `compose.e2e.yaml` — all of which
predate this round and are unchanged by it.

_Written: 2026-08-15 during gap-closure planning for Phase 06.1._
