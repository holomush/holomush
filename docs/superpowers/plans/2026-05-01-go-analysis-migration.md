<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# go/analysis Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate the project's ten lint rules off `go-ruleguard`'s DSL and onto standard `golang.org/x/tools/go/analysis` analyzers, packaged as a golangci-lint v2 module plugin (`bin/custom-gcl`). Restore the seven INV-27 (`dek.Material` non-leakage) rules deleted in PR #3454. Final state: `task pr-prep` green on a cold golangci-lint cache; INV-27 enforcement is back at the lint layer in addition to the static API surface test in `internal/eventbus/crypto/dek/api_test.go`.

**Architecture:** A new Go module rooted at `gorules/` (separate `go.mod` from the parent module) hosts ten analyzers under `gorules/analyzers/<name>/`, registered via `gorules/plugin.go` using `github.com/golangci/plugin-module-register/register`. A repo-root `.custom-gcl.yml` drives `golangci-lint custom` to build `bin/custom-gcl`, which Taskfile invokes from `lint:go`. Each analyzer ships unit tests via `golang.org/x/tools/go/analysis/analysistest` with positive, negative, and allowlist-boundary fixtures.

**Tech Stack:** Go 1.26.2, `golang.org/x/tools/go/analysis`, `golang.org/x/tools/go/analysis/analysistest`, `golang.org/x/tools/go/analysis/passes/inspect`, `golang.org/x/tools/go/ast/inspector`, `github.com/golangci/plugin-module-register/register`, golangci-lint v2.11.4. Removed: `github.com/quasilyte/go-ruleguard/dsl`.

**Spec:** `docs/superpowers/specs/2026-05-01-go-analysis-migration-design.md` (READY after three design-reviewer passes).

**Bead:** `holomush-46ya` (chore(lint): migrate from go-ruleguard DSL to standard go/analysis framework). Child beads will be filed during execution to map onto the phases below.

**Scope:** Phases 1–4 below ship in a single PR (squash-merged per project policy). Each phase is a coherent commit boundary. The PR's final commit MUST leave `task pr-prep` green on a cold cache (per spec §5.3).

**Out of scope:**

- Plugin-side rule rules from holomush#1272 (the WASM plugin design has been removed; targets no longer exist).
- Replacing `gocritic` (kept for diagnostic / style / performance checks).
- Replacing `forbidigo` (kept for the `time.Sleep` ban).
- Generalizing `holomushlint` helpers into a reusable cross-project linter package.

---

## File structure overview

### Created

```text
.custom-gcl.yml                                                          # module-plugin build config
gorules/
  go.mod                                                                  # module github.com/holomush/holomush/gorules
  go.sum
  plugin.go                                                               # aggregator: blank-imports the 10 analyzer subpackages
  analyzers/
    internal/holomushlint/
      ast.go                                                              # call resolution, fully-qualified-symbol matching (covered via analysistest)
      sinks.go                                                            # Sink type + CallTargetsAnySink helper (covered via analysistest)
      types.go                                                            # IsDEKMaterial, package-path predicates
      types_test.go                                                       # PackagePathMatchesAny unit test
      sqlmatch.go                                                         # literal + concat + named-const string extraction
      sqlmatch_test.go                                                    # synthetic-AST unit tests
    ulidmakeforbidden/
      ulidmakeforbidden.go                                                # exported `Analyzer` + `run`
      plugin.go                                                           # init() → register.Plugin("ulidmakeforbidden", ...)
      ulidmakeforbidden_test.go
      testdata/src/example.com/positive/positive.go
      testdata/src/example.com/negative/negative.go
    cursorpackageinternal/
      cursorpackageinternal.go
      plugin.go
      cursorpackageinternal_test.go
      testdata/src/github.com/holomush/holomush/internal/eventbus/cursor/cursor.go
      testdata/src/example.com/positive/positive.go
      testdata/src/github.com/holomush/holomush/internal/eventbus/allow/allow.go
      testdata/src/github.com/holomush/holomush/internal/grpc/allow/allow.go
    sceneopseventsappendonly/
      sceneopseventsappendonly.go
      plugin.go
      sceneopseventsappendonly_test.go
      testdata/src/example.com/positive/positive.go
      testdata/src/example.com/negative/negative.go
    dekmaterialnojson/
      dekmaterialnojson.go
      plugin.go
      dekmaterialnojson_test.go
      testdata/src/github.com/holomush/holomush/internal/eventbus/crypto/dek/dek.go
      testdata/src/example.com/positive/positive.go
      testdata/src/example.com/negative/negative.go
    dekmaterialnogob/                                                     # same layout (analyzer + plugin.go + tests + testdata)
    dekmaterialnoproto/                                                   # same layout
    dekmaterialnofmtformatting/                                           # same layout
    dekmaterialnolog/                                                     # same layout
    dekmaterialnoslog/                                                    # same layout
    codeckeybytesallowlist/
      codeckeybytesallowlist.go
      plugin.go
      codeckeybytesallowlist_test.go
      testdata/src/github.com/holomush/holomush/internal/eventbus/codec/codec.go
      testdata/src/example.com/positive/positive.go
      testdata/src/example.com/negative/negative.go
      testdata/src/github.com/holomush/holomush/internal/eventbus/codec/allow/allow.go
      testdata/src/github.com/holomush/holomush/internal/eventbus/crypto/allow/allow.go
```

### Modified

```text
.gitignore                                                                # add bin/custom-gcl
.golangci.yaml                                                            # drop ruleguard subblock; add custom plugin entry; enable 10 analyzers; add _test.go exclusion for ulidmakeforbidden
Taskfile.yaml                                                             # add lint:build-custom-gcl; rewire lint:go; add test:gorules
lefthook.yaml                                                             # swap golangci-lint for ./bin/custom-gcl
.github/workflows/ci.yaml                                                 # bump v2.11.3 → v2.11.4
go.mod                                                                    # remove github.com/quasilyte/go-ruleguard/dsl
go.sum                                                                    # tidy fallout from go.mod
internal/eventbus/crypto/dek/material.go                                  # update three doc-comment references
internal/eventbus/crypto/dek/api_test.go                                  # update one doc-comment reference
```

### Deleted

```text
gorules/rules.go                                                          # ruleguard DSL file (replaced by analyzer module)
gorules/testdata/dek_no_serialize/expected_violations.go                  # documentation-only fixture (replaced by analyzer testdata)
gorules/testdata/codec_key_bytes/expected_violations.go                   # documentation-only fixture
gorules/testdata/dek_no_serialize/                                        # empty after file deletion
gorules/testdata/codec_key_bytes/                                         # empty after file deletion
gorules/testdata/                                                         # empty after subdirs deletion
```

---

## Pre-flight verification

### Task 0: Verify workspace state

**Files:** none (verification only).

- [ ] **Step 1: Verify worktree, jj state, and freshness**

Run:

```bash
jj root && pwd
jj --no-pager log -r '@ | @-' --limit 2
jj git fetch
jj --no-pager log -r 'main@origin' --limit 1
```

Expected: `pwd` is `/Volumes/Code/github.com/holomush/.worktrees/go-analysis` (or your equivalent isolated workspace). `@-` is `main | feat(crypto): Phase 2 substrate` (commit `wywvppktxkky`). The fetch should be a no-op or only reveal the spec commit on `main@origin` if your branch was already pushed.

- [ ] **Step 2: Verify the spec is committed and locatable**

Run: `ls -la docs/superpowers/specs/2026-05-01-go-analysis-migration-design.md`
Expected: file exists, ~17–18 KB.

- [ ] **Step 3: Verify pre-existing repo state matches the plan's assumptions**

Run:

```bash
test -f gorules/rules.go && echo "rules.go present"
test -d gorules/testdata/dek_no_serialize && echo "old dek testdata present"
test -d gorules/testdata/codec_key_bytes && echo "old codec testdata present"
rg -nF 'github.com/quasilyte/go-ruleguard/dsl' go.mod
rg -nF 'rules: gorules/rules.go' .golangci.yaml
```

Expected: all four checks succeed (file/dir present, ruleguard dep + ruleguard config present).

---

## Phase 1: Tear down ruleguard infrastructure

The new analyzer module's `gorules/go.mod` cannot coexist with `gorules/rules.go` (which imports `github.com/quasilyte/go-ruleguard/dsl`) because adding `gorules/go.mod` carves the file into the new module, and `go mod tidy` scans `//go:build ruleguard`-tagged files (see spec §6). We therefore tear down the old infra first, accepting a temporary window in which `ULIDMakeForbidden`, `CursorPackageInternal`, and `SceneOpsEventsAppendOnly` are not enforced. The PR's final commit re-enables all three plus the seven INV-27 rules.

