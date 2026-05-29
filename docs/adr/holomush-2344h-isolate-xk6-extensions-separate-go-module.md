<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->

# Isolate xk6 extensions in a separate Go module

**Date:** 2026-05-28
**Status:** Accepted
**Decision:** holomush-2344h
**Deciders:** Sean Brandt

## Context

The load harness (ADR holomush-evggu) adds k6 `xk6` extension code in Go — a raw-TCP telnet driver and (possibly) a forked `xk6-connectrpc`. These depend on k6's internal packages and the xk6 extension API, which carry a large, fast-moving transitive dependency tree. The root module's `go test ./...`, `task lint`, and `go mod tidy` walk every package reachable from the root `go.mod`. Folding the xk6 code into the root module would pull k6's dependency tree into every root-module tool and risk version conflicts with production dependencies.

## Decision

The xk6 Go extensions live in a **separate Go module** at `test/load/go.mod` (module path `github.com/holomush/holomush/test/load`), isolated from the root `go.mod` by Go's module-boundary semantics. Their unit tests run via `task loadtest:test` (which runs the suite under `test/load`), never via the root `./...`.

## Rationale

- Go nested modules are excluded from a parent module's `./...` scans by the toolchain — the isolation is structural and automatic, not convention-enforced or linter-policed.
- k6's dependency tree is large and churns quickly; keeping it out of the root module prevents conflicts with production dependencies and keeps `task lint` and the root test suite fast.
- The `test/load` module has no production consumers; sharing type definitions across the boundary would be a design smell, so the lost reuse costs nothing real.

## Alternatives Considered

- **Fold xk6 extensions into the root `go.mod`** — single module, shared types, one `go.sum`, no cross-module `replace` directives. Rejected: k6's transitive dependency tree enters the root module, polluting `task lint`, the root test suite, and `go mod tidy` for every contributor, and risking dependency conflicts with production code.
- **Separate Go module at `test/load/go.mod`** — root tooling never sees k6 deps; the boundary is enforced by the Go toolchain; `task loadtest:test` is the single explicit entry point. Cost: `replace` directives for in-tree extensions and a second `go.sum`. Chosen.

## Consequences

**Positive:** the root test suite, `task lint`, and `go mod tidy` are unaffected by k6 dependency churn; the module boundary is enforced by the Go toolchain with no linter rule or CI check needed.

**Negative:** in-tree xk6 extensions need `replace` directives in `test/load/go.mod`; contributors must run the extension tests from the `test/load` module (via `task loadtest:test`) rather than relying on the root module; two `go.sum` files to maintain.

**Neutral:** consistent with the conventional Go pattern of isolating tool/codegen modules (e.g., a `tools/go.mod`).

## References

- Plan: docs/superpowers/plans/2026-05-28-load-perf-testing-harness.md (File Structure; Task 2)
- Design bead: holomush-ql7ef · Tooling ADR: holomush-evggu
