<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 22. Flat Prefixed Strings Over Typed Structs

> [Back to Decision Index](../README.md)

**Review finding:** `AccessRequest` uses flat strings (`Subject:
"character:01ABC"`) parsed at evaluation time, which is inconsistent with the
world model's typed structs (`Location.ID ulid.ULID`).

**Decision:** Keep flat prefixed strings for `AccessRequest`.

**Rationale:** Flat strings simplify serialization for audit logging (no
marshaling needed) and the `policy test` admin command (admins type
`character:01ABC` directly — no struct construction required). The format is
consistent with external API boundaries (telnet/web protocols already exchange
string identifiers). If profiling shows parsing as a bottleneck, introduce a
cached parsed representation without changing the public API.