**Task numbering note:** Original Tasks 1 and 2 were merged into a single atomic Task 1 (per plan-reviewer NB#1) so the `gorules/rules.go` deletion and the `.golangci.yaml` ruleguard-subblock removal land together. Phase 1 therefore has three tasks: Task 1 (atomic delete + config), Task 3 (drop the parent-module dep), and Task 4 (gitignore). Subsequent Phase 1 tasks keep their original numbers to avoid ripple-renumbering across the rest of the plan.

### Task 1: Delete ruleguard files AND drop the gocritic ruleguard subblock (atomic)

**Atomic by design.** `.golangci.yaml`'s `gocritic.settings.ruleguard.rules: gorules/rules.go` reference must be removed in the same commit that deletes `gorules/rules.go`, otherwise the intermediate commit boundary would have a `task lint` step that fails (gocritic ruleguard pointing at a missing file). Plan-reviewer NB#1.

**Files:**

- Delete: `gorules/rules.go`
- Delete: `gorules/testdata/dek_no_serialize/expected_violations.go`
- Delete: `gorules/testdata/codec_key_bytes/expected_violations.go`
- Delete: `gorules/testdata/dek_no_serialize/` (now empty)
- Delete: `gorules/testdata/codec_key_bytes/` (now empty)
- Delete: `gorules/testdata/` (now empty)
- Modify: `.golangci.yaml` (remove `linters.settings.gocritic.settings` ruleguard subblock at lines 141–143)

- [ ] **Step 1: Delete files and (now-empty) directories**

Run:

```bash
rm gorules/rules.go
rm gorules/testdata/dek_no_serialize/expected_violations.go
rm gorules/testdata/codec_key_bytes/expected_violations.go
rmdir gorules/testdata/dek_no_serialize
rmdir gorules/testdata/codec_key_bytes
rmdir gorules/testdata
```

Expected: `gorules/` is now empty.

- [ ] **Step 2: Edit `.golangci.yaml` to remove the ruleguard config**

Remove the `settings:` block immediately under `gocritic:`. Before:

```yaml
    gocritic:
      enabled-tags:
        - diagnostic
        - style
        - performance
      disabled-checks:
        - hugeParam # Event struct (120 bytes) is passed by value by design
      settings:
        ruleguard:
          rules: gorules/rules.go
```

After:

```yaml
    gocritic:
      enabled-tags:
        - diagnostic
        - style
        - performance
      disabled-checks:
        - hugeParam # Event struct (120 bytes) is passed by value by design
```

- [ ] **Step 3: Verify nothing else references the deleted file**

Run: `rg -nF 'gorules/rules.go' .`
Expected: no hits (or only hits inside the spec/plan, which is fine).

- [ ] **Step 4: Verify the lint config still parses and lint passes**

Run: `golangci-lint config verify`
Expected: exit 0; no schema errors.

Run: `task lint:go`
Expected: exit 0. (The 3 active ruleguard rules — ULID/Cursor/SceneOps — are no longer enforced; they're reintroduced as analyzers in Tasks 9, 10, 11.)

- [ ] **Step 5: Commit**

Run:

```bash
jj --no-pager commit -m "chore(lint): delete go-ruleguard DSL rules and gocritic config (holomush-46ya)

Tear-down step of the go/analysis migration (spec
docs/superpowers/specs/2026-05-01-go-analysis-migration-design.md §6 Shape A).
Removes atomically:
- gorules/rules.go: the build-tagged ruleguard DSL file (3 active rules:
  ULIDMakeForbidden, CursorPackageInternal, SceneOpsEventsAppendOnly).
- gorules/testdata/{dek_no_serialize,codec_key_bytes}/expected_violations.go:
  documentation-only fixtures that explained patterns the deleted
  INV-27 rules WOULD flag.
- linters.settings.gocritic.settings.ruleguard from .golangci.yaml: the
  config that pointed at the now-deleted rules.go file.

Atomic because gocritic config + rules file are tied — splitting them
across two commits would leave an intermediate state with broken
ruleguard config.

These rules and fixtures are reintroduced as standard go/analysis
analyzers in subsequent commits in this series (Tasks 9-19). INV-27
enforcement remains via the static API surface test in
internal/eventbus/crypto/dek/api_test.go during the gap.

gocritic itself stays enabled for its diagnostic/style/performance tags.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Task 3: Remove `go-ruleguard/dsl` from parent `go.mod`

**Files:**

- Modify: `go.mod` (remove `github.com/quasilyte/go-ruleguard/dsl v0.3.23`)
- Modify: `go.sum` (tidied output)

- [ ] **Step 1: Run `go mod tidy`**

Run: `go mod tidy`
Expected: removes `github.com/quasilyte/go-ruleguard/dsl` from `go.mod` and any related entries from `go.sum`. No errors.

- [ ] **Step 2: Verify the removal**

Run: `rg -nF 'go-ruleguard' go.mod go.sum`
Expected: no hits.

- [ ] **Step 3: Verify the project still builds and unit tests still pass**

Run: `task test`
Expected: green (no regressions; the deleted rules.go was build-tagged and never compiled into the parent module).

- [ ] **Step 4: Commit**

Run:

```bash
jj --no-pager commit -m "chore(deps): remove go-ruleguard/dsl after gorules/rules.go deletion (holomush-46ya)

go-ruleguard/dsl was a direct dependency only because gorules/rules.go
imported it (via the build-tagged ruleguard build constraint). With that
file deleted in the previous commit, the dependency is unreachable.
Re-added entries in go.sum are pruned by go mod tidy.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Task 4: Update `.gitignore` for the build artifact

**Files:**

- Modify: `.gitignore` (add `/bin/custom-gcl`)

- [ ] **Step 1: Edit `.gitignore`**

Append the following lines (or place them in an existing build-artifacts section if one exists):

```text
# golangci-lint module-plugin custom binary (built via `task lint:build-custom-gcl`)
/bin/custom-gcl
```

- [ ] **Step 2: Verify**

Run: `rg -nF 'custom-gcl' .gitignore`
Expected: matches the added line.

- [ ] **Step 3: Commit**

Run:

```bash
jj --no-pager commit -m "chore(lint): gitignore bin/custom-gcl (holomush-46ya)

The custom-gcl binary is built locally by 'task lint:build-custom-gcl'
and is rebuilt deterministically from gorules/ source; never check it
in.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Phase 2: Bootstrap module-plugin scaffolding

### Task 5: Create the `gorules/` Go module skeleton

The central `gorules/plugin.go` is an aggregator that blank-imports each analyzer subpackage. Each analyzer subpackage owns its own `register.Plugin(<name>, …)` call (added in Tasks 9–19). At bootstrap the aggregator has no imports and no analyzers; the imports grow incrementally.

**Files:**

- Create: `gorules/go.mod`
- Create: `gorules/plugin.go` (aggregator, no imports yet)

- [ ] **Step 1: Initialize the module**

Run:

```bash
mkdir -p gorules
cd gorules
go mod init github.com/holomush/holomush/gorules
go get github.com/golangci/plugin-module-register@latest
go get golang.org/x/tools/go/analysis@latest
cd ..
```

Expected: `gorules/go.mod` declares `module github.com/holomush/holomush/gorules` with `golangci/plugin-module-register` and `golang.org/x/tools` as direct dependencies. `gorules/go.sum` is generated.

- [ ] **Step 2: Write the aggregator package**

Create `gorules/plugin.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package holomushrules is the module-plugin aggregator. It does not
// itself register any plugins — each analyzer subpackage registers
// itself with golangci-lint via init() + register.Plugin(<name>, ...).
// Blank imports below pull each analyzer subpackage so its init()
// fires when golangci-lint builds the custom-gcl binary (which adds
// `import _ "github.com/holomush/holomush/gorules"` at build time per
// .custom-gcl.yml).
//
// The package name is unconstrained by the module-plugin API.
//
// See docs/superpowers/specs/2026-05-01-go-analysis-migration-design.md §4.3.
package holomushrules

// Blank imports — populated by Tasks 9–19 (one per analyzer).
// Bootstrap: no analyzers yet.
```

- [ ] **Step 3: Verify the package compiles**

Run: `cd gorules && go build ./...`
Expected: exit 0 (the aggregator with no imports is valid Go).

Run: `cd gorules && go test ./...`
Expected: `ok` (no test files yet, but the test invocation succeeds).

- [ ] **Step 4: Verify the parent module is unaffected**

Run: `task test`
Expected: green. Adding `gorules/go.mod` carves `gorules/` out of the parent module's package set, so the parent module's tests are unaffected.

- [ ] **Step 5: Commit**

Run:

```bash
jj --no-pager commit -m "feat(gorules): bootstrap go/analysis plugin module (holomush-46ya)

Creates a separate Go module under gorules/ with module path
github.com/holomush/holomush/gorules. The package at the module root
is an aggregator that will blank-import each analyzer subpackage as
they are added in subsequent commits. Each analyzer subpackage owns
its own register.Plugin(<name>, ...) call so that each analyzer
becomes its own enableable linter ID in golangci-lint (per spec §4.3
and the per-analyzer-plugin contract verified against
golangci-lint v2.11.4).

Per spec §4.2, two-go.mod-in-one-repo is supported: the parent module's
package set automatically excludes gorules/ once gorules/go.mod exists.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Task 6: Create `.custom-gcl.yml` and verify `golangci-lint custom` builds

**Files:**

- Create: `.custom-gcl.yml`

- [ ] **Step 1: Create `.custom-gcl.yml`**

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Build configuration for `golangci-lint custom`. Produces bin/custom-gcl,
# which embeds the standard golangci-lint linter set plus the analyzers
# from gorules/ (the holomushrules plugin).
#
# See docs/superpowers/specs/2026-05-01-go-analysis-migration-design.md §4.5.1.
version: v2.11.4
name: custom-gcl
destination: ./bin/
plugins:
  - module: github.com/holomush/holomush/gorules
    path: ./gorules
```

- [ ] **Step 2: Build the custom binary**

Run: `golangci-lint custom`
Expected: build succeeds; produces `./bin/custom-gcl` (~80–120 MB binary).

- [ ] **Step 3: Verify the binary loads the plugin**

Run: `./bin/custom-gcl linters | rg holomushrules || true`
Expected: no `holomushrules` linter is yet listed (plugin returns zero analyzers). The grep MAY return non-zero but the binary itself runs and exits cleanly. The point is the binary exists and runs.

Run: `./bin/custom-gcl version`
Expected: prints version `2.11.4` plus build metadata.

- [ ] **Step 4: Commit**

Run:

```bash
jj --no-pager commit -m "build(lint): add .custom-gcl.yml for module-plugin builds (holomush-46ya)

Configures 'golangci-lint custom' to build bin/custom-gcl, the project's
custom golangci-lint binary that embeds the holomushrules plugin
(gorules/ module). Pinned to v2.11.4 to match the to-be-bumped CI
install (Phase 4 Task 24).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Task 7: Wire `task lint:build-custom-gcl` and rewire `task lint:go`

**Files:**

- Modify: `Taskfile.yaml` (add `lint:build-custom-gcl`; modify `lint:go`; add `test:gorules`; thread `test:gorules` into `test`)

- [ ] **Step 1: Inspect the existing `test` and `lint:go` targets**

Run:

```bash
rg -nB1 -A4 '^  test:' Taskfile.yaml | head -40
rg -nB1 -A4 '^  lint:go:' Taskfile.yaml | head -10
```

Expected output (paraphrased): `test:` runs `gotestsum -- ./...` (or similar); `lint:go:` runs `golangci-lint run`. Use the actual cmds you find as the basis for the modifications below.

- [ ] **Step 2: Edit `Taskfile.yaml`**

Replace the existing `lint:go:` target with the wired-up version, and append the two new targets. The exact placement of `test:gorules` and the integration into `test` should match the file's existing organization (alongside `test:int` etc.).

```yaml
  lint:go:
    desc: Lint Go code (uses bin/custom-gcl with the holomushrules plugin)
    deps: [lint:build-custom-gcl]
    cmds:
      - ./bin/custom-gcl run

  lint:build-custom-gcl:
    desc: Build the custom-gcl binary if missing or stale
    # NOTE: .golangci.yaml is intentionally excluded from sources — the
    # binary embeds analyzer code at build time but reads .golangci.yaml
    # at run time, so config-only changes do not require a rebuild.
    sources:
      - .custom-gcl.yml
      - gorules/**/*.go
      - gorules/go.mod
      - gorules/go.sum
    generates:
      - ./bin/custom-gcl
    cmds:
      - golangci-lint custom

  test:gorules:
    desc: Run analysistest unit tests for the gorules analyzer module
    dir: gorules
    cmds:
      - go test ./...
```

Add `test:gorules` as a dependency of (or step within) the existing `test` target so `task test` covers the analyzer module. If `test` is currently a single command, convert to a `cmds:` list with both invocations:

```yaml
  test:
    desc: Run unit tests (parent module + gorules analyzer module)
    cmds:
      - task: test:gorules
      - <existing parent-module test command, verbatim>
```

- [ ] **Step 3: Verify the Taskfile parses**

Run: `task --list-all 2>/dev/null | rg -F '* lint:build-custom-gcl' && task --list-all 2>/dev/null | rg -F '* test:gorules'`
Expected: both grep matches succeed.

- [ ] **Step 4: Verify `task lint:go` still works**

Run:

```bash
rm -f bin/custom-gcl
task lint:go
```

Expected: builds custom-gcl, runs lint, exits 0. The build step takes 30–90s on a cold cache.

- [ ] **Step 5: Verify `task test` covers gorules**

Run: `task test`
Expected: includes the gorules module test (the smoke test from Task 5); green overall.

- [ ] **Step 6: Commit**

Run:

```bash
jj --no-pager commit -m "build(lint): wire Taskfile for custom-gcl + test:gorules (holomush-46ya)

- lint:go now depends on lint:build-custom-gcl and runs ./bin/custom-gcl
  instead of the system golangci-lint. The build step is cache-aware via
  Taskfile sources/generates (rebuilds only when .custom-gcl.yml or
  gorules/ source changes).
- test:gorules runs go test inside the gorules module. Threaded into
  task test so analyzer unit tests run as part of the standard test
  command.

Per spec §4.5.3.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Task 8: Implement `holomushlint` shared helpers (TDD)

The `analyzers/internal/holomushlint/` package owns helpers shared across multiple analyzers:

- `IsCallToFQSym(pass, call, pkgPath, funcName) bool` — does this CallExpr resolve to a specific package-level function?
- `IsCallToMethod(pass, call, recvPkgPath, recvTypeName, methodName) bool` — does this CallExpr resolve to a specific method on a specific receiver type?
- `Sink` type + `CallTargetsAnySink(pass, call, sinks) bool` — used by the six DEK no-sink analyzers
- `IsDEKMaterial(t types.Type) bool` — true for `dek.Material` and `*dek.Material`
- `PackagePathMatchesAny(pkgPath, prefixes []string) bool` — for allowlist checks
- `ExtractStringConst(pass, expr) (string, bool)` — for SceneOps SQL extraction (literal, +-chain, named const)

**Test scope decision (per spec §7 open question):** AST-walking helpers (`IsCallToFQSym`, `IsCallToMethod`, `Sink`/`CallTargetsAnySink`, `IsDEKMaterial`) get **transitive coverage** via `analysistest.Run` in the analyzer tests (Tasks 9–19) — there is no clean way to unit-test them without spinning up a full type-checker, and analysistest is the right granularity. Two helpers DO get standalone unit tests because they are logic-heavy and their failure modes wouldn't be cleanly caught by analyzer tests:

- `PackagePathMatchesAny`: pure string logic with subtle prefix-vs-exact semantics.
- `ExtractStringConst`: three parser shapes (literal, +-chain, named const), the named-const path uses `types.Info.Types[expr].Value` which we exercise via synthetic AST + manually-constructed `types.Info` (no importer needed).

**Files:**

- Create: `gorules/analyzers/internal/holomushlint/ast.go`
- Create: `gorules/analyzers/internal/holomushlint/sinks.go`
- Create: `gorules/analyzers/internal/holomushlint/types.go`
- Create: `gorules/analyzers/internal/holomushlint/types_test.go`
- Create: `gorules/analyzers/internal/holomushlint/sqlmatch.go`
- Create: `gorules/analyzers/internal/holomushlint/sqlmatch_test.go`

- [ ] **Step 1: Create `ast.go` — call-resolution helpers**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package holomushlint provides shared helpers for the HoloMUSH lint
// analyzers in gorules/analyzers/. Helpers in this package MUST NOT
// be exported beyond the gorules module; the internal/ position
// enforces that.
package holomushlint

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// IsCallToFQSym reports whether call resolves to the package-level
// function (or constructor) named funcName in package pkgPath.
//
// Use this for free functions like fmt.Sprintf, json.Marshal,
// proto.Marshal, ulid.Make, log.Printf. For methods (where the
// receiver is significant), use IsCallToMethod.
func IsCallToFQSym(pass *analysis.Pass, call *ast.CallExpr, pkgPath, funcName string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	obj := pass.TypesInfo.Uses[sel.Sel]
	if obj == nil {
		return false
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	if fn.Pkg() == nil {
		return false
	}
	if fn.Pkg().Path() != pkgPath {
		return false
	}
	if fn.Name() != funcName {
		return false
	}
	// Free function has no receiver; method has one.
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	return sig.Recv() == nil
}

// IsCallToMethod reports whether call resolves to a method named
// methodName on the named type recvTypeName in package recvPkgPath.
// The receiver may be either value or pointer type at the call site;
// IsCallToMethod normalizes via the named-type identity.
//
// Example: IsCallToMethod(pass, call, "encoding/json", "Encoder", "Encode")
// matches both `enc.Encode(v)` where enc is *json.Encoder and
// `(&json.Encoder{}).Encode(v)`.
func IsCallToMethod(pass *analysis.Pass, call *ast.CallExpr, recvPkgPath, recvTypeName, methodName string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	obj := pass.TypesInfo.Uses[sel.Sel]
	if obj == nil {
		return false
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	if fn.Name() != methodName {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	recv := sig.Recv()
	if recv == nil {
		return false
	}
	recvType := recv.Type()
	if ptr, ok := recvType.(*types.Pointer); ok {
		recvType = ptr.Elem()
	}
	named, ok := recvType.(*types.Named)
	if !ok {
		return false
	}
	if named.Obj().Pkg() == nil {
		return false
	}
	if named.Obj().Pkg().Path() != recvPkgPath {
		return false
	}
	if named.Obj().Name() != recvTypeName {
		return false
	}
	return true
}
```

- [ ] **Step 2: Create `sinks.go` — Sink type + CallTargetsAnySink helper**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holomushlint

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

// Sink describes a forbidden function or method. Exactly one of
// (FuncName) or (RecvTypeName + MethodName) is populated:
//
//   - free function: PkgPath + FuncName, RecvTypeName = ""
//   - method on a type: PkgPath (where the type is defined) +
//     RecvTypeName + MethodName, FuncName = ""
type Sink struct {
	PkgPath      string // package containing the function or receiver type
	FuncName     string // free-function name (when RecvTypeName == "")
	RecvTypeName string // named-type receiver (when FuncName == "")
	MethodName   string // method name (when RecvTypeName != "")
}

// String returns a human-readable identifier for diagnostics.
func (s Sink) String() string {
	if s.RecvTypeName == "" {
		return s.PkgPath + "." + s.FuncName
	}
	return "(*" + s.PkgPath + "." + s.RecvTypeName + ")." + s.MethodName
}

// CallTargetsAnySink reports whether call's resolved callee matches any
// sink in the slice. Returns true on the first match.
func CallTargetsAnySink(pass *analysis.Pass, call *ast.CallExpr, sinks []Sink) bool {
	for _, s := range sinks {
		if s.RecvTypeName == "" {
			if IsCallToFQSym(pass, call, s.PkgPath, s.FuncName) {
				return true
			}
		} else {
			if IsCallToMethod(pass, call, s.PkgPath, s.RecvTypeName, s.MethodName) {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 3: Create `types.go` — type-identity helpers**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holomushlint

import (
	"go/types"
	"strings"
)

// DEKMaterialPath is the canonical fully-qualified package path of the
// dek.Material type, exported as a constant so analyzer tests can stub
// the package at the same path under testdata/src/.
const DEKMaterialPath = "github.com/holomush/holomush/internal/eventbus/crypto/dek"

// IsDEKMaterial reports whether t is dek.Material or *dek.Material.
func IsDEKMaterial(t types.Type) bool {
	if t == nil {
		return false
	}
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	if named.Obj().Pkg() == nil {
		return false
	}
	if named.Obj().Pkg().Path() != DEKMaterialPath {
		return false
	}
	return named.Obj().Name() == "Material"
}

// PackagePathMatchesAny reports whether pkgPath equals any allow exactly,
// or starts with any allow followed by "/" (i.e., is a sub-package).
//
// Example: PackagePathMatchesAny("github.com/holomush/holomush/internal/web/handlers",
//                                []string{"github.com/holomush/holomush/internal/web"})
// returns true.
func PackagePathMatchesAny(pkgPath string, allow []string) bool {
	for _, a := range allow {
		if pkgPath == a {
			return true
		}
		if strings.HasPrefix(pkgPath, a+"/") {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Create `types_test.go`**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holomushlint_test

import (
	"testing"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

func TestPackagePathMatchesAnyExactAndPrefix(t *testing.T) {
	allow := []string{
		"github.com/holomush/holomush/internal/eventbus",
		"github.com/holomush/holomush/internal/grpc",
	}
	cases := []struct {
		path string
		want bool
	}{
		{"github.com/holomush/holomush/internal/eventbus", true},
		{"github.com/holomush/holomush/internal/eventbus/codec", true},
		{"github.com/holomush/holomush/internal/grpc/handlers", true},
		{"github.com/holomush/holomush/internal/web", false},
		// Boundary: prefix-without-slash must NOT match.
		{"github.com/holomush/holomush/internal/eventbusx", false},
	}
	for _, tc := range cases {
		if got := holomushlint.PackagePathMatchesAny(tc.path, allow); got != tc.want {
			t.Errorf("path=%q: got %v, want %v", tc.path, got, tc.want)
		}
	}
}

// IsDEKMaterial is exercised end-to-end by the dekmaterialno* analyzer
// tests via analysistest; a unit-test variant requires constructing a
// types.Type for an external package, which is unwieldy here. The
// analyzer testdata is the authoritative coverage path.
```

- [ ] **Step 5: Create `sqlmatch.go` — string-constant extraction for SceneOps**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holomushlint

import (
	"go/ast"
	"go/constant"
	"go/token"
	"strconv"

	"golang.org/x/tools/go/analysis"
)

// ExtractStringConst attempts to recover a string-constant value from
// expr. Three shapes are supported, matching the SceneOps SQL rule's
// design (spec §4.4.3):
//
//   1. *ast.BasicLit of kind STRING (raw or interpreted).
//   2. *ast.BinaryExpr chain of `+`-joined string literals (any depth).
//   3. *ast.Ident resolving to a constant.Value of kind String via
//      pass.TypesInfo.Types[expr].Value.
//
// Returns (s, true) on success, ("", false) otherwise. Callers handling
// the false case should not emit a diagnostic — failure to recover a
// string is not itself a violation, only an inability to inspect the
// SQL.
func ExtractStringConst(pass *analysis.Pass, expr ast.Expr) (string, bool) {
	// Shape 3 first: types.Info covers idents, qualified idents
	// (other.Const), and constant-folded BasicLits all at once.
	if tv, ok := pass.TypesInfo.Types[expr]; ok {
		if tv.Value != nil && tv.Value.Kind() == constant.String {
			return constant.StringVal(tv.Value), true
		}
	}
	// Shapes 1 and 2 fall through if types.Info doesn't have a folded value
	// (the package may not have been type-checked, or expr may be nil-typed).
	return extractStringFromAST(expr)
}

func extractStringFromAST(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		l, ok := extractStringFromAST(e.X)
		if !ok {
			return "", false
		}
		r, ok := extractStringFromAST(e.Y)
		if !ok {
			return "", false
		}
		return l + r, true
	case *ast.ParenExpr:
		return extractStringFromAST(e.X)
	}
	return "", false
}
```

- [ ] **Step 6: Create `sqlmatch_test.go`**

The test uses synthetic AST nodes and a manually-constructed `types.Info` so we can exercise all three string-extraction shapes (literal, +-chain, named const) without needing a Go-module-aware type checker.

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holomushlint_test

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

func newPass(info *types.Info) *analysis.Pass {
	return &analysis.Pass{
		TypesInfo: info,
		Fset:      token.NewFileSet(),
	}
}

func TestExtractStringConstFromBasicLit(t *testing.T) {
	lit := &ast.BasicLit{Kind: token.STRING, Value: `"hello"`}
	pass := newPass(&types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
	})
	got, ok := holomushlint.ExtractStringConst(pass, lit)
	if !ok || got != "hello" {
		t.Fatalf(`got (%q, %v); want ("hello", true)`, got, ok)
	}
}

func TestExtractStringConstFromRawStringLit(t *testing.T) {
	lit := &ast.BasicLit{Kind: token.STRING, Value: "`raw\nstring`"}
	pass := newPass(&types.Info{Types: map[ast.Expr]types.TypeAndValue{}})
	got, ok := holomushlint.ExtractStringConst(pass, lit)
	if !ok || got != "raw\nstring" {
		t.Fatalf("got (%q, %v)", got, ok)
	}
}

func TestExtractStringConstFromConcatChain(t *testing.T) {
	left := &ast.BasicLit{Kind: token.STRING, Value: `"UPDATE "`}
	mid := &ast.BasicLit{Kind: token.STRING, Value: `"scene_ops_events"`}
	right := &ast.BasicLit{Kind: token.STRING, Value: `" SET x = 1"`}
	expr := &ast.BinaryExpr{
		Op: token.ADD,
		X:  &ast.BinaryExpr{Op: token.ADD, X: left, Y: mid},
		Y:  right,
	}
	pass := newPass(&types.Info{Types: map[ast.Expr]types.TypeAndValue{}})
	got, ok := holomushlint.ExtractStringConst(pass, expr)
	if !ok || got != "UPDATE scene_ops_events SET x = 1" {
		t.Fatalf("got (%q, %v)", got, ok)
	}
}

func TestExtractStringConstFromNamedConst(t *testing.T) {
	// Synthetic ident with a manually-attached constant value.
	ident := &ast.Ident{Name: "sql"}
	pass := newPass(&types.Info{
		Types: map[ast.Expr]types.TypeAndValue{
			ident: {Value: constant.MakeString("DELETE FROM scene_ops_events")},
		},
	})
	got, ok := holomushlint.ExtractStringConst(pass, ident)
	if !ok || got != "DELETE FROM scene_ops_events" {
		t.Fatalf("got (%q, %v)", got, ok)
	}
}

func TestExtractStringConstReturnsFalseForNonStringExpr(t *testing.T) {
	ident := &ast.Ident{Name: "x"}
	pass := newPass(&types.Info{
		Types: map[ast.Expr]types.TypeAndValue{
			ident: {Value: constant.MakeInt64(42)},
		},
	})
	if got, ok := holomushlint.ExtractStringConst(pass, ident); ok {
		t.Fatalf("expected false; got %q", got)
	}
}

func TestExtractStringConstReturnsFalseForUnknownExprKind(t *testing.T) {
	// CallExpr is not a supported shape.
	call := &ast.CallExpr{Fun: &ast.Ident{Name: "f"}}
	pass := newPass(&types.Info{Types: map[ast.Expr]types.TypeAndValue{}})
	if _, ok := holomushlint.ExtractStringConst(pass, call); ok {
		t.Fatal("expected false for CallExpr")
	}
}

// keep the imports above honest in case go vet runs in isolation
var _ = types.NewPackage
```

- [ ] **Step 7: Run all helper tests**

Run: `cd gorules && go test ./analyzers/internal/holomushlint/...`
Expected: green; both `types_test.go` and `sqlmatch_test.go` pass. `ast.go` and `sinks.go` have no standalone tests (covered transitively via analysistest in Tasks 9–19).

- [ ] **Step 8: Commit**

Run:

```bash
jj --no-pager commit -m "feat(gorules/holomushlint): shared analyzer helpers (holomush-46ya)

Adds gorules/analyzers/internal/holomushlint/ with helpers used by all
ten project analyzers:
- ast.go: IsCallToFQSym, IsCallToMethod (resolve a CallExpr to a
  fully-qualified package-level function or method).
- sinks.go: Sink type + CallTargetsAnySink (used by the six DEK
  no-sink analyzers).
- types.go: IsDEKMaterial, PackagePathMatchesAny (allowlist match with
  exact + sub-package semantics).
- sqlmatch.go: ExtractStringConst (literal + +-chain + named-const
  resolution for the SceneOps SQL rule).

Per the spec §7 open question, AST-walking helpers are covered
transitively by analysistest in the analyzer tests; only
PackagePathMatchesAny (pure string logic) and ExtractStringConst
(three parser shapes including types.Info-based const folding) get
standalone unit tests.

Per spec §4.4 sub-sections.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Phase 3: Implement the ten analyzers

Each analyzer task follows the same TDD shape: testdata first, then analyzer code, then plugin.go registration, then `.golangci.yaml` enable, then verify on the real repo, then commit. Tasks 9–12 cover the three structurally-distinct analyzers (call-match, broad reference, SQL-aware). Tasks 13–18 cover the six DEK no-sink analyzers. Task 19 covers the codec.Key.Bytes allowlist.

### Task 9: `ulidmakeforbidden` analyzer (TDD)

**This task establishes the per-analyzer template that Tasks 10–19 follow.** Each analyzer subpackage gets its own `<name>.go` (the analyzer code) AND a `plugin.go` (the `register.Plugin(<name>, …)` call). The central `gorules/plugin.go` aggregator gains a blank import to trigger the analyzer's `init()`. The `.golangci.yaml` config gains both an `enable` entry and a `linters.settings.custom.<name>` entry per analyzer (each `type: module`).

**Files:**

- Create: `gorules/analyzers/ulidmakeforbidden/ulidmakeforbidden.go`
- Create: `gorules/analyzers/ulidmakeforbidden/plugin.go`
- Create: `gorules/analyzers/ulidmakeforbidden/ulidmakeforbidden_test.go`
- Create: `gorules/analyzers/ulidmakeforbidden/testdata/src/example.com/positive/positive.go`
- Create: `gorules/analyzers/ulidmakeforbidden/testdata/src/example.com/negative/negative.go`
- Create: `gorules/analyzers/ulidmakeforbidden/testdata/src/github.com/oklog/ulid/v2/ulid.go` (stub)
- Modify: `gorules/plugin.go` (add blank import)
- Modify: `.golangci.yaml` (add `linters.settings.custom.ulidmakeforbidden` entry, `linters.enable: ulidmakeforbidden`, and the `_test.go` exclusion)

- [ ] **Step 1: Write the analyzer test (failing)**

Create `gorules/analyzers/ulidmakeforbidden/ulidmakeforbidden_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package ulidmakeforbidden_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/holomush/holomush/gorules/analyzers/ulidmakeforbidden"
)

func TestAnalyzerFlagsUlidMakeAndIgnoresNegativeCases(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), ulidmakeforbidden.Analyzer,
		"example.com/positive",
		"example.com/negative",
	)
}
```

- [ ] **Step 2: Write the testdata**

Create `gorules/analyzers/ulidmakeforbidden/testdata/src/github.com/oklog/ulid/v2/ulid.go` (stub of the upstream package surface our rule consults):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Stub for analysistest. Mirrors the import path of github.com/oklog/ulid/v2
// and exports just the surface the rule consults.
package ulid

type ULID [16]byte

func Make() ULID { return ULID{} }
```

Create `gorules/analyzers/ulidmakeforbidden/testdata/src/example.com/positive/positive.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package positive

import "github.com/oklog/ulid/v2"

var _ = ulid.Make() // want `use idgen.New\(\) for entity IDs or core.NewULID\(\) for event IDs; ulid.Make\(\) uses math/rand`
```

Create `gorules/analyzers/ulidmakeforbidden/testdata/src/example.com/negative/negative.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package negative

import (
	"github.com/oklog/ulid/v2"
)

// A function literally named Make() defined locally MUST NOT match — the
// rule targets the upstream ulid package only.
type Local struct{}

func (Local) Make() ulid.ULID { return ulid.ULID{} }

var l Local
var _ = l.Make() // OK — method on local type
```

- [ ] **Step 3: Run the test (expect FAIL — analyzer not implemented yet)**

Run: `cd gorules && go test ./analyzers/ulidmakeforbidden/...`
Expected: build error, `undefined: ulidmakeforbidden.Analyzer`.

- [ ] **Step 4: Implement the analyzer**

Create `gorules/analyzers/ulidmakeforbidden/ulidmakeforbidden.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package ulidmakeforbidden implements the lint rule that forbids
// ulid.Make() in production code. ulid.Make() uses math/rand
// internally, violating the project-wide crypto/rand requirement.
// Use idgen.New() for entity IDs or core.NewULID() for event IDs.
//
// Test-file scope: this analyzer is excluded for `_test.go` via
// .golangci.yaml exclusions.rules; tests legitimately use ulid.Make()
// for fixture generation.
package ulidmakeforbidden

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

const (
	pkgPath  = "github.com/oklog/ulid/v2"
	funcName = "Make"
	message  = "use idgen.New() for entity IDs or core.NewULID() for event IDs; ulid.Make() uses math/rand"
)

var Analyzer = &analysis.Analyzer{
	Name:     "ulidmakeforbidden",
	Doc:      "forbids ulid.Make() (uses math/rand instead of crypto/rand)",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		if holomushlint.IsCallToFQSym(pass, call, pkgPath, funcName) {
			pass.Reportf(call.Pos(), "%s", message)
		}
	})
	return nil, nil
}
```

- [ ] **Step 5: Run the test (expect PASS)**

Run: `cd gorules && go test ./analyzers/ulidmakeforbidden/...`
Expected: `ok github.com/holomush/holomush/gorules/analyzers/ulidmakeforbidden`. Both positive and negative testdata packages pass their `// want` assertions.

- [ ] **Step 6: Create the analyzer's plugin.go (golangci-lint registration)**

Create `gorules/analyzers/ulidmakeforbidden/plugin.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package ulidmakeforbidden

import (
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() { register.Plugin("ulidmakeforbidden", newPlugin) }

func newPlugin(_ any) (register.LinterPlugin, error) { return &linterPlugin{}, nil }

type linterPlugin struct{}

func (linterPlugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{Analyzer}, nil
}

func (linterPlugin) GetLoadMode() string { return register.LoadModeTypesInfo }
```

- [ ] **Step 7: Add the analyzer's blank import to the aggregator**

Edit `gorules/plugin.go` to add a single blank-import line. Use the Edit tool, NOT Write — preserve the existing SPDX header and doc comment from Task 5 Step 2.

The bootstrap form (after Task 5):

```go
package holomushrules

// Blank imports — populated by Tasks 9–19 (one per analyzer).
// Bootstrap: no analyzers yet.
```

becomes:

```go
package holomushrules

// Blank imports — populated by Tasks 9–19 (one per analyzer).
import (
	_ "github.com/holomush/holomush/gorules/analyzers/ulidmakeforbidden"
)
```

(The "Bootstrap: no analyzers yet." comment is removed once the first import is added; subsequent tasks just append additional `_ "..."` lines, keeping alphabetical order.)

Run: `cd gorules && go build ./... && go test ./...`
Expected: both green.

- [ ] **Step 8: Enable the analyzer in `.golangci.yaml`**

Add the `linters.settings.custom.ulidmakeforbidden` entry (this is the first; subsequent tasks add additional entries to the same `custom:` block):

```yaml
    custom:
      ulidmakeforbidden:
        type: module
        description: forbids ulid.Make() (uses math/rand instead of crypto/rand)
        original-url: github.com/holomush/holomush
```

Add to `linters.enable`:

```yaml
    - ulidmakeforbidden
```

Add the `_test.go` exclusion under `linters.exclusions.rules` (immediately after the existing `gocritic`/`wrapcheck`/`errcheck` exclusions, around `.golangci.yaml:53-60`):

```yaml
      # ulidmakeforbidden replaces a ruleguard rule that only fired in
      # production because gocritic was excluded for _test.go. Tests
      # legitimately use ulid.Make() for fixture generation; preserve
      # that scope.
      - path: '_test\.go'
        linters:
          - ulidmakeforbidden
```

- [ ] **Step 9: Rebuild custom-gcl and verify lint passes on the real repo**

Run:

```bash
rm -f bin/custom-gcl
task lint:go
```

Expected: builds, runs, exits 0. The current production code does not use `ulid.Make()` (the project-wide invariant is already enforced by the bash check that this analyzer replaces); test files are excluded by the new rule.

- [ ] **Step 10: Commit**

Run:

```bash
jj --no-pager commit -m "feat(gorules): ulidmakeforbidden analyzer (holomush-46ya)

Re-implements the ULIDMakeForbidden ruleguard rule as a standard
go/analysis analyzer. Forbids ulid.Make() in production code (test
files excluded via .golangci.yaml exclusions).

Per spec §4.4.1.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Per-analyzer registration pattern (applies to Tasks 10–19)

For each subsequent analyzer (`<name>` ∈ `cursorpackageinternal`, `sceneopseventsappendonly`, `dekmaterialnojson`, `dekmaterialnogob`, `dekmaterialnoproto`, `dekmaterialnofmtformatting`, `dekmaterialnolog`, `dekmaterialnoslog`, `codeckeybytesallowlist`), repeat this pattern verbatim with the analyzer-specific name substituted:

1. **Create `gorules/analyzers/<name>/plugin.go`** — copy Task 9 Step 6's plugin.go template, swapping `ulidmakeforbidden` → `<name>`. The file is otherwise identical.
2. **Add a blank import** to `gorules/plugin.go` for the new analyzer subpackage. Imports remain in alphabetical order.
3. **Add a `linters.settings.custom.<name>` entry** to `.golangci.yaml`. Use the `description:` text from spec §4.5.2 — verbatim — for that analyzer.
4. **Add `<name>` to `linters.enable`** in `.golangci.yaml`.

The per-task instructions below specify the analyzer-specific code (analyzer logic, sinks, testdata) and treat the registration steps above as boilerplate.

### Task 10: `cursorpackageinternal` analyzer (TDD)

This analyzer flags ANY reference (function call, type usage, value identifier) to a symbol exported from `github.com/holomush/holomush/internal/eventbus/cursor`, unless the analyzed package's path is allowlisted. The rule is broader than the existing ruleguard rule's per-symbol enumeration but functionally equivalent for the symbols actually exported today (per spec §4.4.2 + reviewer pass-1 finding 2).

**Files:**

- Create: `gorules/analyzers/cursorpackageinternal/cursorpackageinternal.go`
- Create: `gorules/analyzers/cursorpackageinternal/cursorpackageinternal_test.go`
- Create: `gorules/analyzers/cursorpackageinternal/testdata/src/github.com/holomush/holomush/internal/eventbus/cursor/cursor.go`
- Create: `gorules/analyzers/cursorpackageinternal/testdata/src/example.com/positive/positive.go`
- Create: `gorules/analyzers/cursorpackageinternal/testdata/src/github.com/holomush/holomush/internal/eventbus/allow/allow.go`
- Create: `gorules/analyzers/cursorpackageinternal/testdata/src/github.com/holomush/holomush/internal/grpc/allow/allow.go`
- Create: `gorules/analyzers/cursorpackageinternal/plugin.go`
- Modify: `gorules/plugin.go` (add blank import)
- Modify: `.golangci.yaml`

- [ ] **Step 1: Write the cursor stub**

Create `testdata/src/github.com/holomush/holomush/internal/eventbus/cursor/cursor.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cursor

type OwnerKind int

const (
	OwnerUnspecified OwnerKind = iota
	OwnerHost
	OwnerPlugin
)

const CurrentVersion = 1

type Owner struct {
	Kind OwnerKind
}

type Cursor struct {
	Owner Owner
}

type HostCursor struct {
	Cursor Cursor
}

func CurrentEpoch() int { return 0 }

func Encode(c Cursor) string { return "" }

func Decode(s string) (Cursor, error) { return Cursor{}, nil }
```

- [ ] **Step 2: Write positive testdata**

Create `testdata/src/example.com/positive/positive.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package positive

import "github.com/holomush/holomush/internal/eventbus/cursor"

var _ = cursor.Encode(cursor.Cursor{}) // want `internal/eventbus/cursor is host-internal — clients and plugins must not import it`

var _ = cursor.CurrentVersion // want `internal/eventbus/cursor is host-internal — clients and plugins must not import it`

var _ cursor.OwnerKind = cursor.OwnerHost // want `internal/eventbus/cursor is host-internal — clients and plugins must not import it`

var _ = cursor.Owner{Kind: cursor.OwnerPlugin} // want `internal/eventbus/cursor is host-internal — clients and plugins must not import it`
```

- [ ] **Step 3: Write allowlist (must-NOT-flag) testdata**

Create `testdata/src/github.com/holomush/holomush/internal/eventbus/allow/allow.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// This package's import path is under internal/eventbus/, which is on
// the cursorpackageinternal allowlist. References to cursor MUST NOT
// flag.
package allow

import "github.com/holomush/holomush/internal/eventbus/cursor"

var _ = cursor.Encode(cursor.Cursor{})
```

Create `testdata/src/github.com/holomush/holomush/internal/grpc/allow/allow.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// internal/grpc/ is allowlisted (it decodes/encodes cursors at the RPC
// boundary).
package allow

import "github.com/holomush/holomush/internal/eventbus/cursor"

var _ = cursor.Decode("")
```

- [ ] **Step 4: Write the analyzer test**

Create `gorules/analyzers/cursorpackageinternal/cursorpackageinternal_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cursorpackageinternal_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/holomush/holomush/gorules/analyzers/cursorpackageinternal"
)

func TestAnalyzerFlagsCursorRefsExceptFromAllowlistedPackages(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), cursorpackageinternal.Analyzer,
		"example.com/positive",
		"github.com/holomush/holomush/internal/eventbus/allow",
		"github.com/holomush/holomush/internal/grpc/allow",
	)
}
```

- [ ] **Step 5: Implement the analyzer**

Create `gorules/analyzers/cursorpackageinternal/cursorpackageinternal.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package cursorpackageinternal implements the lint rule that forbids
// importing or referencing symbols from
// github.com/holomush/holomush/internal/eventbus/cursor outside the
// host's natural homes (eventbus, grpc, web, plugin/goplugin,
// plugin/hostfunc).
//
// Implementation note: walks all *ast.Ident nodes that resolve via
// pass.TypesInfo.Uses to an object whose package is the cursor
// package, and emits a single diagnostic per reference. This is
// broader than the previous ruleguard rule's per-symbol enumeration
// (it covers any future cursor symbol automatically) but functionally
// equivalent for the existing exported surface.
package cursorpackageinternal

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

const (
	cursorPkg = "github.com/holomush/holomush/internal/eventbus/cursor"
	message   = "internal/eventbus/cursor is host-internal — clients and plugins must not import it"
)

var allowlist = []string{
	"github.com/holomush/holomush/internal/eventbus",
	"github.com/holomush/holomush/internal/grpc",
	"github.com/holomush/holomush/internal/web",
	"github.com/holomush/holomush/internal/plugin/goplugin",
	"github.com/holomush/holomush/internal/plugin/hostfunc",
}

var Analyzer = &analysis.Analyzer{
	Name:     "cursorpackageinternal",
	Doc:      "forbids references to internal/eventbus/cursor outside host packages",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	if pass.Pkg == nil {
		return nil, nil
	}
	if holomushlint.PackagePathMatchesAny(pass.Pkg.Path(), allowlist) {
		return nil, nil
	}
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	// Track positions we've already reported to avoid duplicate diagnostics
	// when the same SelectorExpr is visited at multiple ast levels.
	reported := map[ast.Node]struct{}{}
	insp.Preorder([]ast.Node{(*ast.Ident)(nil)}, func(n ast.Node) {
		ident := n.(*ast.Ident)
		obj := pass.TypesInfo.Uses[ident]
		if obj == nil {
			return
		}
		if obj.Pkg() == nil {
			return
		}
		if obj.Pkg().Path() != cursorPkg {
			return
		}
		if _, dup := reported[ident]; dup {
			return
		}
		reported[ident] = struct{}{}
		pass.Reportf(ident.Pos(), "%s", message)
	})
	return nil, nil
}
```

- [ ] **Step 6: Run the test**

Run: `cd gorules && go test ./analyzers/cursorpackageinternal/...`
Expected: green; the four `// want` assertions in `positive.go` fire; allow packages produce no diagnostics.

- [ ] **Step 7: Register, enable, rebuild, lint, commit**

Apply the per-analyzer registration pattern (sub-steps 1–4 in the "Per-analyzer registration pattern" section above) for `cursorpackageinternal`. Specifically:

1. Create `gorules/analyzers/cursorpackageinternal/plugin.go` (copy Task 9 Step 6, swap `ulidmakeforbidden` → `cursorpackageinternal`).
2. Add `_ "github.com/holomush/holomush/gorules/analyzers/cursorpackageinternal"` to `gorules/plugin.go`'s import block (alphabetical order).
3. Add to `.golangci.yaml` `linters.settings.custom`:

   ```yaml
         cursorpackageinternal:
           type: module
           description: forbids references to internal/eventbus/cursor outside host packages
           original-url: github.com/holomush/holomush
   ```

4. Add `- cursorpackageinternal` to `.golangci.yaml` `linters.enable`.

Run:

```bash
rm -f bin/custom-gcl
task lint:go
cd gorules && go test ./...
```

Both expected green. The analyzer fires only on cross-cursor references outside the five allowlisted prefixes — matching the existing ruleguard rule's coverage on production code.

Run:

```bash
jj --no-pager commit -m "feat(gorules): cursorpackageinternal analyzer (holomush-46ya)

Re-implements CursorPackageInternal as a standard go/analysis analyzer.
Walks *ast.Ident nodes resolving to objects in the cursor package and
flags every reference from non-allowlisted packages. The implementation
is broader than the previous per-symbol-enumeration ruleguard rule
(catches any future cursor symbol automatically) but functionally
equivalent for the existing exported surface.

Per spec §4.4.2.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Task 11: `sceneopseventsappendonly` analyzer (TDD)

**Files:**

- Create: `gorules/analyzers/sceneopseventsappendonly/sceneopseventsappendonly.go`
- Create: `gorules/analyzers/sceneopseventsappendonly/sceneopseventsappendonly_test.go`
- Create: `gorules/analyzers/sceneopseventsappendonly/testdata/src/example.com/positive/positive.go`
- Create: `gorules/analyzers/sceneopseventsappendonly/testdata/src/example.com/negative/negative.go`
- Create: `gorules/analyzers/sceneopseventsappendonly/plugin.go`
- Modify: `gorules/plugin.go` (add blank import)
- Modify: `.golangci.yaml`

- [ ] **Step 1: Write positive and negative testdata**

Create `testdata/src/example.com/positive/positive.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package positive

import "context"

// txExec mimics pgx's tx.Exec / tx.Query / tx.QueryRow shape so the
// analyzer matches by method name.
type tx struct{}

func (tx) Exec(ctx context.Context, sql string, args ...any) (any, error) { return nil, nil }
func (tx) Query(ctx context.Context, sql string, args ...any) (any, error) { return nil, nil }
func (tx) QueryRow(ctx context.Context, sql string, args ...any) any       { return nil }

func updates(ctx context.Context) {
	var t tx
	t.Exec(ctx, "UPDATE scene_ops_events SET op = 'noop' WHERE id = 1") // want `scene_ops_events is append-only`
	t.Query(ctx, "DELETE FROM scene_ops_events WHERE id = 1")            // want `scene_ops_events is append-only`
	t.QueryRow(ctx, "TRUNCATE scene_ops_events")                         // want `scene_ops_events is append-only`
	t.QueryRow(ctx, "TRUNCATE TABLE scene_ops_events")                   // want `scene_ops_events is append-only`
	// Concatenation chain: must also fire.
	t.Exec(ctx, "UPDATE "+"scene_ops_events"+" SET op = 'noop'") // want `scene_ops_events is append-only`
	// Named const: must also fire.
	t.Exec(ctx, deleteSQL) // want `scene_ops_events is append-only`
}

const deleteSQL = "DELETE FROM scene_ops_events WHERE id = 1"
```

Create `testdata/src/example.com/negative/negative.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package negative

import "context"

type tx struct{}

func (tx) Exec(ctx context.Context, sql string, args ...any) (any, error) { return nil, nil }

func inserts(ctx context.Context) {
	var t tx
	// INSERT is allowed (the table is append-only via INSERT).
	t.Exec(ctx, "INSERT INTO scene_ops_events (id, op) VALUES ($1, $2)", 1, "create")
	// UPDATE on a different table — not flagged.
	t.Exec(ctx, "UPDATE other_table SET x = 1")
	// String-construction shape we don't track (e.g., fmt.Sprintf): not flagged.
	t.Exec(ctx, sprintf("UPDATE %s SET x = 1", "scene_ops_events"))
}

func sprintf(format string, args ...any) string { return format }
```

- [ ] **Step 2: Write the analyzer test**

Create `gorules/analyzers/sceneopseventsappendonly/sceneopseventsappendonly_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package sceneopseventsappendonly_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/holomush/holomush/gorules/analyzers/sceneopseventsappendonly"
)

func TestAnalyzerFlagsForbiddenSQLAgainstSceneOpsEvents(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), sceneopseventsappendonly.Analyzer,
		"example.com/positive",
		"example.com/negative",
	)
}
```

- [ ] **Step 3: Implement the analyzer**

Create `gorules/analyzers/sceneopseventsappendonly/sceneopseventsappendonly.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package sceneopseventsappendonly implements the lint rule that
// forbids UPDATE/DELETE/TRUNCATE statements against the
// scene_ops_events table. The table is the append-only ops journal
// for the core-scenes plugin (Phase 3 design P3.D3, P3.D4).
//
// Targets: tx.Exec / tx.Query / tx.QueryRow calls (any receiver type;
// pgx receivers are plural). String-extraction supports literal,
// `+`-chain, and named-const shapes. Anything else (fmt.Sprintf,
// concat with a runtime variable, ...) is silently passed through.
package sceneopseventsappendonly

import (
	"go/ast"
	"regexp"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

var forbiddenRegex = regexp.MustCompile(`(?i)(?:update\s+scene_ops_events|delete\s+from\s+scene_ops_events|truncate(?:\s+table)?\s+scene_ops_events)`)

const message = "scene_ops_events is append-only (Phase 3 design P3.D3/D4): use a new INSERT via recordOpsEventTx to record corrections instead of UPDATE/DELETE/TRUNCATE"

var methods = map[string]bool{"Exec": true, "Query": true, "QueryRow": true}

var Analyzer = &analysis.Analyzer{
	Name:     "sceneopseventsappendonly",
	Doc:      "forbids UPDATE/DELETE/TRUNCATE against scene_ops_events",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return
		}
		if !methods[sel.Sel.Name] {
			return
		}
		if len(call.Args) < 2 {
			return
		}
		// args[0] is ctx, args[1] is the SQL string.
		sql, ok := holomushlint.ExtractStringConst(pass, call.Args[1])
		if !ok {
			return
		}
		if forbiddenRegex.MatchString(sql) {
			pass.Reportf(call.Args[1].Pos(), "%s", message)
		}
	})
	return nil, nil
}
```

- [ ] **Step 4: Run the test**

Run: `cd gorules && go test ./analyzers/sceneopseventsappendonly/...`
Expected: green; six `// want` assertions in positive.go fire; negative.go produces zero diagnostics.

