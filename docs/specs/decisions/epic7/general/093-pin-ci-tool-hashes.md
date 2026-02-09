<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 93. Pin CI Tool SHA256 Hashes as Literals

> [Back to Decision Index](../README.md)

**Question:** Should CI workflows pin SHA256 hashes as literals or fetch checksums
dynamically from the same origin as the binary?

**Context:** Current CI workflows download tool binaries and verify them against
SHA256 checksums fetched from GitHub. Both the binary and checksum originate from
the same source, making the verification integrity-only (detects corruption) but
not supply-chain-resistant (a compromised release includes a matching checksum).

**Decision:** Pin SHA256 hashes as string literals directly in CI workflow files.
When updating tool versions, the pinned hash MUST be updated simultaneously. Each
pinned hash MUST include a comment with the tool name and version for
maintainability.

**Rationale:** Pinned hashes provide defense-in-depth against supply-chain attacks
on upstream release artifacts. The maintenance cost is low — hash updates are
infrequent and mechanical. The security benefit is meaningful: an attacker who
compromises a GitHub release cannot silently replace the binary if the expected hash
is committed in a separate repository.

**Cross-reference:** Review finding I10 (PR #69); `.github/workflows/` CI
configuration.
