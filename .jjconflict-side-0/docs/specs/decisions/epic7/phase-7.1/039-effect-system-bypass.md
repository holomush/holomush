<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 39. `EffectSystemBypass` as Fourth Effect Variant

> [Back to Decision Index](../README.md)

**Review finding (C2):** System subject bypass was handled by early return
before `Evaluate()` reached conflict resolution. This meant bypass decisions
were invisible to the type system and audit logging — callers couldn't
distinguish "no policy matched (default deny)" from "system bypassed all
policies."

**Decision:** Add `EffectSystemBypass` as a fourth variant in the `Effect`
enum:

```go
const (
    EffectDefaultDeny   Effect = iota // No policy matched
    EffectAllow
    EffectDeny
    EffectSystemBypass                // System subject bypass (audit-only)
)
```

**Rationale:** Making bypass explicit in the type system means audit logging,
metrics, and callers can distinguish all four outcomes. The `all` audit mode
logs bypass events, providing a complete trail of system-level operations.

**Related:** [ADR #4](../general/004-conflict-resolution.md) defines the
deny-overrides conflict resolution that `EffectSystemBypass` short-circuits
around.
