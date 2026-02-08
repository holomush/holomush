<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 57. ADR Format Evolution

> [Back to Decision Index](../README.md)

**Review finding:** New ADRs (0009-0016) use a different section structure than
existing ADRs (0001-0008). Old format: Context > Options Considered > Decision
\> Consequences > References. New format: Context > Decision > Rationale >
Consequences > References (with Options embedded in Context).

**Decision:** This is intentional evolution. The new format is internally
consistent across all 8 new ADRs and makes the Rationale more prominent.
Document the new format in `docs/adr/README.md`. Do not retroactively change
old ADRs. Additionally, move ADR 0010's unique "Testing Requirements" section
into the Consequences section or the main spec's testing section to maintain
ADR structural consistency.

**Rationale:** ADR formats naturally evolve as teams learn what information is
most useful. The new format emphasizes Rationale (why) over Options (what
else), which is more valuable for future readers. Documenting the evolution
prevents confusion without requiring retroactive changes.

**Cross-reference:** All ADRs in `docs/adr/`; bead `holomush-ly15`.
