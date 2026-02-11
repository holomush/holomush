<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 57. ADR Format Evolution

> [Back to Decision Index](../README.md)

**Review finding:** Epic 7 design decisions use a streamlined format compared to
traditional ADR structure.

**Decision:** Epic 7 ADRs use a lightweight "Question > Options > Decision >
Rationale" format appropriate for design-phase decisions. This format is
documented in `docs/specs/decisions/epic7/README.md`. Traditional ADRs may use
more detailed formats with explicit Consequences sections when needed.

**Rationale:** The ADR numbering follows a 3-digit convention (001-110) for Epic
7 design decisions, stored in `docs/specs/decisions/epic7/` with subdirectories
per phase. Most Epic 7 ADRs use a concise format optimized for design decisions
made during architecture exploration. The format emphasizes:

- **Question:** What problem or choice needs resolution?
- **Options Considered:** Table format comparing alternatives
- **Decision:** The chosen approach
- **Rationale:** Why this approach was selected

This format differs from traditional ADRs with explicit Consequences sections
because Epic 7 ADRs primarily document design-time decisions where consequences
are often implicit in the rationale. Some ADRs (like #10) may include additional
sections (Testing Requirements, Constraints) when needed for clarity.

**Cross-reference:** ADR index at `docs/specs/decisions/epic7/README.md`; bead
`holomush-ly15`.
