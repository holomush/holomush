<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-2hhq2; do not edit manually; use `/adr update holomush-2hhq2` -->

# Reuse verb category as the communication-content discriminator

**Date:** 2026-07-04
**Status:** Accepted
**Decision:** holomush-2hhq2
**Deciders:** sean

## Context

The emit gate needs a signal for "must this payload validate as CommunicationContent?" The existing per-verb `category` field (`VerbRegistration.Category`, `internal/core/registry.go:17`) already drives client and telnet rendering dispatch (`EventRenderer.svelte:27`, `gateway_handler.go:1131`). Two verbs were found mis-shelved against that use: `pemit` (`category: command`) and `whisper_notice` (`category: communication`), which this design corrects regardless of the discriminator choice.

## Decision

`category: communication` is the sole discriminator for `CommunicationContent` enforcement — no new per-verb flag is introduced. `pemit` recategorizes command→communication and `whisper_notice` recategorizes communication→system, establishing the invariant `category: communication ⟺ the payload is a CommunicationContent`.

## Rationale

- For genuine conversational content, "renders as communication" and "carries the communication content body" are the same population once the two mis-shelved verbs are corrected.
- A second near-identical axis (a `communication_content` flag) would duplicate `category` for no gain and require permanent upkeep to stay in sync.

## Alternatives Considered

- **Reuse existing `category` field (chosen):** zero new manifest surface; makes the render-dispatch axis do double duty without duplication; forces correction of the two mis-shelved verbs rather than tolerating them.
- **New parallel `communication_content` manifest flag (rejected):** decouples "renders as communication" from "carries CommunicationContent", but is a redundant second axis for what — once the two verbs are fixed — is the same population, reintroducing the per-field duplication the design exists to eliminate.

## Consequences

- Positive: one manifest field drives both rendering dispatch and payload-conformance enforcement; `pemit` gets correct `CommunicationRenderer` treatment instead of leaking through `CommandRenderer`.
- Negative: a future verb needing partial overlap (communication rendering without a full content body, or vice versa) has no independent axis and would force another recategorization.
- Neutral: `whisper_notice`'s move to `system` is a manifest-only change; it carries no content body under either category.
