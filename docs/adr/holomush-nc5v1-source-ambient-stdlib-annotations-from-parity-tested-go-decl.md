<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-nc5v1; do not edit manually; use `/adr update holomush-nc5v1` -->

# Source ambient stdlib annotations from a parity-tested Go decl table

**Date:** 2026-06-16
**Status:** Accepted
**Decision:** holomush-nc5v1
**Deciders:** Sean Brandt

## Context

The ambient holomush.* and holo.* hostfuncs are registered imperatively via SetField calls scattered across functions.go and stdlib.go — there is no structured metadata (no proto descriptors, no reflection API) to introspect their parameter / return types. The Lua stub generator (holomush-eykuh.9) must source annotations for the ambient surface from somewhere, and that source becomes the going-forward contract for how the ambient surface is documented.

## Decision

The ambient decl table (internal/plugin/luabridge/gen/ambient.go) is the single source of truth for ambient annotations. Its (Module, Name) set is held exactly equal to the live runtime registrations by a CI parity test that invokes the production hostfunc.Register entrypoint on a real LState, walks the resulting holomush/holo tables, and asserts set equality.

## Rationale

- No structured metadata exists for the ambient surface — descriptor introspection is impossible by construction.
- A hand-authored .lua fragment provides no drift safety net; the parity test closes the gap on the name-set dimension.
- Invoking the production entrypoint (not a duplicated name list) means the test exercises real registration code, giving genuine anti-drift value (spec §5.2).
- The parity test deliberately excludes RegisterSessionFuncs so the truth set is not polluted with out-of-scope capability-gated functions (holo.session.*).

## Alternatives Considered

- **Go decl table + live-runtime parity test (chosen):** machine-checkable name-set parity in CI; type annotations colocated with the generator in Go and reviewable alongside implementation changes.
- **Hand-authored .lua fragment, no parity test (rejected):** no automated drift detection — a removed hostfunc lingers in the stub, a new one stays invisible until someone hand-edits.
- **Descriptor introspection (rejected):** structurally impossible — the ambient surface has no proto descriptors to introspect.

## Consequences

- Positive: adding/removing an ambient hostfunc without updating the decl table fails CI; type annotations live in Go, colocated with the generator.
- Negative: param/return TYPE accuracy is not machine-checked — only the name set is parity-tested (mitigated by a required per-function signature-verification step in the plan); the gen test suite couples to internal/plugin/hostfunc.
- Neutral: the decl table covers four dotted modules (holomush, holomush.config, holo.fmt, holo.emit); each entry carries Module, Name, Doc, Params, Returns.
