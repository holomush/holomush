---
name: golangci-lint v1 vs v2 schema for exclusions
description: Spec YAML snippets for `.golangci.yaml` exclusions must match the file's `version:` line — v1 used `issues.exclude-rules`, v2 uses `linters.exclusions.rules`
type: feedback
---

When reviewing specs that propose YAML edits to `.golangci.yaml`, ALWAYS
check the file's top-level `version:` field and confirm the snippet's
schema path matches.

**Why:** The v1 → v2 migration renamed several config locations:

| v1 path | v2 path |
|---|---|
| `issues.exclude-rules` | `linters.exclusions.rules` |
| `issues.exclude-files` | `linters.exclusions.paths` |
| `linters-settings.<linter>.ignore-tests` | `linters.exclusions.rules` (path-scoped) |
| `linters-settings` (general) | `linters.settings` |

A v1-shaped snippet pasted into a v2 config will EITHER be ignored as an
unknown top-level key (silent failure — exclusion does not take effect)
OR fail config-load (loud failure). Either way, the runtime behavior
deviates from what the spec claims.

**How to apply:** When a spec says "add an exclusion to `.golangci.yaml`",
read the existing file's `version:` line first. If `version: "2"`, expect
`linters.exclusions.rules`. If `version: "1"` or absent, expect
`issues.exclude-rules`. Verify against Context7 `/websites/golangci-lint_run`
migration guide if unsure. This project (HoloMUSH) is v2 — confirm by
reading `.golangci.yaml:1`.

Seen 2026-05-01 in go-analysis-migration-design pass 2: spec proposed a
`_test.go` exclusion for `ulidmakeforbidden` using `issues.exclude-rules`
syntax against a `version: "2"` config. The fix was meant to resolve a
prior pass's blocking finding about the analyzer firing on 1300+ test
files; the schema mismatch defeated the fix at runtime.
