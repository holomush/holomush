---
phase: 03-platform-hardening-deployment-scaling
plan: 01
subsystem: eventbus-config
tags: [config, koanf, fail-closed, validation, nats-external]
status: complete
requires: []
provides:
  - "eventbus.ModeExternal / Config.URL / Config.Credentials (external-mode vocabulary)"
  - "eventbus.TLSConfig{CA,Cert,Key}, Config.Provision *bool + IsProvision(), DLQConfig{MaxAge,MaxBytes}"
  - "eventbus.Config.Validate() (fail-closed) + EVENTBUS_CONFIG_INVALID oops code"
affects:
  - "cmd/holomush/core.go (event_bus config-load site now Defaults()+Validate())"
tech-stack:
  added: []
  patterns:
    - "CryptoConfig *bool + IsEnabled() pattern extended to Provision *bool + IsProvision() (explicit-false survives Defaults)"
    - "coreConfig.Validate() boot-refuse pattern mirrored for eventBusConfig.Validate()"
key-files:
  created: []
  modified:
    - internal/eventbus/config.go
    - internal/eventbus/config_test.go
    - internal/eventbus/subsystem_test.go
    - cmd/holomush/core.go
decisions:
  - "Provision represented as *bool (not plain bool) so provision:false survives Defaults() — see Deviations"
metrics:
  duration: "~35m"
  completed: "2026-07-10"
  tasks: 2
  files: 4
---

# Phase 3 Plan 01: External-mode config vocabulary + fail-closed validation Summary

Reconciled the pre-existing unimplemented `cluster`/`cluster_url`/`credentials_file`
event_bus config vocabulary to D-01's `external`/`url`/`credentials`, added the
TLS/provision/DLQ config surface, and introduced a pure `Config.Validate()` that
fails closed (external mode + empty URL → `EVENTBUS_CONFIG_INVALID`) wired into
core boot. Embedded remains the zero-config default.

## What shipped

- **Vocabulary reconciliation (D-01):** `ModeCluster`→`ModeExternal` (`"external"`),
  `ClusterURL koanf:"cluster_url"`→`URL koanf:"url"`, `CredentialsFile
  koanf:"credentials_file"`→`Credentials koanf:"credentials"`. Zero residual
  `ModeCluster|ClusterURL|CredentialsFile` in `internal/`+`cmd/`.
- **New config surface:** `TLSConfig{CA,Cert,Key}` on `Config.TLS koanf:"tls"` (D-04);
  `Config.Provision *bool koanf:"provision"` + `IsProvision()` (D-03);
  `DLQConfig{MaxAge,MaxBytes}` on `Config.DLQ koanf:"dlq"` (D-12).
- **Defaults():** `DLQ.MaxAge` defaults to ~30d (`defaultDLQMaxAge`); embedded stays
  the unset-Mode default; `Provision`/`Crypto.Enabled` left untouched (pointer
  disambiguation). Defaults() remains pure (no I/O).
- **Validate() (D-01/D-02):** returns `oops.Code("EVENTBUS_CONFIG_INVALID").With("mode",…)`
  when `Mode==ModeExternal && URL==""`, nil otherwise. Pure — no dial/filesystem.
- **Boot wiring:** `cmd/holomush/core.go` applies `eventBusConfig.Defaults()` then
  `Validate()` immediately after the `event_bus` `config.Load`, returning the error
  (boot refuse). `productionSubsystems` arity untouched (still 15 params, RESEARCH OQ-6).

## Tests

`internal/eventbus/config_test.go` (unit): Defaults() unset→embedded, explicit-external
survives, Provision default-true + explicit-false-survives, DLQ.MaxAge fill + preserve,
full external-mode koanf decode (via `providers/file` + `UnmarshalWithConf{Tag:"koanf"}`,
matching production `config.Load`), and Validate() fail-closed / external+URL / embedded
paths (`errutil.AssertErrorCode`). `task test -- ./internal/eventbus/` green (196 tests);
`task build`, `task lint`, `task fmt` all green.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - critical correctness] `Provision` implemented as `*bool` (+ `IsProvision()`), not plain `bool`**
- **Found during:** Task 1.
- **Issue:** The plan action text specified `Provision bool` with "default true via
  Defaults()". A plain `bool` cannot distinguish "unset" from an explicit
  `provision: false`; a Defaults() zero-value guard (`if !c.Provision { c.Provision = true }`)
  would silently clobber an operator's explicit `provision: false`, defeating D-03's
  locked opt-out seam. It would also force downstream Plan 03 to change the exported
  field type when it wires the opt-out — the exact drift this foundation plan exists
  to prevent.
- **Fix:** Mirrored the repo's established `CryptoConfig.Enabled *bool` + `IsEnabled()`
  pattern (explicitly cited in the plan's read_first): `Provision *bool koanf:"provision"`
  left untouched by Defaults(), with `Config.IsProvision()` applying default-true lazily.
  Acceptance criterion "`Config{}.Defaults()` yields Provision=true" is satisfied via
  `IsProvision() == true`; a new test locks that `provision: false` survives Defaults().
- **Files modified:** internal/eventbus/config.go, internal/eventbus/config_test.go
- **Commit:** 519b91fd2

Note: the plan places `Validate()` in Task 2, but the method body landed in the Task 1
config.go edit (config.go was authored as a coherent whole); Task 2 added its tests and
the core.go boot wiring. Net commits/behavior are identical.

## TDD Gate Compliance

This is a `type: tdd` plan. Task 1 is primarily a symbol **rename** plus additive
struct fields — a test-only RED commit is not achievable as a compiling tree (renamed
symbols cannot coexist with the old ones, and additive struct-field existence cannot be
driven by a compiling-but-failing test). RED/GREEN were exercised locally per task
(wrote the test, observed failure, implemented, observed pass) but committed atomically
per task to keep every commit bisect-clean (repo aversion to broken intermediate commits
on the shared milestone branch). No separate `test(...)` commit precedes the `feat(...)`
commits; this is an intentional, documented deviation from the strict RED-commit gate.

## Commits

- 519b91fd2 `feat(03-01): reconcile eventbus mode vocabulary + add TLS/provision/DLQ config surface`
- c815e6afd `feat(03-01): wire fail-closed Config.Validate() into core boot`

## Self-Check: PASSED

- Files exist: internal/eventbus/config.go, internal/eventbus/config_test.go, cmd/holomush/core.go — all FOUND.
- Commits exist: 519b91fd2, c815e6afd — both FOUND.
- Residual rename check: `rg "ModeCluster|ClusterURL|CredentialsFile" internal/ cmd/` — CLEAN.
- No stubs introduced (config-surface + validation only).
