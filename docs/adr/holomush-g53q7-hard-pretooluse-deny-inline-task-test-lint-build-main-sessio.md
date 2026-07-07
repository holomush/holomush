<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-g53q7; do not edit manually; use `/adr update holomush-g53q7` -->

# Hard PreToolUse deny for inline task test/lint/build in the main session

**Date:** 2026-07-06
**Status:** Accepted
**Decision:** holomush-g53q7
**Deciders:** Sean Brandt

## Context

The four local-* offload agents (holomush-wagqb Phase 2) shipped with no enforcement demand layer, so the main session still ran `task test|lint|build` inline via Bash, defeating the token-offload design. holomush-drf7b adds hook-level enforcement to make offload the default path.

## Decision

Extend `.claude/hooks/enforce-task-runner.sh` to DENY inline `task test|test:int|test:cover|lint|build` in the main session via PreToolUse JSON (`hookSpecificOutput.permissionDecision: "deny"`), with the reason naming the `local-check` replacement. Subagent calls are exempt (detected via `agent_id` in hook input). Escape hatch: `# offload-exempt` appended to the command (mirrors `# jj-exempt`). `task pr-prep` variants get a soft stderr nudge only — never denied, because the parent MUST run the final pre-push gate inline (schema-regeneration side-effects). Deny mode ships only after an empirical probe confirms `agent_id` presence and the deny-JSON schema on the running Claude Code version; otherwise `OFFLOAD_ENFORCE=nudge` is the fallback default.

## Rationale

- Repo precedent: soft nudges for verbose commands get rationalized away — raw `go test` needed a hard block, not a nudge.
- Ask-mode is incompatible with unattended agent sessions (no human to answer).
- pr-prep exemption preserves the non-negotiable inline final gate (spec N4).
- Probe-gating (RD5) avoids breaking the offload agents themselves if the harness fact drifts across CC versions.

## Alternatives Considered

- **Hard deny + escape hatch (chosen):** enforced at the action moment; matches the existing hard-block precedent; hatch preserves genuine inline-output needs.
- **Soft nudge only (rejected):** repo history shows nudges get ignored under momentum.
- **Ask-mode (rejected):** adds a human prompt to every verification cycle in unattended sessions.

## Consequences

- Positive: offload agents get real demand instead of sitting unused; the deny message names the exact replacement dispatch.
- Negative: a deny cancels sibling calls in a parallel tool batch (documented in the hook header, accepted); enforcement depends on a probed harness fact.
- Neutral: `# offload-exempt` extends the established comment-token escape-hatch idiom.
