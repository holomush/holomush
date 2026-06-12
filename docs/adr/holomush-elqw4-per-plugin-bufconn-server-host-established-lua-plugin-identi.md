<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-elqw4; do not edit manually; use `/adr update holomush-elqw4` -->

# Per-plugin bufconn server for host-established Lua plugin identity

**Date:** 2026-06-12
**Status:** Accepted
**Decision:** holomush-elqw4
**Deciders:** Sean Brandt

## Context

The binary plugin runtime establishes plugin identity by constructing a dedicated *grpc.Server per plugin via newPluginHostServiceServer(host, pluginName), baking the ABAC subject into hostCapabilityBase at wiring time — never accepting it from the wire (INV-PLUGIN-22). The Lua runtime needed an equivalent unforgeable-identity path when gaining its own host-capability gRPC server (sub-spec 3, holomush-eykuh.2).

## Decision

Each Lua plugin VM gets its own bufconn.Listener + *grpc.Server with pluginName baked into hostCapabilityBase at construction time, mirroring newPluginHostServiceServer (reusing internal/plugin.NewInProcessConn). Identity is connection-scoped and host-established at wiring time; the in-process boundary is strictly stronger than binary (a plugin cannot enumerate other connections).

## Rationale

Connection-scoped identity prevents any plugin from claiming another plugin's ABAC subject — the forgery surface eliminated by ec22.1 for binary is eliminated symmetrically for Lua. Reuses the existing hostCapabilityBase identity model with zero new identity mechanisms; ABAC subject derivation and emit actor-vouching come free by reusing the same hostcap servers.

## Alternatives Considered

Shared server + plugin-name metadata header — REJECTED: plugin-name would be wire-supplied and forgeable, letting a plugin send any name in the header and bypass ABAC subject derivation (violates INV-PLUGIN-22).

## Consequences

Positive: INV-PLUGIN-22 holds for Lua by construction, not policy; no new auth-token mechanism; exact symmetry with binary. Negative: one *grpc.Server per Lua plugin VM (footprint scales with plugin count). Neutral: in-process bufconn boundary means plugins cannot observe each other's connections.