- [ ] **Step 5: Register, enable, rebuild, lint, commit**

Apply the per-analyzer registration pattern for `sceneopseventsappendonly`:

1. Create `gorules/analyzers/sceneopseventsappendonly/plugin.go`.
2. Add `_ "github.com/holomush/holomush/gorules/analyzers/sceneopseventsappendonly"` to `gorules/plugin.go`.
3. Add to `.golangci.yaml` `linters.settings.custom`:

   ```yaml
         sceneopseventsappendonly:
           type: module
           description: forbids UPDATE/DELETE/TRUNCATE against scene_ops_events
           original-url: github.com/holomush/holomush
   ```

4. Add `- sceneopseventsappendonly` to `linters.enable`.

Run:

```bash
rm -f bin/custom-gcl
task lint:go
cd gorules && go test ./...
```

Both expected green. The analyzer fires only against `scene_ops_events`-targeted UPDATE/DELETE/TRUNCATE in production code; the legitimate INSERT writer in `plugins/core-scenes/ops_events.go` is not flagged.

Run:

```bash
jj --no-pager commit -m "feat(gorules): sceneopseventsappendonly analyzer (holomush-46ya)

Re-implements SceneOpsEventsAppendOnly as a standard go/analysis
analyzer with broader string-extraction coverage than the original
ruleguard rule:

- Literal STRING BasicLit (parity with ruleguard)
- +-chain of literals (parity with ruleguard)
- Named string-typed const, resolved via pass.TypesInfo.Types[expr].Value
  (NEW — closes the gap the original rule's godoc admitted)

Per spec §4.4.3.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Task 12: Common `dek.Material` testdata stub (extracted from Tasks 13–18)

The six DEK no-sink analyzers share testdata for `dek.Material`. Tasks 13–18 each create their own `testdata/src/github.com/holomush/holomush/internal/eventbus/crypto/dek/dek.go` stub (analysistest does not share testdata across analyzer packages by design — each analyzer's testdata directory is isolated). The stub is identical across all six. This task documents the canonical content; each subsequent task creates a copy.

**Canonical `dek.go` stub** (used in Tasks 13–18):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Stub of internal/eventbus/crypto/dek for analysistest. Mirrors only
// the API surface the dekmaterialno* analyzers consult (Material type
// + NewMaterial constructor; the AsCodecKey method is irrelevant to
// the rules and intentionally elided to keep the stub minimal).
package dek

type Material struct {
	bytes []byte
}

func NewMaterial(b []byte) *Material { return &Material{bytes: append([]byte(nil), b...)} }
```

