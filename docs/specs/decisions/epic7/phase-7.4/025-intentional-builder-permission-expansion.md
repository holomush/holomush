<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 25. Intentional Builder Permission Expansion

> [Back to Decision Index](../README.md)

**Review finding:** Seed policies grant builders `delete` on locations, but the
static system only grants `write:location:*` — no `delete:location:*`. This
would cause shadow mode disagreements.

**Decision:** Preserve the expansion as intentional. Builders who can create and
modify locations SHOULD also be able to delete them. The static system's
omission was a gap, not a deliberate restriction.

**Rationale:** Builder workflow requires the ability to clean up test locations.
Without `delete`, builders must ask an admin to remove locations, which is an
unnecessary bottleneck.

_Note: Shadow mode was removed by [Decision #37](../phase-7.6/037-no-shadow-mode.md). The
original rationale about shadow mode exclusions no longer applies, but the
permission expansion itself is intentional._
