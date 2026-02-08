<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 49. Revised Audit Volume Estimate

> [Back to Decision Index](../README.md)

**Review finding (I7):** The original estimate of ~864K records/day assumed
~5 checks/sec. Real MUSH workloads (movement, look, inventory, say, property
reads) produce ~120 checks/sec peak at 200 users.

**Decision:** Revise the estimate: `all` mode produces ~10M records/day (~35GB
at 7-day retention with uncompressed audit rows). `denials_only` mode remains
practical at a fraction of this volume.

**Rationale:** The corrected estimate affects operational guidance (disk
provisioning, retention policy, partition strategy). Admins need accurate
numbers to make informed decisions about audit mode selection.