No commit at this task — Tasks 13–18 each copy the stub into their own testdata trees and commit alongside their analyzer.

### Task 13: `dekmaterialnojson` analyzer (TDD)

**Files:**

- Create: `gorules/analyzers/dekmaterialnojson/dekmaterialnojson.go`
- Create: `gorules/analyzers/dekmaterialnojson/dekmaterialnojson_test.go`
- Create: `gorules/analyzers/dekmaterialnojson/testdata/src/github.com/holomush/holomush/internal/eventbus/crypto/dek/dek.go` (canonical stub from Task 12)
- Create: `gorules/analyzers/dekmaterialnojson/testdata/src/example.com/positive/positive.go`
- Create: `gorules/analyzers/dekmaterialnojson/testdata/src/example.com/negative/negative.go`
- Create: `gorules/analyzers/dekmaterialnojson/plugin.go`
- Modify: `gorules/plugin.go` (add blank import), `.golangci.yaml`

- [ ] **Step 1: Write the dek stub** (as Task 12 specifies)

- [ ] **Step 2: Write positive and negative testdata**

Create `testdata/src/example.com/positive/positive.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package positive

import (
	"encoding/json"
	"io"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func leakViaMarshal(m *dek.Material) ([]byte, error) {
	return json.Marshal(m) // want `INV-27: dek.Material MUST NOT be passed to encoding/json`
}

func leakViaMarshalIndent(m *dek.Material) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ") // want `INV-27: dek.Material MUST NOT be passed to encoding/json`
}

func leakViaEncoder(m *dek.Material, w io.Writer) error {
	return json.NewEncoder(w).Encode(m) // want `INV-27: dek.Material MUST NOT be passed to encoding/json`
}
```

