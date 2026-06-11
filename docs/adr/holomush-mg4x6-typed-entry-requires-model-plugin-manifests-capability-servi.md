<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-mg4x6; do not edit manually; use `/adr update holomush-mg4x6` -->

# Typed-entry requires model for plugin manifests (capability/service kind tags)

**Date:** 2026-06-11
**Status:** Accepted
**Decision:** holomush-mg4x6
**Deciders:** Sean Brandt

## Context

The plugin manifest `requires:` field held a flat list of proto service names, conflating host capabilities with plugin-provided services and overloading one field for both DAG load-order and Lua capability injection. The manifest already carries a DEPRECATED top-level `capabilities:` field (manifest.go:94-96) that exists only to detect old-format manifests and error.

## Decision

Adopt typed object entries in `requires:`, each with a mandatory `capability:` or `service:` kind tag plus optional per-entry attributes (`version`, `optional`, `scope`). `capability` names use a controlled short vocabulary (host-provided, no DAG edge); `service` names use full proto paths (provider-before-consumer edge). The pre-existing `dependencies:` version map folds into service entries' `version`. `provides:` is unchanged.

## Rationale

- Per-entry attributes (least-privilege scope/access, optional, version) require the object form; flat strings would force a second manifest-wide migration later.
- The explicit kind tag is an author assertion the resolver VALIDATES: `capability: X` where X is plugin-provided yields MISDECLARED_DEPENDENCY, never a silent reclassification.
- The deprecated top-level `capabilities:` field makes the two-flat-fields option actively hazardous (name collision).

## Alternatives Considered

**Two flat fields (`capabilities:` + `requires:`)** — rejected: collides with the deprecated top-level `capabilities:` field; flat strings cannot carry per-entry attributes.
**One flat list with implicit kind detection** — rejected: resolver must guess kind; a mis-typed capability silently reclassifies to an unsatisfied service; still no attributes.
**Typed object entries with explicit kind tag** — CHOSEN.

## Consequences

**Positive:** resolver enforces kind correctness at boot; forward-compatible (new attributes need no migration); single declaration site absorbs the `dependencies:` map. **Negative:** all in-tree manifests need a migration pass (sub-spec 5); tooling reading flat `requires` must update. **Neutral:** `provides:` unchanged — only the consumer side is redesigned.
