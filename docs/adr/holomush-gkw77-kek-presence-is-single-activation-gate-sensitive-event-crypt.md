<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-gkw77; do not edit manually; use `/adr update holomush-gkw77` -->

# KEK presence is the single activation gate for sensitive-event crypto

**Date:** 2026-06-09
**Status:** Accepted
**Decision:** holomush-gkw77
**Deciders:** Sean Brandt

## Context

PR #4409 shipped the DEK/AuthGuard/audit machinery dormant. The live publisher gates encryption on `RekeyManager != nil` (KEK presence); the live subscriber builds its authenticated identity only when a separate `cryptoEnabled` flag is set, and that flag is never set true in production. A deployment with a KEK present but `cryptoEnabled` false encrypts on publish while delivering metadata-only payloads to every reader, including scene/comms participants. The two gates must be one signal.

## Decision

The server's activation flag (`cryptoActive`, renamed from `cryptoEnabled`) MUST be wired from `RekeyManager != nil` at `CoreServer` construction in cmd/holomush/sub_grpc.go, and the guard `if s.bindings != nil && s.cryptoActive` MUST be applied at BOTH identity-build sites (internal/grpc/server.go:995 Subscribe and internal/grpc/query_stream_history.go:306 QueryStreamHistory) in lockstep. Flag removal is out of scope (deferred until mandatory-KEK makes it always-true).

## Rationale

- A single source of truth (KEK presence) makes the publish/subscribe asymmetry structurally impossible; no config drift can reintroduce it.
- `s.bindings != nil` is NOT a valid KEK-presence signal: the binding repository is wired unconditionally for guest-session construction, so gating on it would activate binding lookup on KEK-less boots and hard-fail SUBSCRIBE_BINDING_LOOKUP_FAILED.
- Defers flag removal so mandatory-KEK can make the flag always-true before a follow-up cleanup removes it (cf. the WithCryptoEnabled deletion in #4399).

## Alternatives Considered

- Keep cryptoEnabled as a separate explicit flag set true in prod config: rejected — two flags can still diverge on misconfiguration; adds operator-visible surface for what should be a structural consequence of KEK presence.
- Delete the gate and do binding lookup unconditionally: rejected — hard-fails on KEK-less boots, which are valid until mandatory-KEK lands.

## Consequences

Positive: the single-gate invariant is structurally assertable via a gate truth-table test; no metadata-only delivery to participants when KEK is present. Negative: all `cryptoEnabled: true` struct literals in test files must be renamed. Neutral: `cryptoActive` remains as a defensive invariant until a follow-up cleanup removes it.
