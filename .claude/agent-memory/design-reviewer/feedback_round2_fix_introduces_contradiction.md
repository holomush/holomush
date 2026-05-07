---
name: Round-2 fixes can introduce orthogonal contradictions
description: When R1 finding asks for a single sweeping rule, R2 author often applies it past the boundary where another section made a different considered choice
type: feedback
---

When round-1 surfaces a finding about a single subsystem ("publish-before-COMMIT
ordering for the bootstrap event") and the R2 author addresses it by writing
a sweeping general statement ("the same publish-before-COMMIT pattern applies
to all per-player events"), grep the rest of the spec for sections that already
declared a *different* ordering for one of those events.

**Why:** Specs accumulate considered decisions section-by-section. A localized
R1 fix applied as a generalization can flatten over a different decision made
elsewhere — without the author noticing the conflict. The pattern is structural,
not careless.

**How to apply:** When R2 introduces a generalization of the form "the same
[mechanism] applies to [enumeration]", grep each item in the enumeration
elsewhere in the spec for an explicit ordering / mechanism / contract claim.
If found, raise as a contradiction blocking finding even when both R1 #N and
R2 #N look correct in isolation.

Seen 2026-05-07 in event-payload-crypto-phase5-totp-substrate-design r2:
§"Verify mechanics" (lines 343-346) prescribes emit-after-COMMIT for
`crypto.totp_locked` (considered choice — lockout is a defensive signal
that should not be aborted by NATS hiccup). §"Bootstrap closure mechanism"
ghost-case rewrite (lines 558-559) generalizes "publish-before-COMMIT"
to include `verify-locked` events, contradicting the earlier paragraph.
