<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 24. Bootstrap Sequence

> [Back to Decision Index](../README.md)

**Review finding:** The spec defined seed policies but didn't specify how they
are created. With default-deny and no policies, the first admin would be locked
out (chicken-and-egg problem).

**Decision:** Server startup detects an empty `access_policies` table and seeds
policies as the `system` subject, which bypasses policy evaluation entirely.
Seed policies use deterministic names (`seed:player-self-access`, etc.) for
idempotency.

**Rationale:** The `system` subject bypass already exists in the evaluation
algorithm (step 1). Seeding at startup is consistent with how the static system
initializes default roles. Deterministic naming prevents duplicate seeds on
restart.

**Updates [Decision #36](../phase-7.6/036-direct-replacement-no-adapter.md):** Adds bootstrap mechanism to
the direct replacement strategy.
