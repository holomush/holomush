<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 107. Policy Reload Bypass Acceptance Criterion in T31

> [Back to Decision Index](../README.md)

**Review finding (PR #69, Critical S1):** Spec requirement S5
(holomush-5k1.346) mandates an out-of-band cache reload mechanism that
bypasses ABAC authorization, restricted to local/system callers. The plan
defers the full `policy` command suite to Epic 8, leaving no task covering
this MUST-level requirement.

**Question:** Where should the `policy reload` bypass acceptance criterion
be added?

**Options considered:**

| Option                    | Pros                                       | Cons                                  |
| ------------------------- | ------------------------------------------ | ------------------------------------- |
| Add to T18 (cache mgmt)  | Natural fit for cache behavior             | T18 is about cache internals, not CLI |
| Add to T31 (admin cmds)  | Fits admin command surface; already in 7.4 | Adds scope to T31                     |
| New dedicated task        | Clean separation                           | Overhead for single criterion         |

**Decision:** Add acceptance criterion to **T31 (admin commands)**.

**Rationale:** T31 already defines the admin command surface for Phase 7.4.
Adding `policy reload` as a minimal bypass mechanism fits naturally within
that task's scope. The full command suite remains deferred to Epic 8, but
the MUST-level bypass is covered.
