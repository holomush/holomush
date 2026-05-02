# go/analysis migration — design spec

- **Bead:** holomush-46ya
- **Status:** Design — pre-plan
- **Author:** Sean Brandt (with Claude Opus 4.7)
- **Date:** 2026-05-01
- **Related:**
  - holomush#1272 (original ruleguard self-reference bug, plugins/)
  - PR #3454 (the trigger; deleted INV-27 ruleguard rules, kept static API surface test)
  - `gorules/rules.go` lines 141–177 (in-tree explanation of the deleted rules)

---

## 1. Problem

`go-ruleguard`'s DSL rule loader fails silently when a rule references a Go type defined in the module being analysed (`Type.Is("…/internal/foo.Bar")`). The resolver returns an empty rule set **globally** for the affected golangci-lint run, surfacing as

```text
ruleguard: execution error: used Run() with an empty rule set; forgot to call Load() first?
```

on every package golangci-lint scans (200+ false-positive findings on a clean cache). PR #3454 hit this for the seven INV-27 rules that referenced `internal/eventbus/crypto/dek.Material`. The workaround was to delete the rules entirely; INV-27 enforcement now rests solely on the static API surface test in `internal/eventbus/crypto/dek/api_test.go`.

We are on the latest release of every relevant component: golangci-lint v2.11.4 (which vendors go-ruleguard v0.4.5 and go-critic v0.14.3), and `go-ruleguard/dsl` v0.3.23. No upstream version bump is available. The fix is to migrate off the DSL.

## 2. Goals

