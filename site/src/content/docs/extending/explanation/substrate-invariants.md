---
title: "Substrate contract invariants"
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

The substrate contract is the set of rules that govern what plugins may rely on
from the substrate and what they MUST NOT touch. This page explains those rules
and why they exist. For the inventory of surfaces plugins can rely on, see
[Substrate Contract](/extending/reference/substrate-contract/).

## Plugin-boundary rule INV-S1

**Plugin PRs MUST touch only `plugins/<plugin-name>/`** and may import from
approved substrate-facing packages (`pkg/plugin/*`, generated proto under
`pkg/proto/`). That is the complete permitted footprint.

If your plugin needs a substrate capability that does not yet exist, that
substrate change is **separate work**: its own bead, its own PR, its own review
gate. Bundling a substrate change inside a plugin PR is forbidden — it bypasses
the substrate review gates (`crypto-reviewer`, `abac-reviewer`, `code-reviewer`).

The dual invariant is **INV-S2**: substrate (`internal/`) MUST stay domain-free.
No `internal/` package may contain entity types, event vocabularies, or domain
logic for scenes, channels, forums, discord, or any other specific use. INV-S1
keeps plugin work out of substrate; INV-S2 keeps domain knowledge out of
substrate.

For the full boundary definition and anti-patterns, see
[ADR holomush-z1e7 — Strict Plugin-Boundary: Plugins Must Not Modify internal/](https://github.com/holomush/holomush/blob/main/docs/adr/holomush-z1e7-strict-plugin-boundary.md).

## Manifest emit-type validation INV-S5

INV-S5 requires set-equality between your manifest's declared emit types and
your code's registered emit types. Mismatch fails plugin load with error code
`EVENT_TYPE_REGISTRY_MISMATCH`. This catches two failure modes that the
runtime emit gate misses:

- **Declared-but-unregistered:** a `crypto.emits` entry the code never emits
  (dead declaration or typo).
- **Registered-but-undeclared:** plugin code emitting a type the manifest never
  declared, silently as plaintext.

INV-S5 applies only to plugins with a non-empty `crypto.emits` block. Plugins
without `crypto.emits` skip the check entirely.

To satisfy this invariant in your plugin, follow
[Register plugin emit types](/extending/how-to/register-emit-types/).

## SDK-extraction policy INV-S7

`pkg/plugin/eventkit/` and `pkg/plugin/groupkit/` are co-designed in the
substrate-contract spec but their code lands **only after N=2 validation**
(INV-S7): two distinct use plugins must adopt a primitive cleanly before it is
extracted to substrate SDK code.

**Today:** plugins implement event-replay, group-membership, and focus-wire
patterns inline (scenes-bespoke, channels-bespoke).

**After N=2 validation:** the primitive is extracted to the appropriate package
(see the SDK table in
[Substrate Contract](/extending/reference/substrate-contract/#eventkit-and-groupkit-sdks-named-not-yet-built)).

Forums uses `eventkit` only. Discord defaults to no SDK. **INV-S10 forbids
forums and discord from importing `groupkit`.**

Relevant ADRs:

- [holomush-p7w0 — Split Plugin SDK into eventkit and groupkit by Scope](https://github.com/holomush/holomush/blob/main/docs/adr/holomush-p7w0-split-plugin-sdk-eventkit-groupkit.md)
- [holomush-lrt3 — Require N=2 Consumer Validation Before SDK Primitive Extraction](https://github.com/holomush/holomush/blob/main/docs/adr/holomush-lrt3-n2-consumer-validation-sdk-extraction.md)

## See also

- [Substrate Contract](/extending/reference/substrate-contract/) — the primitive inventory.
- [Register plugin emit types](/extending/how-to/register-emit-types/) — the INV-S5 procedure.