Create `testdata/src/example.com/negative/negative.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package negative

import (
	"encoding/json"
)

type SafeStruct struct {
	Name string
}

func okToMarshalOtherTypes(s SafeStruct) ([]byte, error) {
	return json.Marshal(s) // OK — not dek.Material
}
```

- [ ] **Step 3: Write the analyzer test**

Create `gorules/analyzers/dekmaterialnojson/dekmaterialnojson_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dekmaterialnojson_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/holomush/holomush/gorules/analyzers/dekmaterialnojson"
)

func TestAnalyzerFlagsDEKMaterialPassedToJSONSinks(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), dekmaterialnojson.Analyzer,
		"example.com/positive",
		"example.com/negative",
	)
}
```

- [ ] **Step 4: Implement the analyzer**

Create `gorules/analyzers/dekmaterialnojson/dekmaterialnojson.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package dekmaterialnojson forbids passing dek.Material (or
// *dek.Material) to encoding/json sinks (json.Marshal,
// json.MarshalIndent, (*json.Encoder).Encode). Part of INV-27 (Material
// non-leakage).
package dekmaterialnojson

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

const sinkDescription = "encoding/json"

var sinks = []holomushlint.Sink{
	{PkgPath: "encoding/json", FuncName: "Marshal"},
	{PkgPath: "encoding/json", FuncName: "MarshalIndent"},
	{PkgPath: "encoding/json", RecvTypeName: "Encoder", MethodName: "Encode"},
}

var Analyzer = &analysis.Analyzer{
	Name:     "dekmaterialnojson",
	Doc:      "INV-27: dek.Material MUST NOT be passed to encoding/json",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		if !holomushlint.CallTargetsAnySink(pass, call, sinks) {
			return
		}
		for _, arg := range call.Args {
			if holomushlint.IsDEKMaterial(pass.TypesInfo.TypeOf(arg)) {
				pass.Reportf(arg.Pos(),
					"INV-27: dek.Material MUST NOT be passed to %s — Material is opaque by design (see internal/eventbus/crypto/dek/material.go and bead holomush-46ya for context)",
					sinkDescription)
				return
			}
		}
	})
	return nil, nil
}
```

