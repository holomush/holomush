<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-avxk7; do not edit manually; use `/adr update holomush-avxk7` -->

# Release narrative via in-session Claude Code skill, not CI automation

**Date:** 2026-06-28
**Status:** Accepted
**Decision:** holomush-avxk7
**Deciders:** HoloMUSH Contributors

## Context

Cutting a release produces only a GoReleaser-filtered commit log, with no
narrative of what changed and why. Adding narrative summarization requires an
LLM; the question is where that LLM runs. The release-cut path (release.yaml,
App-token, dispatch-only) must remain untouched per ADR holomush-jfb9x.

## Decision

The narrative pipeline runs as a local maintainer-invoked Claude Code skill
(`/release-notes <tag>`), not as CI automation. The in-session model drafts the
narrative; the maintainer edits and approves it before publishing. No LLM API
key is added to CI, and the release-cut path is unchanged.

## Rationale

- The in-loop maintainer IS the human review step; collapsing generation and review into one interactive surface eliminates a CI draft-review round-trip.
- "No LLM API key in CI" is a hard requirement; CI automation would need a secret and a separate review mechanism.
- Interactive disambiguation (when a theme is ambiguous) is only possible in-session.
- release.yaml and the App-token dispatch path remain completely untouched.

## Alternatives Considered

- **Maintainer-run in-session skill (chosen):** maintainer already in the loop; summarizing model is the in-loop model; no CI secret; release-cut CI path unchanged.
- **Headless CI script with LLM API key (rejected):** fully automated, but requires an API secret in CI, adds a draft-review round-trip, cannot interactively disambiguate, and reintroduces complexity to release.yaml.

## Consequences

- Positive: no new CI secrets; maintainer can edit the draft before publishing; release-cut path unchanged.
- Negative: requires a manual maintainer action post-tag.
- Neutral: pairs naturally with the existing bead/theme curation workflow.
