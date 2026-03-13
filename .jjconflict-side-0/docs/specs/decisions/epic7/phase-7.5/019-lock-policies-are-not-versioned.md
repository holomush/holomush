<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 19. Lock Policies Are Not Versioned

> [Back to Decision Index](../README.md)

**Review finding:** Lock commands compile to "scoped policies" but the spec
didn't specify what happens on modification — is the old policy versioned,
deleted, or updated in place?

**Decision:** Lock-generated policies are NOT versioned.

- `lock X/action = condition` creates a policy via `PolicyStore.Create()`
  with naming convention `lock:{resource-type}:{resource-id}:{action}`.
- `unlock X/action` deletes the policy via `PolicyStore.DeleteByName()`.
- Modifying a lock deletes the old policy and creates a new one in a single
  transaction.

**Rationale:** Lock versioning would explode the audit log for casual player
actions (setting a lock on a chest shouldn't generate version history). Admins
who need version history use the full `policy` command set. Player locks are
ephemeral by design — they exist for in-game convenience, not governance.