- [ ] **Step 5: Run the test**

Run: `cd gorules && go test ./analyzers/dekmaterialnojson/...`
Expected: green.

- [ ] **Step 6: Register, enable, rebuild, lint, commit**

Apply the per-analyzer registration pattern for `dekmaterialnojson`:

1. Create `gorules/analyzers/dekmaterialnojson/plugin.go`.
2. Add `_ "github.com/holomush/holomush/gorules/analyzers/dekmaterialnojson"` to `gorules/plugin.go`.
3. Add to `.golangci.yaml` `linters.settings.custom`:

   ```yaml
         dekmaterialnojson:
           type: module
           description: "INV-27: dek.Material MUST NOT be passed to encoding/json"
           original-url: github.com/holomush/holomush
   ```

4. Add `- dekmaterialnojson` to `linters.enable`.

```bash
rm -f bin/custom-gcl && task lint:go && cd gorules && go test ./...
```

Both green. (Today's `dek/` package has no `json.Marshal(m)` callers; the static API surface test was the previous defense.)

```bash
jj --no-pager commit -m "feat(gorules): dekmaterialnojson analyzer (holomush-46ya)

Restores INV-27 enforcement at the lint layer for the encoding/json
sink class. Targets json.Marshal, json.MarshalIndent, and
(*json.Encoder).Encode; flags any dek.Material or *dek.Material
argument.

Per spec §4.4.4–4.4.9.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Task 14: `dekmaterialnogob` analyzer (TDD)

Identical structure to Task 13. Differences:

- Package directory: `gorules/analyzers/dekmaterialnogob/`
- Sinks:

```go
var sinks = []holomushlint.Sink{
	{PkgPath: "encoding/gob", RecvTypeName: "Encoder", MethodName: "Encode"},
	{PkgPath: "encoding/gob", FuncName: "Register"},
}
```

- `sinkDescription = "encoding/gob"`
- Analyzer Name: `"dekmaterialnogob"`
- Diagnostic message uses `"encoding/gob"`.
- Positive testdata uses `gob.NewEncoder(w).Encode(m)` and `gob.Register(m)`.
- Negative testdata calls `gob.NewEncoder(w).Encode(safeStruct)`.

- [ ] **Step 1: Copy the canonical `dek.go` stub from Task 12 into `testdata/src/github.com/holomush/holomush/internal/eventbus/crypto/dek/dek.go`**

- [ ] **Step 2: Positive testdata**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package positive

import (
	"encoding/gob"
	"io"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func leakViaEncode(m *dek.Material, w io.Writer) error {
	return gob.NewEncoder(w).Encode(m) // want `INV-27: dek.Material MUST NOT be passed to encoding/gob`
}

func leakViaRegister(m *dek.Material) {
	gob.Register(m) // want `INV-27: dek.Material MUST NOT be passed to encoding/gob`
}
```

- [ ] **Step 3: Negative testdata**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package negative

import (
	"encoding/gob"
	"io"
)

type SafeStruct struct{ Name string }

func ok(s SafeStruct, w io.Writer) error {
	return gob.NewEncoder(w).Encode(s)
}
```

- [ ] **Step 4: Test file** — copy Task 13 Step 3 verbatim, swapping `dekmaterialnojson` → `dekmaterialnogob`.

- [ ] **Step 5: Implementation file** — copy Task 13 Step 4 verbatim and apply the differences listed above.

- [ ] **Step 6: Run test**

Run: `cd gorules && go test ./analyzers/dekmaterialnogob/...`
Expected: green.

- [ ] **Step 7: Register, enable, rebuild, lint, commit**

Apply the per-analyzer registration pattern for `dekmaterialnogob`:

1. Create `gorules/analyzers/dekmaterialnogob/plugin.go`.
2. Add `_ "github.com/holomush/holomush/gorules/analyzers/dekmaterialnogob"` to `gorules/plugin.go`.
3. Add to `.golangci.yaml` `linters.settings.custom`:

   ```yaml
         dekmaterialnogob:
           type: module
           description: "INV-27: dek.Material MUST NOT be passed to encoding/gob"
           original-url: github.com/holomush/holomush
   ```

4. Add `- dekmaterialnogob` to `linters.enable`.

```bash
rm -f bin/custom-gcl && task lint:go && cd gorules && go test ./...
```

```bash
jj --no-pager commit -m "feat(gorules): dekmaterialnogob analyzer (holomush-46ya)

Restores INV-27 enforcement for the encoding/gob sink class. Targets
(*gob.Encoder).Encode and gob.Register.

Per spec §4.4.4–4.4.9.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Task 15: `dekmaterialnoproto` analyzer (TDD)

Same structure. Differences:

- Package directory: `gorules/analyzers/dekmaterialnoproto/`
- Sinks:

```go
var sinks = []holomushlint.Sink{
	{PkgPath: "google.golang.org/protobuf/proto", FuncName: "Marshal"},
	{PkgPath: "google.golang.org/protobuf/proto", RecvTypeName: "MarshalOptions", MethodName: "Marshal"},
}
```

- `sinkDescription = "google.golang.org/protobuf/proto"`
- Analyzer Name: `"dekmaterialnoproto"`.

> Note: `dek.Material` does not implement `proto.Message`, so `proto.Marshal(m)` does not type-check today. The positive testdata exercises a stub `proto` package whose `Marshal` accepts `any` to keep analysistest happy. This is an intentional forward-defensive rule (per spec §4.4.4–4.4.9 reviewer pass-1 note).

- [ ] **Step 1: Stubs**

`testdata/src/github.com/holomush/holomush/internal/eventbus/crypto/dek/dek.go`: canonical stub.

`testdata/src/google.golang.org/protobuf/proto/proto.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Stub for analysistest. Marshal and MarshalOptions.Marshal accept any
// type so the testdata can pass *dek.Material without the real
// proto.Message interface constraint.
package proto

type MarshalOptions struct {
	Deterministic bool
}

func Marshal(m any) ([]byte, error) { return nil, nil }

func (MarshalOptions) Marshal(m any) ([]byte, error) { return nil, nil }
```

- [ ] **Step 2: Positive testdata**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package positive

import (
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func leakViaMarshal(m *dek.Material) ([]byte, error) {
	return proto.Marshal(m) // want `INV-27: dek.Material MUST NOT be passed to google.golang.org/protobuf/proto`
}

func leakViaOpts(m *dek.Material) ([]byte, error) {
	return proto.MarshalOptions{Deterministic: true}.Marshal(m) // want `INV-27: dek.Material MUST NOT be passed to google.golang.org/protobuf/proto`
}
```

- [ ] **Step 3: Negative testdata**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package negative

import (
	"google.golang.org/protobuf/proto"
)

type SafeMessage struct{ Name string }

func ok(m SafeMessage) ([]byte, error) {
	return proto.Marshal(m)
}
```

- [ ] **Step 4: Test, implementation, register, enable, rebuild, lint, commit** — same shape as Tasks 13/14. Apply the per-analyzer registration pattern for `dekmaterialnoproto`: create the subpackage's `plugin.go`, add the blank import to `gorules/plugin.go`, add the `linters.settings.custom.dekmaterialnoproto` entry (description: `"INV-27: dek.Material MUST NOT be passed to google.golang.org/protobuf/proto"`), and `- dekmaterialnoproto` to `linters.enable`.

```bash
jj --no-pager commit -m "feat(gorules): dekmaterialnoproto analyzer (holomush-46ya)

Restores INV-27 enforcement for the protobuf sink class. Forward-
defensive: dek.Material does not implement proto.Message today, so the
positive case does not type-check against current code. The rule fires
if Material ever gains a proto.Message method set (e.g., via accidentally
added Reset()/String()/ProtoMessage() methods, or via a generated stub).

Per spec §4.4.4–4.4.9.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Task 16: `dekmaterialnofmtformatting` analyzer (TDD)

Differences:

- Package: `gorules/analyzers/dekmaterialnofmtformatting/`
- Sinks (all free functions in `fmt`):

```go
var sinks = []holomushlint.Sink{
	{PkgPath: "fmt", FuncName: "Sprint"},
	{PkgPath: "fmt", FuncName: "Sprintf"},
	{PkgPath: "fmt", FuncName: "Sprintln"},
	{PkgPath: "fmt", FuncName: "Print"},
	{PkgPath: "fmt", FuncName: "Printf"},
	{PkgPath: "fmt", FuncName: "Println"},
	{PkgPath: "fmt", FuncName: "Fprint"},
	{PkgPath: "fmt", FuncName: "Fprintf"},
	{PkgPath: "fmt", FuncName: "Fprintln"},
	{PkgPath: "fmt", FuncName: "Errorf"},
}
```

- `sinkDescription = "fmt formatting"`
- Analyzer Name: `"dekmaterialnofmtformatting"`.

- [ ] **Step 1: Stub** — canonical dek.go.

- [ ] **Step 2: Positive testdata** — exercise at least three of the ten sinks (Sprintf, Errorf, Printf):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package positive

import (
	"fmt"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func leakViaSprintf(m *dek.Material) string {
	return fmt.Sprintf("%v", m) // want `INV-27: dek.Material MUST NOT be passed to fmt formatting`
}

func leakViaErrorf(m *dek.Material) error {
	return fmt.Errorf("material: %v", m) // want `INV-27: dek.Material MUST NOT be passed to fmt formatting`
}

func leakViaPrintf(m *dek.Material) {
	fmt.Printf("%v\n", m) // want `INV-27: dek.Material MUST NOT be passed to fmt formatting`
}
```

- [ ] **Step 3: Negative testdata**:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package negative

import "fmt"

type SafeStruct struct{ Name string }

var _ = fmt.Sprintf("%v", SafeStruct{Name: "ok"})
```

- [ ] **Step 4: Test, implementation, register, enable, rebuild, lint, commit.** Apply the per-analyzer registration pattern for `dekmaterialnofmtformatting`: subpackage `plugin.go`, blank import in aggregator, `linters.settings.custom.dekmaterialnofmtformatting` entry (description: `"INV-27: dek.Material MUST NOT be passed to fmt formatting"`), and `- dekmaterialnofmtformatting` to `linters.enable`.

```bash
jj --no-pager commit -m "feat(gorules): dekmaterialnofmtformatting analyzer (holomush-46ya)

Restores INV-27 enforcement for fmt formatting sinks. Targets ten
fmt.* free functions: Sprint, Sprintf, Sprintln, Print, Printf,
Println, Fprint, Fprintf, Fprintln, Errorf.

Per spec §4.4.4–4.4.9.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Task 17: `dekmaterialnolog` analyzer (TDD)

Differences:

- Package: `gorules/analyzers/dekmaterialnolog/`
- Sinks (free functions + Logger methods in `log`):

```go
var sinks = []holomushlint.Sink{
	// free functions
	{PkgPath: "log", FuncName: "Print"},
	{PkgPath: "log", FuncName: "Printf"},
	{PkgPath: "log", FuncName: "Println"},
	{PkgPath: "log", FuncName: "Fatal"},
	{PkgPath: "log", FuncName: "Fatalf"},
	{PkgPath: "log", FuncName: "Fatalln"},
	{PkgPath: "log", FuncName: "Panic"},
	{PkgPath: "log", FuncName: "Panicf"},
	{PkgPath: "log", FuncName: "Panicln"},
	// *log.Logger methods
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Print"},
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Printf"},
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Println"},
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Fatal"},
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Fatalf"},
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Fatalln"},
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Panic"},
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Panicf"},
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Panicln"},
}
```

- `sinkDescription = "log"`
- Analyzer Name: `"dekmaterialnolog"`.

- [ ] **Step 1–3:** stubs + positive (exercise `log.Printf` + `(*log.Logger).Printf`) + negative testdata.

- [ ] **Step 4–7:** test, implementation, register, enable, rebuild, lint, commit. Apply the per-analyzer registration pattern for `dekmaterialnolog`: subpackage `plugin.go`, blank import in aggregator, `linters.settings.custom.dekmaterialnolog` entry (description: `"INV-27: dek.Material MUST NOT be passed to log"`), and `- dekmaterialnolog` to `linters.enable`.

```bash
jj --no-pager commit -m "feat(gorules): dekmaterialnolog analyzer (holomush-46ya)

Restores INV-27 enforcement for the standard log package. Targets nine
package-level functions (Print*, Fatal*, Panic*) and the matching
methods on *log.Logger.

Per spec §4.4.4–4.4.9.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Task 18: `dekmaterialnoslog` analyzer (TDD)

Differences:

- Package: `gorules/analyzers/dekmaterialnoslog/`
- Sinks (free functions + *Logger methods in `log/slog`):

```go
var sinks = []holomushlint.Sink{
	// free functions (use the package-level default logger)
	{PkgPath: "log/slog", FuncName: "Info"},
	{PkgPath: "log/slog", FuncName: "Debug"},
	{PkgPath: "log/slog", FuncName: "Warn"},
	{PkgPath: "log/slog", FuncName: "Error"},
	{PkgPath: "log/slog", FuncName: "Log"},
	{PkgPath: "log/slog", FuncName: "Any"},
	{PkgPath: "log/slog", FuncName: "Group"},
	// *slog.Logger methods (mirror set)
	{PkgPath: "log/slog", RecvTypeName: "Logger", MethodName: "Info"},
	{PkgPath: "log/slog", RecvTypeName: "Logger", MethodName: "Debug"},
	{PkgPath: "log/slog", RecvTypeName: "Logger", MethodName: "Warn"},
	{PkgPath: "log/slog", RecvTypeName: "Logger", MethodName: "Error"},
	{PkgPath: "log/slog", RecvTypeName: "Logger", MethodName: "Log"},
}
```

- `sinkDescription = "log/slog"`
- Analyzer Name: `"dekmaterialnoslog"`.

> Implementation note: `slog.Any(key, value)` and `slog.Group(key, args...)` build attrs that flow into a logger call. The analyzer flags the construction itself (where Material first leaks into the slog data path), not downstream logger calls. This is sufficient because the construction is where Material has type identity at the call boundary.

- [ ] **Step 1–7:** same shape. Apply the per-analyzer registration pattern for `dekmaterialnoslog`: subpackage `plugin.go`, blank import in aggregator, `linters.settings.custom.dekmaterialnoslog` entry (description: `"INV-27: dek.Material MUST NOT be passed to log/slog"`), and `- dekmaterialnoslog` to `linters.enable`.

```bash
jj --no-pager commit -m "feat(gorules): dekmaterialnoslog analyzer (holomush-46ya)

Restores INV-27 enforcement for log/slog. Targets seven slog.* free
functions (Info, Debug, Warn, Error, Log, Any, Group) and five
*slog.Logger methods.

Per spec §4.4.4–4.4.9.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Task 19: `codeckeybytesallowlist` analyzer (TDD)

This analyzer is structurally distinct from the no-sink rules. It walks `*ast.SelectorExpr` nodes resolving to the `Bytes` field of `codec.Key` and flags reads (not writes/constructions) from non-allowlisted packages.

**Files:**

- Create: `gorules/analyzers/codeckeybytesallowlist/codeckeybytesallowlist.go`
- Create: `gorules/analyzers/codeckeybytesallowlist/codeckeybytesallowlist_test.go`
- Create: `gorules/analyzers/codeckeybytesallowlist/testdata/src/github.com/holomush/holomush/internal/eventbus/codec/codec.go`
- Create: `gorules/analyzers/codeckeybytesallowlist/testdata/src/example.com/positive/positive.go`
- Create: `gorules/analyzers/codeckeybytesallowlist/testdata/src/example.com/negative/negative.go`
- Create: `gorules/analyzers/codeckeybytesallowlist/testdata/src/github.com/holomush/holomush/internal/eventbus/codec/allow/allow.go`
- Create: `gorules/analyzers/codeckeybytesallowlist/testdata/src/github.com/holomush/holomush/internal/eventbus/crypto/allow/allow.go`
- Create: `gorules/analyzers/codeckeybytesallowlist/plugin.go`
- Modify: `gorules/plugin.go` (add blank import), `.golangci.yaml`

- [ ] **Step 1: codec stub**

`testdata/src/github.com/holomush/holomush/internal/eventbus/codec/codec.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package codec

type KeyID uint64

type Key struct {
	ID    KeyID
	Bytes []byte
}
```

- [ ] **Step 2: Positive testdata** (each `// want` line is a read; the construction and assignment cases are NOT flagged):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package positive

import "github.com/holomush/holomush/internal/eventbus/codec"

func readValue(k codec.Key) []byte {
	return k.Bytes // want `INV-27 \(residual defense\): codec.Key.Bytes reads are restricted`
}

func readPointerExplicit(pk *codec.Key) []byte {
	return (*pk).Bytes // want `INV-27 \(residual defense\): codec.Key.Bytes reads are restricted`
}

func readPointerAuto(pk *codec.Key) []byte {
	return pk.Bytes // want `INV-27 \(residual defense\): codec.Key.Bytes reads are restricted`
}

func readIndex(k codec.Key) byte {
	return k.Bytes[0] // want `INV-27 \(residual defense\): codec.Key.Bytes reads are restricted`
}

func readSlice(k codec.Key, n int) []byte {
	return k.Bytes[:n] // want `INV-27 \(residual defense\): codec.Key.Bytes reads are restricted`
}
```

- [ ] **Step 3: Negative testdata** (writes — must NOT flag):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package negative

import "github.com/holomush/holomush/internal/eventbus/codec"

// Composite-literal construction is a WRITE — the field name is an
// *ast.Ident inside the struct literal, not a *ast.SelectorExpr.
// Must not flag, even from non-allowlisted package.
func construct(id codec.KeyID, b []byte) codec.Key {
	return codec.Key{ID: id, Bytes: b}
}

// Field assignment is also a WRITE.
func assign(k *codec.Key, b []byte) {
	k.Bytes = b
}
```

- [ ] **Step 4: Allowlist testdata** (allowlisted packages MAY read):

`testdata/src/github.com/holomush/holomush/internal/eventbus/codec/allow/allow.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// internal/eventbus/codec/allow is under the codec/ allowlist prefix
// — reads MUST NOT flag.
package allow

import "github.com/holomush/holomush/internal/eventbus/codec"

func ok(k codec.Key) []byte { return k.Bytes }
```

`testdata/src/github.com/holomush/holomush/internal/eventbus/crypto/allow/allow.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// internal/eventbus/crypto/allow is under the crypto/ allowlist prefix
// — reads MUST NOT flag.
package allow

import "github.com/holomush/holomush/internal/eventbus/codec"

func ok(k codec.Key) []byte { return k.Bytes }
```

- [ ] **Step 5: Analyzer test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package codeckeybytesallowlist_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/holomush/holomush/gorules/analyzers/codeckeybytesallowlist"
)

func TestAnalyzerFlagsCodecKeyBytesReadsExceptFromAllowlist(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), codeckeybytesallowlist.Analyzer,
		"example.com/positive",
		"example.com/negative",
		"github.com/holomush/holomush/internal/eventbus/codec/allow",
		"github.com/holomush/holomush/internal/eventbus/crypto/allow",
	)
}
```

- [ ] **Step 6: Implementation**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package codeckeybytesallowlist forbids reading codec.Key.Bytes
// outside the codec/ and crypto/ package trees. Reads in composite
// literals, field assignments, and other write contexts are NOT
// flagged — the rule targets read-side leakage only.
//
// Implementation: walks *ast.SelectorExpr nodes whose resolved
// Selection.Obj() equals codec.Key's Bytes field. Composite-literal
// keys (e.g., codec.Key{Bytes: x}) are *ast.Ident, not *ast.SelectorExpr,
// so they are not visited by this walker. Field assignments
// (k.Bytes = x) ARE *ast.SelectorExpr; we filter them out by checking
// whether the parent node is an *ast.AssignStmt with the SelectorExpr
// in the Lhs.
package codeckeybytesallowlist

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

const (
	codecPkg = "github.com/holomush/holomush/internal/eventbus/codec"
	keyType  = "Key"
	field    = "Bytes"
	message  = "INV-27 (residual defense): codec.Key.Bytes reads are restricted to internal/eventbus/codec/... and internal/eventbus/crypto/... — Material exposure goes through dek.Material.AsCodecKey only"
)

var allowlist = []string{
	"github.com/holomush/holomush/internal/eventbus/codec",
	"github.com/holomush/holomush/internal/eventbus/crypto",
}

var Analyzer = &analysis.Analyzer{
	Name:     "codeckeybytesallowlist",
	Doc:      "INV-27 residual defense: codec.Key.Bytes reads are restricted",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	if pass.Pkg == nil {
		return nil, nil
	}
	if holomushlint.PackagePathMatchesAny(pass.Pkg.Path(), allowlist) {
		return nil, nil
	}
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.WithStack([]ast.Node{(*ast.SelectorExpr)(nil)}, func(n ast.Node, push bool, stack []ast.Node) bool {
		if !push {
			return false
		}
		sel := n.(*ast.SelectorExpr)
		if sel.Sel.Name != field {
			return true
		}
		selection, ok := pass.TypesInfo.Selections[sel]
		if !ok {
			return true
		}
		if selection.Kind() != types.FieldVal {
			return true
		}
		recv := selection.Recv()
		if ptr, ok := recv.(*types.Pointer); ok {
			recv = ptr.Elem()
		}
		named, ok := recv.(*types.Named)
		if !ok {
			return true
		}
		if named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != codecPkg {
			return true
		}
		if named.Obj().Name() != keyType {
			return true
		}
		// Skip writes: SelectorExpr appears as the LHS of an AssignStmt.
		if isWriteContext(stack) {
			return true
		}
		pass.Reportf(sel.Pos(), "%s", message)
		return true
	})
	return nil, nil
}

// isWriteContext returns true when the SelectorExpr at the top of the
// stack is on the LHS of an assignment.
func isWriteContext(stack []ast.Node) bool {
	if len(stack) < 2 {
		return false
	}
	parent := stack[len(stack)-2]
	assign, ok := parent.(*ast.AssignStmt)
	if !ok {
		return false
	}
	target := stack[len(stack)-1]
	for _, lhs := range assign.Lhs {
		if lhs == target {
			return true
		}
	}
	return false
}
```

