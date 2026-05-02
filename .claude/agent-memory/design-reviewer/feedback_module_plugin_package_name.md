---
name: golangci-lint module-plugin package name claim
description: golangci-lint v2 module plugins do NOT require `package main`; the canonical upstream example uses `package linters`. Verify any spec that asserts a package-name constraint.
type: feedback
---

golangci-lint v2 module-plugin specs sometimes assert that the plugin file
"must be `package main` per golangci-lint loader" or similar.

**Why:** This is wrong. The authoritative source
`golangci/plugin-module-register/register.go` defines `LinterPlugin`,
`NewPlugin`, `LoadModeSyntax`, `LoadModeTypesInfo`, and the `Plugin(name,
constructor)` registration function with no package-name constraint. The
canonical upstream example `golangci/example-plugin-module-linter` uses
`package linters`. Module plugins are loaded via Go's normal module/import
machinery, NOT via `go build -buildmode=plugin`, so there is no `main`
requirement.

**How to apply:** When reviewing any spec that proposes a golangci-lint
module plugin, check for claims about required package name and load
modes. Verify against:

- `https://raw.githubusercontent.com/golangci/plugin-module-register/main/register/register.go`
- `https://raw.githubusercontent.com/golangci/example-plugin-module-linter/main/example.go`

Seen 2026-05-01 in `2026-05-01-go-analysis-migration-design.md`: spec
§4.3 line 106 claimed `package main // module-plugin convention; must
be \`main\` per golangci-lint loader`. Both halves of the claim are
fabricated.

Related correct claim from the same spec to remember as a positive: the
`//go:build ruleguard` tag IS visible to `go mod tidy` (only `ignore` is
hidden), so any spec carving a sub-module with `//go:build ruleguard`
files must delete those files before/with the new `go.mod` is added.
