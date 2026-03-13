<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 109. Dedicated Policy for `list_characters` Action

> [Back to Decision Index](../README.md)

**Review finding (PR #69, Minor M9):** T22b gap G3 leaves the
`list_characters` action as an unresolved OR choice between expanding an
existing policy or creating a dedicated one.

**Question:** Should `list_characters` be added to the existing
`seed:player-location-read` policy or get its own dedicated policy?

**Options considered:**

| Option                                        | Pros                              | Cons                                 |
| --------------------------------------------- | --------------------------------- | ------------------------------------ |
| Expand `seed:player-location-read`            | Fewer policies; simpler           | Broader grant than needed            |
| Dedicated `seed:player-location-list-characters` | Least-privilege; granular control | One more policy to manage            |

**Decision:** Create a **dedicated `seed:player-location-list-characters`
policy**.

**Rationale:** Least-privilege is a core ABAC principle. `list_characters`
is a distinct action from `read` — combining them grants broader access
than necessary. A dedicated policy allows independent revocation and audit
visibility for character listing operations.
