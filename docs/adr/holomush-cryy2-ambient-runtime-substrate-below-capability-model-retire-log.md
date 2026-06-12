<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-cryy2; do not edit manually; use `/adr update holomush-cryy2` -->

# Ambient runtime substrate below the capability model; retire the Log RPC

**Date:** 2026-06-12
**Status:** Accepted
**Decision:** holomush-cryy2
**Deciders:** Sean Brandt

## Context

The god-service includes a Log RPC, and the Lua runtime unconditionally injects log, new_request_id, holo.* stdlib, and holomush.config accessors. The question: are these capabilities (declared, least-privilege-gated) or runtime substrate (always available, ungated)? The answer decides whether logging requires a manifest declaration and whether the Log RPC is rehomed or deleted.

## Decision

log, new_request_id, holo.* stdlib, and holomush.config are ambient runtime substrate: always injected, never declared, never present in holomush.plugin.host.v1. The Log RPC is deleted, not rehomed. The deciding test for the ambient/capability boundary is: does the call cross the host trust boundary to touch something the plugin does not already own?

## Rationale

- log writes to the host's own logger (write-only, no state return, no authority) and fails the boundary test; gating observability is anti-host (the host wants maximal visibility, especially into misbehaving plugins).
- new_request_id is a pure utility; stdlib/config are in-process helpers with no cross-boundary reach.
- kv is self-namespaced yet IS a declared capability, because the test is brokered host authority (persistent storage beyond the VM), not self-scope.
- Binary plugins already get framework-native stderr capture from go-plugin; the Log RPC was declared-but-unserved (holomush-l6std), confirming it was never the real path.

## Alternatives Considered

**Model log as a capability (rehome Log into a LogService)** — REJECTED: creates a perverse incentive to suppress logging to avoid a manifest declaration, costing the host observability precisely for plugins that fail to declare; a LogService is write-only with no state return and conveys no authority.

## Consequences

Positive: every plugin stays fully observable with no manifest change; the 14-token vocabulary contains only genuine authority grants; INV-PLUGIN-48 is a testable structural assertion (ambient functions absent from host.v1). Negative: the boundary needs a documented heuristic for future borderline cases; the Log RPC is deleted with no migration path (none needed — it was unserved). Neutral: Lua ambient injection is left intact by this sub-spec; ungating mechanics are a sub-spec 5 concern.