- [ ] **Step 7: Run test**

Run: `cd gorules && go test ./analyzers/codeckeybytesallowlist/...`
Expected: green; positive and negative test packages pass.

- [ ] **Step 8: Register, enable, rebuild, lint, commit**

Apply the per-analyzer registration pattern for `codeckeybytesallowlist` (the tenth and final analyzer):

1. Create `gorules/analyzers/codeckeybytesallowlist/plugin.go`.
2. Add `_ "github.com/holomush/holomush/gorules/analyzers/codeckeybytesallowlist"` to `gorules/plugin.go`.
3. Add to `.golangci.yaml` `linters.settings.custom`:

   ```yaml
         codeckeybytesallowlist:
           type: module
           description: "INV-27 residual defense: codec.Key.Bytes reads are restricted"
           original-url: github.com/holomush/holomush
   ```

4. Add `- codeckeybytesallowlist` to `linters.enable`.

After this task, `gorules/plugin.go` has all ten blank imports, `.golangci.yaml` `linters.enable` has all ten analyzer names, and `linters.settings.custom` has all ten entries.

Run:

```bash
rm -f bin/custom-gcl
task lint:go
cd gorules && go test ./...
```

Both green. Today's `internal/eventbus/crypto/dek/material.go:46` (`codec.Key{ID: id, Bytes: out}`) is a composite-literal construction — not flagged. Reads in `internal/eventbus/codec/...` and `internal/eventbus/crypto/...` are allowlisted. No production code outside those trees reads `codec.Key.Bytes`.

