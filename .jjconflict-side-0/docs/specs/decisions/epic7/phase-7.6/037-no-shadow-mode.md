<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 37. No Shadow Mode

> [Back to Decision Index](../README.md)

**Review finding:** Shadow mode validates that seed policies replicate the
static system's behavior. But the static system has known gaps: `$here` resource
patterns that never match actual call site resource strings (dead permissions),
missing `delete:location` for builders, legacy `@`-prefixed command names. The
seed policies intentionally fix these gaps, making 100% agreement impossible
without exclusion filtering — which itself is bug-prone.

**Decision:** Remove shadow mode entirely. The ABAC seed policies define the
correct permission model from scratch, not a replica of the static system.

**Rationale:** Shadow mode is only valuable when migrating a live system with
existing users. HoloMUSH has no releases. The seed policies are validated by
integration tests, not by runtime comparison with a legacy system. This
eliminates an entire class of complexity and removes the risk of exclusion
filtering bugs masking real policy errors.
