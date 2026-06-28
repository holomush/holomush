<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-nla30; do not edit manually; use `/adr update holomush-nla30` -->

# AIP-134 FieldMask + client-side changed-fields diff for partial scene updates

**Date:** 2026-06-28
**Status:** Accepted
**Decision:** holomush-nla30
**Deciders:** Sean Brandt

## Context

The web Settings sheet (slice 2/4 of holomush-5rh.24) edits six mutable scene fields (title, description, tags, visibility, pose_order_mode, content_warnings) through SceneAccessService.UpdateScene -> SceneService.UpdateScene. Proto3 zero-value ambiguity means an empty string is indistinguishable from "intentionally cleared" versus "not touched", so the partial-update contract must express field presence explicitly. The downstream scenev1.UpdateSceneRequest already carries a google.protobuf.FieldMask; the facade and BFF layers must decide how to propagate or construct it from a web form.

## Decision

Partial scene updates use google.protobuf.FieldMask update_mask on both the SceneAccessService facade and the WebService BFF requests. The web client computes the mask by diffing form state against the SceneInfo baseline loaded when the sheet opens (settingsMask, a pure function) and skips the RPC entirely when the mask is empty. The facade and BFF forward the mask verbatim; the handler applies only masked fields (AIP-134). UpdateSceneResponse returns the full post-update SceneInfo, which the client applies to its workspace cache (applySceneInfo) with no follow-up GetScene.

## Rationale

- Proto3 zero-value ambiguity rules out implicit-null merge patch: an empty string cannot distinguish "cleared description" from "field untouched" without an explicit mask.
- AIP-134 FieldMask is the industry-standard partial-update pattern; scenev1.UpdateSceneRequest already uses it, so the facade mirrors the downstream contract with no translation layer.
- Empty-mask detection client-side avoids a gratuitous RPC when the user opens and closes the sheet without changes.
- A client-side diff against an immutable baseline is the natural shape for a load-then-edit form and localizes all mask logic in one unit-tested pure function (settingsMask).

## Alternatives Considered

- **AIP-134 FieldMask + client-side changed-fields diff (chosen):** unambiguous intent; zero-value-safe; empty mask skips the RPC; mirrors the existing downstream contract.
- **Full-replace, send all six fields every save (rejected):** clobbers fields a concurrent editor changed but this user did not touch; cannot express an intentional clear distinctly.
- **Implicit-null merge patch, absent/zero = no change (rejected):** proto3 scalar defaults are indistinguishable from intentional clears; would force optional/oneof wrapper churn.
- **Per-field setter RPCs, SetSceneTitle / SetSceneVisibility / ... (rejected):** multiplies proto RPCs by field count; a six-field form needs six round-trips or batching.

## Consequences

- Positive: no unintentional field overwrites; zero-value fields are unambiguous; the client skips the round-trip on a no-op close; facade/BFF forward the mask with no re-derivation.
- Negative: the client must hold the original baseline for the open sheet's lifetime; each new editable field requires a settingsMask update or it is silently excluded from saves.
- Neutral: response-carried SceneInfo avoids a follow-up GetScene (applySceneInfo); location_id is excluded from the mask surface by a scope decision (scene anchor, not a settings field).
