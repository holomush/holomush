<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-q924m; do not edit manually; use `/adr update holomush-q924m` -->

# Use topic-tab navigation over a single grouped sidebar

**Date:** 2026-05-28
**Status:** Accepted
**Decision:** holomush-q924m
**Deciders:** Sean Brandt

## Context

SP2 (`holomush-38kmt`) established an autogenerate sidebar with the 5 audience-section groups in a **single** sidebar. SP5 proposes adopting `starlight-sidebar-topics` to surface those 5 sections as **top-level tabs**, each carrying its own scoped sidebar — matching the navigation model of the peer docs sites used as exemplars (Hermes Agent, OpenClaw, Pi). The SP2 autogenerate ADR governs how the groups are *derived*; it does not cover the tab-vs-single-list *surfacing* model.

## Decision

Adopt `starlight-sidebar-topics` to convert the 5 SP2 autogenerate groups into scoped **topic tabs** — each tab owns its sidebar. The underlying autogenerate groups remain in config, so the single-sidebar model is a fallback if the plugin is ever dropped.

## Rationale

- The audience-first IA (SP2) produces exactly the 5 sections topic-tabs require — no restructuring cost; the plugin consumes the existing directory structure.
- Scoped sidebars cut cognitive load per persona: a player never scrolls past plugin-dev pages; an operator sees only operating docs.
- The single long sidebar was explicitly the shortcoming motivating SP5's comparison against peer docs sites.
- `starlight-sidebar-topics` (HiDeoo, ~17.6K weekly, Starlight ≥0.38; we're 0.39) is the maintained option for this nav shape.

## Alternatives Considered

- **Single grouped sidebar (SP2 autogenerate status quo)** — no new dependency, all sections visible at once, already working; but presents all 5 audience sections as one long list regardless of reader role, and doesn't match the exemplar nav model. Rejected (retained only as fallback).

## Consequences

**Positive:** each reader sees only their section's sidebar; nav model matches the exemplars (perceived cohesion); no IA restructuring needed.

**Negative:** adds the `starlight-sidebar-topics` plugin dependency (abandonment → revert to grouped sidebar); topic-tab root links must resolve, so INV-8 link-check + INV-5 render check become mandatory gates.

**Neutral:** `astro.config.mjs`'s `sidebar` array is replaced by a `topics` array (different config shape); does **not** supersede `holomush-38kmt` (that governs group derivation; this governs surfacing).
