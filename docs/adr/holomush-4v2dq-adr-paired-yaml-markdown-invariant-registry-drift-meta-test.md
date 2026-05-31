<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-4v2dq; do not edit manually; use `/adr update holomush-4v2dq` -->

# ADR: Paired YAML+markdown invariant registry with drift meta-test

**Date:** 2026-05-31
**Status:** Accepted
**Decision:** holomush-4v2dq
**Deciders:** Sean Brandt

## Context

The invariant registry needed a format that serves two distinct consumers: human contributors looking up invariant definitions, and CI/lint tooling verifying drift protection (every invariant has a test, no orphan invariants). A single-markdown-file approach was considered but parsing markdown tables in Go tests is fragile — whitespace drift, malformed rows, or a missing pipe character breaks the meta-test silently. The existing per-family meta-tests used hardcoded []int slices (e.g., var phase3cInvariants = []int{53, 54, ...}) which are robust against formatting drift but require manual synchronization with spec documents.

## Decision

The invariant registry uses a paired format: docs/architecture/invariants.yaml is the machine-readable source of truth, and docs/architecture/invariants.md is the human-readable view. The unified meta-test at test/meta/invariant_registry_test.go reads the YAML file directly (no markdown parsing). A CI lint check (scripts/check-invariant-registry-consistency.sh) verifies the YAML and markdown are in sync — every YAML entry has a matching markdown table row and vice versa. The existing per-family hardcoded-slice meta-tests (i_priv_coverage_test.go, inv_binding_test.go, i_pres_coverage_test.go, inv_p4_coverage_meta_test.go, inv_p5_coverage_meta_test.go, scenes_phase6_invariants_test.go, plugin_config_invariants_test.go) are retired once the unified test passes.

## Rationale

YAML is already a project dependency (gopkg.in/yaml.v3 in go.mod) and is trivially parseable by Go tests. The dual-format approach separates concerns: the YAML is the drift contract (stable field names, no formatting ambiguity), while the markdown is the human presentation (scope index prose, tables formatted for readability). The lint consistency check catches drift between the two — if someone edits only the YAML or only the markdown, CI fails. The unified meta-test eliminates the maintenance burden of per-family hardcoded slices, which required manual updates whenever a new invariant was added to a spec.

## Alternatives Considered

1. **Single markdown file with table parsing** — simpler (one file, no sync check needed), but parsing markdown tables in Go is fragile. A whitespace change in a table row breaks the meta-test with an opaque error. Rejected: the CI reliability cost outweighs the simplicity gain.
2. **JSON sidecar** — equally machine-readable, but YAML is already a project dependency and is more human-friendly to edit by hand. Rejected: no advantage over YAML.
3. **Keep per-family hardcoded slices** — no new infrastructure, no format sync issues. But each new invariant family requires a new meta-test file with a manual []int slice that must be kept in sync with the spec. Rejected: this is the status quo that the registry is replacing — it does not scale.
4. **TOML registry** — machine-readable and used in the project (rumdl.toml), but less familiar for structured data than YAML. Rejected: YAML already in go.mod, no new dependency needed.

## Consequences

- Two files to maintain (YAML + markdown), with a CI check enforcing consistency.
- Adding a new invariant means: (1) add entry to invariants.yaml, (2) add row to invariants.md table, (3) add // Verifies: annotation in a test file. The CI check ensures steps 1 and 2 stay in sync.
- The unified meta-test is a single Go file that replaces 7 existing meta-test files.
- The drift-protection is stronger than before: the meta-test not only verifies bindings exist for registered invariants, but also scans docs/superpowers/specs/ for orphan invariant IDs not in the registry.
