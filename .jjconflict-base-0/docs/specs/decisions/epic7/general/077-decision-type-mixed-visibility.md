<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 77. Decision Type Mixed Visibility (Value Semantics Safety)

> [Back to Decision Index](../README.md)

**Question:** The `Decision` struct has `allowed` (unexported) and `Effect`
(exported). This appears inconsistent for invariant protection — someone could
mutate `d.Effect = EffectAllow` without updating `allowed`. Should `Effect` also
be unexported?

**Options Considered:**

1. **Make `Effect` unexported** — Add `GetEffect()` accessor. Fully consistent
   but adds boilerplate for a commonly-read field.
2. **Keep mixed visibility** — Rely on Go value semantics for safety.
3. **Make both exported** — Remove invariant enforcement entirely.

**Decision:** Option 2. Keep the current mixed visibility. Go value semantics
make this safe: `Decision` is returned by value (not pointer) from
`NewDecision()` and `Evaluate()`. Callers receive a copy — mutating `Effect` on
their copy does not affect the canonical instance. The `NewDecision()`
constructor enforces the `allowed`/`Effect` invariant at creation time.

**Rationale:** The `allowed` field is unexported because it is a _derived_
value — always computable from `Effect`. Making it unexported prevents callers
from constructing an inconsistent `Decision{allowed: true, Effect: EffectDeny}`.
`Effect` is exported because it is a primary field that callers read frequently
in switch statements. Adding a `GetEffect()` accessor would be Go anti-pattern
(unnecessary getter for a value type). A clarifying comment on the struct
documents this reasoning.

**Cross-reference:** Spec Decision struct (lines 415-440), `NewDecision()`
constructor. Bead: `holomush-5k1.293`.
