<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->

# Use k6 + Custom xk6 Binary for Full-System Load Testing

**Date:** 2026-05-28
**Status:** Accepted
**Decision:** holomush-evggu
**Deciders:** Sean Brandt

## Context

HoloMUSH's full-system load harness must drive two bespoke wire protocols: the **Connect** protocol (HTTP/2-via-TLS-ALPN, the browser SPA's path through `internal/web/server.go`'s `NewWebServiceHandler`) and **raw-TCP telnet** (the gateway telnet listener). A Go-native generator would have exact fidelity and could attach to the in-process `integrationtest` CoreServer, but it would require reimplementing ramp executors, percentile aggregation, threshold-gating, and Grafana output. k6 provides all of those as batteries, but its built-in `k6/net/grpc` module drives the **gRPC** envelope, not the **Connect** envelope the browser actually uses on the same handler. The xk6 extension mechanism lets us bundle a Go module that imports `connectrpc.com/connect` directly — and a community extension (`bumberboy/xk6-connectrpc`, born from grafana/k6#5193) already exists — eliminating the protocol-fidelity gap.

## Decision

Adopt **k6 with a custom `xk6 build` binary** bundling `xk6-connectrpc` (Connect wire protocol, H2/TLS) and an xk6 telnet/raw-TCP module, driving the **full Dockerized `holomush` binary at the network boundary**. A mandatory P1 spike vets `xk6-connectrpc` for server-streaming support (the `Subscribe` event path) before committing to use-as-is vs. fork-and-extend. The in-process core-tier load driver is deferred (component-level coverage stays with Go micro-benchmarks + `task soak:eventbus`).

## Rationale

- **Connect-envelope fidelity:** `xk6-connectrpc` imports `connectrpc.com/connect`, whose client speaks the Connect protocol by default, driving the exact wire path the SvelteKit PWA uses (INV-LOAD-1). k6's built-in gRPC module would exercise a different envelope on the same handler.
- **Telnet fidelity:** a raw-TCP xk6 module hits the real gateway telnet listener bit-exactly (INV-LOAD-2); no off-the-shelf HTTP tool can drive the bespoke telnet protocol.
- **Avoid re-inventing k6 batteries:** ramp/arrival-rate executors, p99 metrics, thresholds-as-exit-code pass/fail gates, and Prometheus remote-write are load-bearing for the SLO program and slot into the project's existing OTel/Grafana stack — not worth reimplementing.
- **Supply-chain discipline:** pinned extension commits + reproducible `xk6 build` + CI cache + license check contain the new-toolchain risk.

## Alternatives Considered

- **Go-native load generator (both tiers)** — zero new toolchain, exact fidelity for both protocols, and could attach to the in-process `integrationtest` CoreServer tier. Rejected: requires reimplementing the ramp/percentile/threshold/dashboard machinery k6 provides for free, and the in-process advantage is deferred (Phase 3) so it is not realized in P1/P2.
- **k6 with the built-in `k6/net/grpc` module** — no custom extension, simpler build. Rejected: drives the gRPC envelope, not the Connect envelope the browser uses on the same handler (a real fidelity gap), and cannot drive raw-TCP telnet.
- **Off-the-shelf HTTP tools (vegeta, Gatling)** — mature, no custom builds. Rejected: cannot drive the bespoke telnet protocol, cannot reach the in-process core tier, and fragment into disjoint tools with no shared threshold/dashboard story.
- **k6 + custom xk6 binary (`xk6-connectrpc` + xk6-telnet)** — exact fidelity on both protocols plus k6 batteries and existing-Grafana reuse; distributed execution available for ceiling experiments. Chosen.

## Consequences

**Positive:** a single scenario script drives both protocols with shared k6 metrics and threshold gates; Grafana/Prometheus output slots into the existing OTel stack with no new dashboarding infrastructure; distributed load generation (Phase 3) becomes a k6 config change, not an architectural rewrite.

**Negative:** a new k6/xk6 toolchain is added to the build surface and the custom binary must be maintained; `xk6-connectrpc` is young, so a streaming gap may force a fork plus an upstream-maintenance obligation; the harness cannot attach to the in-process `integrationtest` CoreServer tier (it needs a running server), deferring the in-process tier to Phase 3.

**Neutral:** the in-process Go core-tier driver is explicitly deferred; the fork-vs-upstream decision is contingent on the P1 spike outcome.

## References

- Spec: docs/superpowers/specs/2026-05-28-load-perf-testing-harness-design.md §3.1
- Design bead: holomush-ql7ef
- grafana/k6#5193 (Connect RPC support); bumberboy/xk6-connectrpc