```bash
jj --no-pager commit -m "feat(gorules): codeckeybytesallowlist analyzer (holomush-46ya)

Restores INV-27 residual-defense enforcement for codec.Key.Bytes
reads. Allowlists internal/eventbus/codec/... and
internal/eventbus/crypto/... (where codec construction and key
material handling happen). Flags reads (selector expressions in
non-LHS positions) anywhere else.

Per CodeRabbit feedback, the rule covers BOTH value and pointer
receivers (k.Bytes where k is codec.Key, and pk.Bytes where pk is
*codec.Key) — Selection.Obj() resolves uniformly across the receiver
shape.

Per spec §4.4.10.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Phase 4: Integration

### Task 20: Update `lefthook.yaml`

**Files:**

- Modify: `lefthook.yaml` (line 43-area: `golangci-lint run --new-from-rev=HEAD~1` → `./bin/custom-gcl run --new-from-rev=HEAD~1`)

- [ ] **Step 1: Edit `lefthook.yaml`**

Find the existing entry:

```yaml
glob: "*.go"
run: golangci-lint run --new-from-rev=HEAD~1
stage_fixed: true
```

Replace with:

```yaml
glob: "*.go"
# Use the project's custom golangci-lint binary so the pre-commit
# coverage matches CI exactly. If bin/custom-gcl is missing, fail
# fast — running the system golangci-lint would silently lose the
# project analyzers and let real violations through.
run: ./bin/custom-gcl run --new-from-rev=HEAD~1
stage_fixed: true
```

- [ ] **Step 2: Verify the hook syntax**

Run: `lefthook validate`
Expected: exit 0; no schema errors.

- [ ] **Step 3: Commit**

```bash
jj --no-pager commit -m "build(lint): lefthook uses ./bin/custom-gcl (holomush-46ya)

Pre-commit lint hook now invokes the project's custom golangci-lint
binary so it has the same analyzer set as CI. If bin/custom-gcl is
missing the hook fails fast (the developer runs task lint to
rebuild) rather than silently falling back to the system binary
without the project analyzers.

Per spec §4.5.4.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Task 21: Bump CI version pin

**Files:**

- Modify: `.github/workflows/ci.yaml:39-40` (v2.11.3 → v2.11.4)

- [ ] **Step 1: Edit ci.yaml**

Replace lines 39-40's `v2.11.3` references with `v2.11.4`. Both the URL and the version arg:

```yaml
      - name: Install golangci-lint v2
        run: |
          curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/v2.11.4/install.sh \
            | sh -s -- -b "$(go env GOPATH)/bin" v2.11.4
```

- [ ] **Step 2: Verify alignment with `.custom-gcl.yml`**

Run: `rg -n 'v2\.11\.[0-9]+' .github/workflows/ci.yaml .custom-gcl.yml`
Expected: all matches show `v2.11.4`.

- [ ] **Step 3: Commit**

```bash
jj --no-pager commit -m "ci(lint): bump golangci-lint v2.11.3 -> v2.11.4 (holomush-46ya)

Aligns with .custom-gcl.yml's version pin. The standard golangci-lint
binary remains required at install time because 'golangci-lint custom'
is the entry point that builds bin/custom-gcl.

Per spec §4.5.5.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Task 22: Update stale doc comments in `internal/eventbus/crypto/dek/`

**Files:**

- Modify: `internal/eventbus/crypto/dek/material.go:7-9` (replace stale `gorules/codec_key_bytes_allowlist.go` reference)
- Modify: `internal/eventbus/crypto/dek/material.go:39-42` (replace stale `gorules/rules.go` reference)
- Modify: `internal/eventbus/crypto/dek/api_test.go:21-23` (replace stale "ruleguard rules in gorules/" + "bypass the ruleguards" references)

> Use the Edit tool's verbatim Before/After matching, not line numbers — the Before blocks below include enough surrounding context to match unambiguously.

- [ ] **Step 1: Edit `material.go` (the codec.Key.Bytes paragraph at lines 7-9)**

Before:

```go
// which constructs a substrate codec.Key inline. The codec.Key.Bytes
// field is the residual leakage path; reads are gated by the ruleguard
// rule at gorules/codec_key_bytes_allowlist.go.
```

After:

```go
// which constructs a substrate codec.Key inline. The codec.Key.Bytes
// field is the residual leakage path; reads are gated by the
// codeckeybytesallowlist analyzer in gorules/analyzers/.
```

- [ ] **Step 2: Edit `material.go` (the AsCodecKey docstring at lines 39-42)**

Before:

```go
// Material or sibling keys minted from the same Material. Reads of
// the returned key's Bytes field outside the codec/crypto package
// trees fail lint via the codec.Key.Bytes allowlist rule in
// gorules/rules.go.
```

After:

```go
// Material or sibling keys minted from the same Material. Reads of
// the returned key's Bytes field outside the codec/crypto package
// trees fail lint via the codeckeybytesallowlist analyzer in
// gorules/analyzers/.
```

- [ ] **Step 3: Edit `api_test.go` (the docstring at lines 21-23)**

Before:

```go
// INV-27 — the ruleguard rules in gorules/ catch known sinks, but this
// test catches API drift (a future contributor adding a Bytes()
// accessor would bypass the ruleguards by introducing a new export).
```

After:

```go
// INV-27 — the dekmaterialno* and codeckeybytesallowlist analyzers in
// gorules/analyzers/ catch known sinks, but this test catches API drift
// (a future contributor adding a Bytes() accessor would bypass the
// analyzers by introducing a new export).
```

- [ ] **Step 4: Verify the package still compiles and the static API surface test still passes**

Run: `task test -- ./internal/eventbus/crypto/dek/...`
Expected: green. The static API surface test (TestPackageHasNoExportedByteSlices) is unaffected by comment edits.

- [ ] **Step 5: Commit**

```bash
jj --no-pager commit -m "docs(crypto/dek): update stale ruleguard references to new analyzers (holomush-46ya)

Three doc comments referenced the deleted ruleguard rules:
- material.go:9 referenced a nonexistent gorules/codec_key_bytes_allowlist.go
- material.go:41 referenced 'the codec.Key.Bytes allowlist rule in
  gorules/rules.go' (deleted in PR #3454)
- api_test.go:22 referenced 'the ruleguard rules in gorules/'

All three now point at the gorules/analyzers/ path with the
analyzer names that actually enforce the rules.

Per spec §4.5.6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

### Task 23: Final cold-cache `task pr-prep` verification

**Files:** none (verification only).

- [ ] **Step 1: Clean caches**

Run:

```bash
golangci-lint cache clean
rm -f bin/custom-gcl
```

This ensures the next lint run starts from a known-empty state — the specific failure shape PR #3454 hit was cache-dependent (per spec §5.3).

- [ ] **Step 2: Run the full pre-PR gate**

Run: `task pr-prep`
Expected: green. This builds custom-gcl from scratch, runs lint, format, schema, license, unit tests, integration tests, and E2E. Total: 5–15 minutes depending on machine.

- [ ] **Step 3: Verify all ten analyzers were actually registered**

Each analyzer subpackage's `init()` calls `register.Plugin(<name>, …)`, producing one linter ID per analyzer. Verify all ten appear in `./bin/custom-gcl linters`:

```bash
./bin/custom-gcl linters > /tmp/linters.txt
for name in ulidmakeforbidden cursorpackageinternal sceneopseventsappendonly \
            dekmaterialnojson dekmaterialnogob dekmaterialnoproto \
            dekmaterialnofmtformatting dekmaterialnolog dekmaterialnoslog \
            codeckeybytesallowlist; do
  if ! rg -F "$name" /tmp/linters.txt > /dev/null; then
    echo "MISSING: $name"
    exit 1
  fi
done
echo "all 10 registered"
```

Expected: `all 10 registered`. The exact display format of each linter line depends on the golangci-lint v2.11.4 `linters` subcommand output (verbatim format may vary), but each analyzer's name MUST appear somewhere in the output.

- [ ] **Step 4: Verify the pre-existing static API surface test still passes**

Run: `task test:int -- -run TestPackageHasNoExportedByteSlices ./internal/eventbus/crypto/dek/...`
Expected: green. This is the residual defense; the new lint analyzers complement, not replace, this test.

- [ ] **Step 5: No commit (verification-only); the next step is invoking code-reviewer + push.**

The reviewer-gate reminder built into the project's hooks will require running `code-reviewer` before push. That happens in the post-implementation checklist below.

---

## Post-implementation checklist

Per the project's adversarial-gate policy and `Landing the Plane` workflow in CLAUDE.md.

- [ ] **PI-1:** Confirm `task pr-prep` is green on a cold cache (Task 23).
- [ ] **PI-2:** Run `code-reviewer` adversarial gate on the branch tip (`/review-code` or `Agent` with `subagent_type: code-reviewer`). Address all blocking findings; cite each in commit messages or PR description.
- [ ] **PI-3:** Update `holomush-46ya` bead (`bd update holomush-46ya --notes ...`) with the final PR link once opened.
- [ ] **PI-4:** Push to remote (`jj git push -b 46ya` or your branch name; `jj bookmark set 46ya -r @-` first if needed) and open the PR.
- [ ] **PI-5:** PR review will run `pr-review-toolkit:review-pr` per project policy. Address all findings.
- [ ] **PI-6:** After merge, clean up the workspace per CLAUDE.md "Landing the Plane":

  ```bash
  cd <repo-root>
  jj workspace forget go-analysis
  rm -rf <repo-parent>/.worktrees/go-analysis
  ```

- [ ] **PI-7:** Close `holomush-46ya` (`bd close holomush-46ya`).
- [ ] **PI-8:** Sync beads DB: `bd dolt push` (no-op locally per project setup, but run the command for habit).

---

## Risk recap (from spec §8)

- **R1:** `golangci-lint custom` first-run network dependency — covered by CI's existing module-cache.
- **R2:** macOS/Linux only — documented; not mitigated.
- **R3:** Broader `cursorpackageinternal` semantics may surface false positives — mitigated by allowlist testdata; review on first run.
- **R4:** Two go.mod files in one repo — mitigated by explicit `task test:gorules` (Task 7).
- **R5:** `bin/custom-gcl` cache invalidation — `.custom-gcl.yml` is in the `sources` list (Task 7 Step 2).
