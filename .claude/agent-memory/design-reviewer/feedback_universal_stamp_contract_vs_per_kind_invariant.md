---
name: Universal stamp-site contract vs per-kind invariant contradiction
description: When a spec asserts a universal "every stamp site stamps a valid X" contract while the per-kind invariant carves out an exception, runtime failure modes contradict the invariant
type: feedback
---

When a design spec asserts BOTH a per-kind invariant carving out an exception (e.g., "every Actor for kinds {A, B, C} carries a ULID") AND a universal stamp-site contract ("every stamp site stamps a valid ULID; ULID parse failure is a hard error"), the two contradict at runtime: the kind that is excluded from the invariant still flows through the universal failure-mode contract.

**Why:** Spec authors build the per-kind invariant first (correctly carving out the exempted kind), then write the failure-mode / bus-conversion contract paragraph as a sweeping universal because that's how the post-epic happy path looks. They forget that the exempted kind's existing production stamp sites still flow through the bus conversion, and now those stamp sites trigger the universal failure mode.

**How to apply:** When a spec carves out a kind in an invariant ("MUST hold for {A, B, C}"), grep all production stamp sites for that kind ("D"). Walk each through the failure-mode contracts the spec proposes. If any contract fires on a kind-D stamp site, the contract needs an explicit kind-D exception, OR the kind-D stamp sites need migration, OR the kind-D inclusion in the invariant.

Seen 2026-05-04 in legacy-id-elimination r2: INV-W9ML-1 carved out `ActorSystem` from the ULID requirement, but `coreActorToEventbusActor` was specified to return `ACTOR_ID_NOT_ULID` on parse failure universally. Three production sites (`internal/world/event_store_adapter.go:34`, `internal/grpc/server.go:531`, `internal/command/types.go:619`) stamp `core.Actor{Kind: ActorSystem, ID: "system"|"world-service"}` — all would fail at cutover under the universal contract.

This is a sibling of "lint heuristic enumeration claim ≠ heuristic logic" (existing entry): both are cases where a spec's universal claim doesn't match the per-instance behavior it would induce.
