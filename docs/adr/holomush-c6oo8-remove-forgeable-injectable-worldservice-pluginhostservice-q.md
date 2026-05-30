<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-c6oo8; do not edit manually; use `/adr update holomush-c6oo8` -->

# Remove forgeable injectable WorldService; PluginHostService Query is the sole binary world-read path

**Date:** 2026-05-30
**Status:** Accepted
**Decision:** holomush-c6oo8
**Deciders:** Sean Brandt

## Context

Binary plugins could reach `WorldService` via the plugin service registry (`requires: holomush.world.v1.WorldService`), calling the gRPC server with a caller-supplied subject. This is structurally equivalent to the EmitEvent actor-forgery surface closed by holomush-ec22.1. The path was latent — `core-scenes` declared the `requires` but had zero `worldv1` client usage — but its existence in the registry constitutes an open privilege-escalation surface. The new `PluginHostService` Query* RPCs (see the companion decision) make this injection unnecessary.

## Decision

The `WorldService` gRPC server (`internal/world/grpc_server.go`), its registry registration (`internal/plugin/setup/subsystem.go`), and the in-process conn (`internal/plugin/setup/world_conn.go`) are deleted. `PluginHostService` Query* RPCs become the sole binary-plugin world-read surface, parallel to the Lua hostfunc surface. The core server is unaffected — it reads `world.Service` directly in-process, never through the gRPC server. The `WorldService` proto is retained only if a non-plugin consumer mounts the handler; the implementation plan verifies this before deleting the proto service.

## Rationale

- A single constrained path per runtime (PluginHostService for binary, hostfunc for Lua) matches the plugin-runtime-symmetry invariant; two paths for binary would create a privilege gradient.
- INV-3 (`WorldService` MUST NOT be resolvable from the plugin service registry) is verifiable by a structural meta-test — a policy-only approach cannot achieve this guarantee.
- The forgery surface lives in `grpc_server.go`'s `access.CharacterSubject(req.GetSubjectId())` reads; those lines are removed by deleting the file, not by patching it.
- The new PluginHostService path covers the same functional use cases; no capability is lost, only the unsafe delivery mechanism.
- `core-scenes` (the only binary plugin) has zero actual `worldv1` client usage — the `requires` entry is vestigial, so no production behavior changes.

## Alternatives Considered

### Delete the registry injection; PluginHostService Query* is the sole path (chosen)

- **Strengths:** Eliminates the forgery surface structurally — no binary plugin can obtain a `WorldService` client with an arbitrary-subject interface; enforces the single-constrained-path invariant matching the Lua runtime; the `WorldService` gRPC server becomes dead code and is removed, reducing attack surface.
- **Weaknesses:** Requires reworking integration-test fixtures that used the injection as the canonical "binary plugin requires a host service" example; the "missing required service" DAG test must be re-vehicled onto a synthetic absent service.

### Keep the registry injection; add an interceptor that stamps the host-derived subject

- **Strengths:** No test rework; keeps the plugin DAG `requires`/`provides` mechanism exercised for `WorldService`.
- **Weaknesses:** Two subject-derivation paths exist for binary plugins (interceptor vs token lookup) — runtime-divergence risk; does not match the Lua surface shape (Lua has no WorldService gRPC client path); does not achieve the single-constrained-path invariant; leaves a registry entry future plugins could `requires` and misuse.

### Keep the registry injection as an opt-in advanced surface

- **Strengths:** Maximum flexibility for future binary plugins that need arbitrary-subject world queries.
- **Weaknesses:** Perpetuates the forgery surface; arbitrary-subject queries warrant their own higher-risk design; Lua plugins have no equivalent surface, violating plugin-runtime-symmetry.

## Consequences

**Positive:**

- The only binary-accessible path that accepted a caller-supplied subject is removed; INV-3 is structurally enforceable.
- Binary and Lua runtimes have parallel, equally-constrained world-read surfaces.
- Dead code removed (`grpc_server.go`, `grpc_server_test.go`, `world_conn.go`, `NewGRPCServer`).
- The plugin DAG is simplified: no plugin `requires` `WorldService`.

**Negative:**

- Integration-test fixtures coupled to the injectable `WorldService` must be reworked; the "missing required service" DAG test must be re-vehicled onto a synthetic absent service.
- Tutorial documentation (`binary-plugins.md`) must be updated to replace the `requires: holomush.world.v1.WorldService` canonical example.

**Neutral:**

- The `WorldService` proto and generated bindings may be retained if non-plugin consumers mount the handler; the plan gates on a usage audit.
- The Lua bridge's `hostfunc.WithWorldService` is independent of the registry injection and is untouched.