- **G1.** All ten project-internal lint rules (the three currently in `gorules/rules.go` plus the seven INV-27 rules deleted in PR #3454) run as standard `golang.org/x/tools/go/analysis` analyzers.
- **G2.** Lint runs are deterministic on a cold cache. The "empty rule set" failure mode is structurally absent.
- **G3.** Each analyzer has unit tests via `golang.org/x/tools/go/analysis/analysistest`, with explicit positive *and* negative cases plus allowlist boundary cases.
- **G4.** `task lint`, `task pr-prep`, lefthook, and CI all use the new toolchain. No raw `go test` / `golangci-lint` calls in user-facing flows.
- **G5.** INV-27 enforcement returns to lint-time *in addition to* the static API surface test (defense in depth).

## 3. Non-goals

- Catching SQL written by string concatenation routed through arbitrary helpers (full taint tracking). The migrated `SceneOpsEventsAppendOnly` rule extends to *named-constant* resolution but stops short of full data-flow analysis. The current rule only catches inline literals, so any improvement is net positive.
- Replacing `gocritic`. `gocritic` stays enabled for its diagnostic / style / performance checks; only its `ruleguard` sub-checker is removed.
- Replacing `forbidigo` (handles `time.Sleep` ban). Different mechanism, different concerns.
- Re-introducing the holomush#1272 plugins/ rules. Those targeted a since-removed WASM plugin design.

## 4. Architecture

### 4.1 Tooling: golangci-lint v2 module plugin

golangci-lint v2 supports two custom-linter loading modes:

| Mode | Mechanism | Verdict for this project |
|---|---|---|
| Module plugin | `golangci-lint custom` builds a `custom-gcl` binary that bundles the standard linter set + our analyzers. Requires `.custom-gcl.yml`. | **Chosen.** Reproducible across macOS / Linux. Toolchain-version-agnostic. |
| Go plugin (`.so`) | `go build -buildmode=plugin`. Loaded at runtime. | Rejected. macOS Homebrew installs of golangci-lint frequently mismatch the build toolchain, breaking `.so` loads. |
| Manual integration | Fork golangci-lint, blank-import the plugin. | Rejected. Operationally heaviest; warranted only if other modes fail. |

**Concrete artefacts:**

- `.custom-gcl.yml` at repo root pins `version: v2.11.4` and references the local `gorules/` module by relative path.
- `bin/custom-gcl` is the built binary. Gitignored. Cache-aware rebuild via Taskfile `sources`/`generates`.
- `task lint:go` becomes `bin/custom-gcl run` (with `lint:build-custom-gcl` as a dep).

### 4.2 Analyzer module layout

A new Go module rooted at `gorules/` with module path `github.com/holomush/holomush/gorules`. Two-go.mod-in-one-repo is supported by Go's module system (module boundaries are defined by `go.mod` location, not file presence). The parent module's package set automatically excludes `gorules/` once `gorules/go.mod` exists. The two modules do not depend on each other; the `gorules/` module is consumed only by the synthesized `custom-gcl` build via `path:` replacement in `.custom-gcl.yml`.

Layout:

```text
gorules/
  go.mod                                     # module github.com/holomush/holomush/gorules
  go.sum
  plugin.go                                  # implements register.Plugin; returns 10 analyzers
  analyzers/
    internal/holomushlint/                   # shared helpers (NOT exported; internal/ blocks downstream import)
      ast.go                                 # CallExpr matcher, fully-qualified-symbol resolver
      types.go                               # type-identity helpers (matches dek.Material, codec.Key, etc.)
      sqlmatch.go                            # const-folding string extraction for SceneOps SQL rule
    ulidmakeforbidden/
      ulidmakeforbidden.go
      ulidmakeforbidden_test.go
      testdata/src/...
    cursorpackageinternal/
      ...
    sceneopseventsappendonly/
      ...
    dekmaterialnojson/
      ...
    dekmaterialnogob/
      ...
    dekmaterialnoproto/
      ...
    dekmaterialnofmtformatting/
      ...
    dekmaterialnolog/
      ...
    dekmaterialnoslog/
      ...
    codeckeybytesallowlist/
      ...
```

Each analyzer subpackage exports a single `*analysis.Analyzer` named `Analyzer` (in `<name>.go`) and a `plugin.go` that registers the analyzer with golangci-lint via `register.Plugin(<name>, …)`. The aggregator `gorules/plugin.go` blank-imports each subpackage to trigger the registrations when the custom-gcl binary builds. See §4.3 for the contract details.

### 4.3 Module-plugin contract

**Critical constraint** (verified against golangci-lint v2.11.4 source and the upstream `golangci/example-plugin-module-linter`): each `register.Plugin(name, …)` call produces **exactly one** golangci-lint linter ID equal to `name`. golangci-lint wraps all analyzers returned by that plugin's `BuildAnalyzers()` into a single `goanalysis.NewLinter(name, settings.Description, analyzers, nil)` instance. **Per-analyzer enable/disable in `linters.enable` therefore requires per-analyzer `register.Plugin(...)` calls.** Bundling N analyzers under one plugin name means N `linters.enable` entries with that name don't exist; only the plugin name itself is enableable. The upstream contrast example (`github.com/albertocavalcante/go-analyzers-gcl`) uses `register.Plugin("makecopy", …)`, `register.Plugin("searchmigrate", …)`, etc. — one `register.Plugin` per analyzer — for exactly this reason.

We adopt the per-analyzer-plugin shape. **Layout:**

- Each analyzer subpackage owns its own `plugin.go` that calls `register.Plugin(<analyzer-name>, …)` from `init()` and returns its single analyzer from `BuildAnalyzers()`.
- A central aggregator package at `gorules/` (the module root, package name `holomushrules`) blank-imports all ten analyzer subpackages so that the `init()` registrations fire when the custom-gcl binary is built. The aggregator package itself does NOT call `register.Plugin`.
- The `.custom-gcl.yml` lists a single plugin entry referencing the aggregator package (`module: github.com/holomush/holomush/gorules`, `path: ./gorules`); the synthesized binary's `import _ "github.com/holomush/holomush/gorules"` transitively pulls all ten subpackages.

**Aggregator** (`gorules/plugin.go`):

```go
// Package holomushrules is the module-plugin aggregator. It does not
// itself register any plugins — each analyzer subpackage registers
// itself via init(). Blank imports below pull each subpackage so its
// init() fires when golangci-lint builds the custom-gcl binary
// (which adds `import _ "github.com/holomush/holomush/gorules"` at
// build time, per .custom-gcl.yml).
//
// The package name is unconstrained by the module-plugin API.
package holomushrules

import (
    _ "github.com/holomush/holomush/gorules/analyzers/codeckeybytesallowlist"
    _ "github.com/holomush/holomush/gorules/analyzers/cursorpackageinternal"
    _ "github.com/holomush/holomush/gorules/analyzers/dekmaterialnofmtformatting"
    _ "github.com/holomush/holomush/gorules/analyzers/dekmaterialnogob"
    _ "github.com/holomush/holomush/gorules/analyzers/dekmaterialnojson"
    _ "github.com/holomush/holomush/gorules/analyzers/dekmaterialnolog"
    _ "github.com/holomush/holomush/gorules/analyzers/dekmaterialnoproto"
    _ "github.com/holomush/holomush/gorules/analyzers/dekmaterialnoslog"
    _ "github.com/holomush/holomush/gorules/analyzers/sceneopseventsappendonly"
    _ "github.com/holomush/holomush/gorules/analyzers/ulidmakeforbidden"
)
```

**Per-analyzer plugin file** (one per subpackage; sketch for `ulidmakeforbidden`):

```go
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

Plugin load mode is **per-plugin, not per-analyzer** — `GetLoadMode()` is on `LinterPlugin`. With per-analyzer plugins, each analyzer effectively has its own load mode, but they all return `LoadModeTypesInfo` because every analyzer consults `pass.TypesInfo`: `ulidmakeforbidden` uses `TypesInfo.Uses` for function-identity resolution; `cursorpackageinternal` for cross-package object resolution; `sceneopseventsappendonly` for `TypesInfo.Types[expr].Value` const folding; the six `dekmaterialno*` analyzers and `codeckeybytesallowlist` for type-and-field identity. There are no syntax-only analyzers.

### 4.4 Analyzer specifications

Each rule below is given its analyzer name, target patterns, allowlist / scope, and the diagnostic message. Messages preserve the existing wording where possible.

#### 4.4.1 `ulidmakeforbidden`

- **Targets:** `*ast.CallExpr` whose `Fun` resolves (via `pass.TypesInfo.Uses`) to `(github.com/oklog/ulid/v2).Make`.
- **Scope:** production code only. Test files are explicitly excluded — see §4.5.2 below for the `.golangci.yaml` carveout. Today's ruleguard rule runs through gocritic, which `.golangci.yaml:53-60` excludes for `_test.go`; the new analyzer must preserve that de-facto scope to avoid firing on the existing 1300+ `ulid.Make()` callsites in test files.
- **Diagnostic:** `use idgen.New() for entity IDs or core.NewULID() for event IDs; ulid.Make() uses math/rand`.

#### 4.4.2 `cursorpackageinternal`

- **Targets:** any reference (call, type literal, or value identifier) to symbols in `github.com/holomush/holomush/internal/eventbus/cursor`. Specifically:
  - `Encode`, `Decode` (calls)
  - `Cursor`, `Owner`, `HostCursor` (composite literal types)
  - `CurrentVersion`, `CurrentEpoch`, `OwnerKind`, `OwnerHost`, `OwnerPlugin`, `OwnerUnspecified` (identifiers / call)
- **Allowlist (consulted via `pass.Pkg.Path()`):**
  - `github.com/holomush/holomush/internal/eventbus/...`
  - `github.com/holomush/holomush/internal/grpc/...`
  - `github.com/holomush/holomush/internal/web/...` — preserved verbatim from the existing ruleguard rule for forward compatibility; no `internal/web/` package currently imports `internal/eventbus/cursor`. Tightening (removal) is out of scope for this migration; tracked for a future cleanup.
  - `github.com/holomush/holomush/internal/plugin/goplugin/...`
  - `github.com/holomush/holomush/internal/plugin/hostfunc/...`
- **Diagnostic:** `internal/eventbus/cursor is host-internal — clients and plugins must not import it`.

The simplest implementation: walk all `*ast.SelectorExpr` and `*ast.CompositeLit` nodes; resolve via `pass.TypesInfo.Uses` / `pass.TypesInfo.Types`; flag if the resolved object's package is `internal/eventbus/cursor` AND the analysed package is not allowlisted. This is more robust than ruleguard's per-symbol enumeration — it covers any future symbol added to the cursor package.

#### 4.4.3 `sceneopseventsappendonly`

- **Targets:** `*ast.CallExpr` with `Sel.Name ∈ {Exec, Query, QueryRow}` (no receiver-type filter; pgx receivers are plural).
- **String extraction (per Q4 resolution):**
  1. `args[1]` is an `*ast.BasicLit` of kind STRING — `strconv.Unquote` it.
  2. `args[1]` is a flat `+`-chain of `*ast.BasicLit` strings — concat post-unquote.
  3. `args[1]` is an `*ast.Ident` resolving via `pass.TypesInfo.Types[ident].Value` to a `constant.Value` of `Kind() == constant.String` — use `constant.StringVal`.
  4. Anything else — give up silently (no diagnostic, no error).
- **Regex:** `(?i)(?:update\s+scene_ops_events|delete\s+from\s+scene_ops_events|truncate(?:\s+table)?\s+scene_ops_events)` (identical to current rule).
- **Allowlist:** none (repo-wide).
- **Diagnostic:** `scene_ops_events is append-only (Phase 3 design P3.D3/D4): use a new INSERT via recordOpsEventTx to record corrections instead of UPDATE/DELETE/TRUNCATE`.

The string-extraction logic lives in `holomushlint/sqlmatch.go` because future SQL-shape rules will need it.

#### 4.4.4–4.4.9 `dekmaterialno{json,gob,proto,fmtformatting,log,slog}`

Each is a "no-sink" rule: `*dek.Material` (value or pointer) MUST NOT flow into a forbidden function/method's argument list. The shared helper accepts:

- A target type predicate (`isDEKMaterial(t types.Type) bool`).
- A set of forbidden sinks, each described as `{pkgPath, funcOrMethodName, methodReceiverPkg?}`.

| Analyzer | Forbidden sinks |
|---|---|
| `dekmaterialnojson` | `encoding/json.Marshal`, `encoding/json.MarshalIndent`, `(*encoding/json.Encoder).Encode` |
| `dekmaterialnogob` | `(*encoding/gob.Encoder).Encode`, `encoding/gob.Register` |
| `dekmaterialnoproto` | `google.golang.org/protobuf/proto.Marshal`, `(google.golang.org/protobuf/proto.MarshalOptions).Marshal` |
| `dekmaterialnofmtformatting` | `fmt.Sprint`, `Sprintf`, `Sprintln`, `Print`, `Printf`, `Println`, `Fprint`, `Fprintf`, `Fprintln`, `Errorf` |
| `dekmaterialnolog` | `(*log.Logger).Print*`, `(*log.Logger).Fatal*`, `(*log.Logger).Panic*`, plus the package-level shortcuts `log.Print*` etc. |
| `dekmaterialnoslog` | `log/slog.Info`, `Debug`, `Warn`, `Error`, `InfoContext`, `DebugContext`, `WarnContext`, `ErrorContext`, `Log`, `LogAttrs`, `Any`, `Group`, `With`, plus the `*Logger` method equivalents (`(*slog.Logger).Info`, `.InfoContext`, `.LogAttrs`, `.With` etc.; `Any` and `Group` are package-level helpers, not methods). `With` is included because `slog.With(...any) *Logger` bakes its arguments into a returned logger's attributes, leaking them on every subsequent log call without `Material` appearing as a direct arg to the leaf sink — closing the bypass requires gating `With` itself. |

`proto.Marshal` cannot accept `*dek.Material` today (it does not implement `proto.Message`), so the rule's positive case does not type-check against current code. Its purpose is forward-defensive: if `Material` ever gains a `proto.Message` method set (e.g., via accidentally-added `Reset()`/`String()`/`ProtoMessage()` methods, or via a generated stub), `dekmaterialnoproto` blocks the new export at lint time before code review can miss it. Cost is one ~30-line analyzer; benefit is one structural backstop on a high-stakes invariant. Keep.

**Diagnostic format:** `INV-27: dek.Material MUST NOT be passed to <sink-description> — Material is opaque by design (see internal/eventbus/crypto/dek/material.go and bead holomush-46ya for context)`.

#### 4.4.10 `codeckeybytesallowlist`

- **Targets:** any `*ast.SelectorExpr` whose resolved field is the `Bytes []byte` field of `github.com/holomush/holomush/internal/eventbus/codec.Key`.
- **Receiver shape:** must match BOTH `k.Bytes` where `k` is `codec.Key` AND `k.Bytes` where `k` is `*codec.Key` (per CodeRabbit feedback on the Phase 2 PR). Implementation: resolve via `pass.TypesInfo.Selections[selExpr].Obj()` and compare to the field; the receiver-pointer-vs-value distinction is invisible at this layer (Go's selector resolution treats both uniformly).
- **Allowlist (consulted via `pass.Pkg.Path()`):**
  - `github.com/holomush/holomush/internal/eventbus/codec/...`
  - `github.com/holomush/holomush/internal/eventbus/crypto/...`
- **Diagnostic:** `INV-27 (residual defense): codec.Key.Bytes reads are restricted to internal/eventbus/codec/... and internal/eventbus/crypto/... — Material exposure goes through dek.Material.AsCodecKey only`.

### 4.5 Configuration changes

#### 4.5.1 `.custom-gcl.yml` (new file at repo root)

```yaml
version: v2.11.4
name: custom-gcl
destination: ./bin/
plugins:
  - module: github.com/holomush/holomush/gorules
    path: ./gorules
```

#### 4.5.2 `.golangci.yaml` (modified)

Drop the `linters.settings.gocritic.settings.ruleguard` block. Keep `gocritic` enabled. Add:

```yaml
linters:
  enable:
    # ... existing ...
    - ulidmakeforbidden
    - cursorpackageinternal
    - sceneopseventsappendonly
    - dekmaterialnojson
    - dekmaterialnogob
    - dekmaterialnoproto
    - dekmaterialnofmtformatting
    - dekmaterialnolog
    - dekmaterialnoslog
    - codeckeybytesallowlist
  settings:
    custom:
      ulidmakeforbidden:
        type: module
        description: forbids ulid.Make() (uses math/rand instead of crypto/rand)
        original-url: github.com/holomush/holomush
      cursorpackageinternal:
        type: module
        description: forbids references to internal/eventbus/cursor outside host packages
        original-url: github.com/holomush/holomush
      sceneopseventsappendonly:
        type: module
        description: forbids UPDATE/DELETE/TRUNCATE against scene_ops_events
        original-url: github.com/holomush/holomush
      dekmaterialnojson:
        type: module
        description: "INV-27: dek.Material MUST NOT be passed to encoding/json"
        original-url: github.com/holomush/holomush
      dekmaterialnogob:
        type: module
        description: "INV-27: dek.Material MUST NOT be passed to encoding/gob"
        original-url: github.com/holomush/holomush
      dekmaterialnoproto:
        type: module
        description: "INV-27: dek.Material MUST NOT be passed to google.golang.org/protobuf/proto"
        original-url: github.com/holomush/holomush
      dekmaterialnofmtformatting:
        type: module
        description: "INV-27: dek.Material MUST NOT be passed to fmt formatting"
        original-url: github.com/holomush/holomush
      dekmaterialnolog:
        type: module
        description: "INV-27: dek.Material MUST NOT be passed to log"
        original-url: github.com/holomush/holomush
      dekmaterialnoslog:
        type: module
        description: "INV-27: dek.Material MUST NOT be passed to log/slog"
        original-url: github.com/holomush/holomush
      codeckeybytesallowlist:
        type: module
        description: "INV-27 residual defense: codec.Key.Bytes reads are restricted"
        original-url: github.com/holomush/holomush
```

The ten `linters.settings.custom.<name>` entries each correspond to one `register.Plugin(<name>, …)` call in the analyzer subpackage's `plugin.go` (per §4.3).

Plus a new entry under the existing `linters.exclusions.rules` block (`.golangci.yaml:53-60`-area) to preserve the existing test-file scope of `ULIDMakeForbidden`. golangci-lint v2 uses `linters.exclusions.rules` (the v1 `issues.exclude-rules` key was renamed in the v2 migration); the repo is on v2 (`.golangci.yaml:1` reads `version: "2"`):

```yaml
linters:
  exclusions:
    rules:
      # ... existing _test.go entries for gocritic / wrapcheck / errcheck ...
      # ulidmakeforbidden replaces a ruleguard rule that only fired in
      # production because gocritic was excluded for _test.go. Tests
      # legitimately use ulid.Make() for fixture generation; preserve
      # that scope.
      - path: '_test\.go'
        linters:
          - ulidmakeforbidden
```

(Exact placement within `linters.exclusions.rules` is a planning detail; the rule itself is required.)

The bead claimed an `internal/eventbus/crypto/(aad|dek|kek)/` exclusion was added in PR #3454 as a workaround. That is incorrect — the PR diff does not touch `.golangci.yaml`. The actual workaround was deleting the rules from `gorules/rules.go`. No phantom `.golangci.yaml` cleanup is needed.

#### 4.5.3 `Taskfile.yaml` (modified)

```yaml
lint:go:
  desc: Lint Go code (uses custom-gcl with project analyzers)
  deps: [lint:build-custom-gcl]
  cmds:
    - ./bin/custom-gcl run

lint:build-custom-gcl:
  desc: Build the custom-gcl binary if missing or stale
  # NOTE: .golangci.yaml is intentionally excluded from sources — the
  # custom-gcl binary embeds the analyzer code at build time but reads
  # .golangci.yaml at run time, so config-only changes do not require
  # a rebuild.
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
  desc: Run analysistest unit tests for the gorules module
  dir: gorules
  cmds:
    - go test ./...
```

`task test` gains a dependency on `test:gorules` (or the gorules tests are appended to its command list — TBD during planning, but the analyzer tests MUST run as part of `task test`).

#### 4.5.4 `lefthook.yaml` (modified)

```yaml
# Before:
run: golangci-lint run --new-from-rev=HEAD~1
# After:
run: ./bin/custom-gcl run --new-from-rev=HEAD~1
```

The hook MUST not silently fall back to standard `golangci-lint` if `bin/custom-gcl` is missing — that would mask a missing custom binary on a developer's machine and produce different lint coverage than CI. Instead the hook fails fast; the developer runs `task lint:build-custom-gcl` (or `task lint`, which builds it transitively).

#### 4.5.5 `.github/workflows/ci.yaml` (modified)

The CI `Lint` job today calls `task lint` (`.github/workflows/ci.yaml:87-88`), which fans out to `task lint:go`. Once Taskfile is updated per §4.5.3, that path becomes correct automatically. The only direct change to `ci.yaml` is the version pin:

- `.github/workflows/ci.yaml:39-40` — bump the curl URL's version from `v2.11.3` to `v2.11.4` to match `.custom-gcl.yml`.

The standard `golangci-lint` binary is still required at install time because `golangci-lint custom` is the entry point that builds `custom-gcl`. No CI workflow restructuring is needed.

#### 4.5.6 Source files referencing the old rules

Three doc comments need verbatim updates. The replacement text is specified to avoid a third round of phrasing drift across PRs.

**`internal/eventbus/crypto/dek/material.go:8-9`** — currently references a file `gorules/codec_key_bytes_allowlist.go` that never existed:

```go
// Old:
// The codec.Key.Bytes
// field is the residual leakage path; reads are gated by the ruleguard
// rule at gorules/codec_key_bytes_allowlist.go.

// New:
// The codec.Key.Bytes
// field is the residual leakage path; reads are gated by the
// codeckeybytesallowlist analyzer in gorules/analyzers/.
```

**`internal/eventbus/crypto/dek/material.go:41-42`** — currently references "the codec.Key.Bytes allowlist rule in gorules/rules.go" (the rule was deleted in PR #3454):

```go
// Old:
// Reads of
// the returned key's Bytes field outside the codec/crypto package
// trees fail lint via the codec.Key.Bytes allowlist rule in
// gorules/rules.go.

// New:
// Reads of
// the returned key's Bytes field outside the codec/crypto package
// trees fail lint via the codeckeybytesallowlist analyzer in
// gorules/analyzers/.
```

**`internal/eventbus/crypto/dek/api_test.go:21-23`** — currently references "the ruleguard rules in gorules/" and "bypass the ruleguards":

```go
// Old:
// INV-27 — the ruleguard rules in gorules/ catch known sinks, but this
// test catches API drift (a future contributor adding a Bytes()
// accessor would bypass the ruleguards by introducing a new export).

// New:
// INV-27 — the dekmaterialno* and codeckeybytesallowlist analyzers in
// gorules/analyzers/ catch known sinks, but this test catches API drift
// (a future contributor adding a Bytes() accessor would bypass the
// analyzers by introducing a new export).
```

#### 4.5.7 Cleanup at the end of the migration

- Delete `gorules/rules.go` (the build-tagged ruleguard DSL file).
- Delete `gorules/testdata/dek_no_serialize/` and `gorules/testdata/codec_key_bytes/` (their `expected_violations.go` files; analyzer testdata is the canonical replacement).
- Delete `gorules/testdata/` if empty after the above.
- Remove `github.com/quasilyte/go-ruleguard/dsl v0.3.23` from the parent `go.mod` (`gorules/rules.go` was its only importer).

## 5. Test strategy

### 5.1 Per-analyzer `analysistest` tests

Each analyzer has a sibling `_test.go` invoking `analysistest.Run(t, analysistest.TestData(), Analyzer, "<fake-import-path>...")`. Testdata layout per analyzer:

```text
<analyzer-pkg>/testdata/src/
  <stub-import-paths>/   # e.g., github.com/holomush/holomush/internal/eventbus/crypto/dek/
    <minimal-stub>.go    # exports the surface the rule consults
  example.com/positive/  # patterns that MUST flag, marked with `// want "regex"`
  example.com/negative/  # near-miss patterns that MUST NOT flag, no `// want`
  example.com/allowlist/ # only for rules with allowlists; lives under a fake import path that triggers the allowlist
```

For the `cursorpackageinternal` and `codeckeybytesallowlist` rules, the allowlist test fixtures live under stub paths that MIRROR the real allowlist (`internal/eventbus/...` etc.) so the `pass.Pkg.Path()` check resolves identically to production.

Each analyzer's testdata MUST cover, at minimum:

- 1 positive case per forbidden pattern (no shortcuts — every `fmt.*` variant for `dekmaterialnofmtformatting`, all three SQL verbs and both whitespace shapes for `sceneopseventsappendonly`, etc.)
- ≥1 negative near-miss (e.g., `fmt.Sprintf("%v", someOtherStruct)` for the DEK rules; `tx.Exec(ctx, "INSERT INTO scene_ops_events ...")` for SceneOps)
- For allowlist rules: ≥1 case that triggers from a non-allowlisted path AND ≥1 case that does NOT trigger from an allowlisted path

`SceneOpsEventsAppendOnly` testdata also covers the three string-extraction shapes: literal, `+`-chain, named const.

`codeckeybytesallowlist` testdata MUST cover the read-vs-write distinction explicitly, because the rule targets reads only:

- **Positive (must flag):**
  - Read via value receiver: `var k codec.Key; _ = k.Bytes`
  - Read via pointer receiver (explicit deref): `var pk *codec.Key; _ = (*pk).Bytes`
  - Read via pointer receiver (auto-deref): `var pk *codec.Key; _ = pk.Bytes`
  - Read via index expression: `_ = k.Bytes[0]`
  - Read via slicing: `_ = k.Bytes[:n]`
- **Negative (must NOT flag, even from a non-allowlisted package):**
  - Composite-literal construction: `_ = codec.Key{ID: id, Bytes: out}` (this is a write, the field name is an `*ast.Ident` not a `*ast.SelectorExpr`).
  - Field assignment: `k.Bytes = x` (write, not read).
  - The composite-literal in `internal/eventbus/crypto/dek/material.go:46` (`codec.Key{ID: id, Bytes: out}`) MUST type-check cleanly against the migrated rule; an explicit testdata case mirrors that pattern.
- **Allowlist boundary:**
  - Read from `internal/eventbus/codec/...` testdata stub package: must NOT flag.
  - Read from `internal/eventbus/crypto/...` testdata stub package: must NOT flag.
  - Read from `example.com/external` testdata stub package: must flag.

### 5.2 Smoke test against the real codebase

A lightweight integration test that runs `bin/custom-gcl run -- ./...` on the full repo and asserts zero diagnostics — or, equivalently in incremental mode, `bin/custom-gcl run --new-from-rev=origin/main -- ./...` (note: golangci-lint passes this revspec to `git`, so use `origin/main`, not jj's `main@origin`). This catches the "passes analysistest, fires false positives in production" failure mode. Lives as a CI step (or a Taskfile target) rather than a Go test, because it requires the built `custom-gcl` binary.

Concretely: `task pr-prep` already runs `task lint`; once Taskfile is updated, `task pr-prep` is the smoke test and no separate target is needed.

### 5.3 Cold-cache verification

`task pr-prep` MUST be run on a cold golangci-lint cache (`golangci-lint cache clean` first) at least once during plan execution to verify the empty-rule-set bug is structurally absent. This is the specific failure shape PR #3454 hit; absence on a warm cache is not evidence.

## 6. Rollout

A single PR. The change is structurally atomic in two senses:

- **Linter-coverage atomicity.** The old `gorules/rules.go` cannot coexist with the new analyzer module without conflicting coverage: ruleguard would flag `ULIDMakeForbidden` *and* the new analyzer would flag the same call → duplicate diagnostics.
- **Module-boundary atomicity.** Adding `gorules/go.mod` carves `gorules/` out of the parent module. From that point, `gorules/rules.go` belongs to the new module — but it imports `github.com/quasilyte/go-ruleguard/dsl`, which the new module's `go.mod` will not include (and should not, since the DSL is being removed). The `//go:build ruleguard` tag prevents normal `go build` failures, but `go mod tidy` (and tooling that respects `-compat`) scans tagged files and will either fail or re-add the DSL dep. Therefore `gorules/rules.go` (and its testdata fixtures) MUST be deleted in or before the same commit that creates `gorules/go.mod`.

Two viable execution shapes — the writing-plans phase will pick one:

- **Shape A (atomic single commit).** One commit that simultaneously: deletes `gorules/rules.go` + old testdata, creates `gorules/go.mod` and the analyzer module, swaps `.golangci.yaml`, updates Taskfile / lefthook / CI / source comments, and removes `go-ruleguard/dsl` from the parent `go.mod`. Largest single diff but simplest correctness story (tree is green at every commit boundary).
- **Shape B (staged with temporary path).** Scaffold the new analyzer module at a temp path (e.g., `gorules-new/`) so it coexists with `gorules/rules.go` for one commit; final commit renames `gorules-new/` → `gorules/` and performs the deletions and config swaps in one step. Smaller per-commit diffs, but reviewer must follow a rename.

Shape A is the recommended default. Shape B exists only if review feedback during the plan phase requests smaller diffs.

In either shape, the final commit MUST leave the tree such that `task pr-prep` is green on a cold `golangci-lint cache clean`. The `code-reviewer` adversarial gate runs on the final commit before push.

Per project policy:

- `task pr-prep` MUST be green pre-push, on a **cold** golangci-lint cache.
- The `design-reviewer`, `plan-reviewer`, and `code-reviewer` adversarial sub-agents MUST gate this work.
- All work is tracked under bead `holomush-46ya`. Child beads for execution will be created during the writing-plans phase.

## 7. Open questions

None blocking. Items to settle during writing-plans:

- Exact phrasing of analyzer names in `linters.enable` (lowercase, no underscores — golangci-lint convention; validated during planning against current entries).
- Whether `holomushlint` helpers need their own `_test.go` (probably yes for the SQL string-extractor; the AST helpers get covered transitively by analyzer tests).
- The `task test` integration: dep on `test:gorules` vs. inline `cd gorules && go test ./...` in the existing `test` cmd list.

## 8. Risks

- **R1.** `golangci-lint custom` requires network access at first build (Go module proxy). Mitigation: vendor `gorules/`'s deps if needed; document the network requirement; rely on Go module cache for repeat builds. CI already has network in the lint job.
- **R2.** `analysistest` has historically been finicky about testdata paths on Windows. We are not a Windows project, and the entire toolchain (lefthook, Taskfile, integration tests) already assumes macOS/Linux. Document, do not mitigate.
- **R3.** The `cursorpackageinternal` analyzer's "any reference to a symbol in package X" pattern is broader than the current ruleguard rule's per-symbol enumeration. This might catch *more* than the original. Mitigation: testdata explicitly enumerates each gated symbol class; if the broader semantics flags something legitimate, we narrow during code review.
- **R4.** Two go.mod files in one repo can confuse tooling (IDE indexers, `go work`, CI test scripts). The project does not use `go work` today. Add `gorules/` to existing test/lint loops explicitly; do not introduce a workspace file.
- **R5.** `bin/custom-gcl` build cache invalidation: Taskfile's `sources`/`generates` mechanism is checksum-based and reliable, but a `.custom-gcl.yml` `version` bump that isn't reflected in the source list will silently use a stale binary. Mitigation: include `.custom-gcl.yml` in the `sources` list (already specified in §4.5.3).
