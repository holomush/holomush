# ADR 0011: Deny-Overrides Without Priority Ordering

**Date:** 2026-02-05
**Status:** Accepted
**Deciders:** HoloMUSH Contributors

## Context

When multiple policies apply to the same access request, their effects may conflict — one
policy may `permit` while another may `forbid`. The engine needs a deterministic strategy
for combining these conflicting decisions into a single outcome.

Additionally, game administrators may want fine-grained control over which policies "win."
In complex systems, this is often handled by assigning numeric priorities to policies, where
higher-priority policies override lower-priority ones. The question is whether HoloMUSH
should support priority-based ordering.

### Options Considered

**Option A: Deny-overrides without priority**

Any `forbid` policy that matches and whose conditions are satisfied produces a deny. Any
`permit` policy that matches produces an allow. If both exist, deny wins. If neither
matches, the default is deny.

| Aspect     | Assessment                                                                                                              |
| ---------- | ----------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Simple mental model; Cedar-proven approach; policy creation order irrelevant; no "why was my priority wrong?" debugging |
| Weaknesses | Cannot express "this allow should override that deny" without rewriting conditions                                      |

**Option B: Priority-based ordering**

Each policy has a numeric priority. Higher-priority policies override lower-priority ones.

| Aspect     | Assessment                                                                                                                                                 |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | More flexible; can express VIP overrides; familiar from firewall rules                                                                                     |
| Weaknesses | "Why was I denied?" requires understanding priority across all policies; priority conflicts between independently authored policies; escalation arms races |

**Option C: Specificity-based resolution (CSS-like)**

More specific policies override less specific ones, determined by the specificity of
target and condition clauses.

| Aspect     | Assessment                                                                                                                                       |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| Strengths  | Intuitive — more specific rules win                                                                                                              |
| Weaknesses | "Specificity" is hard to define for arbitrary conditions; CSS specificity is notoriously confusing; adds significant complexity to the evaluator |

## Decision

**Option A: Deny-overrides without priority.**

The evaluation algorithm is:

1. Evaluate all candidate policies (no short-circuit — all matches recorded)
2. If any satisfied `forbid` policy exists → **deny** (report which forbid)
3. Else if any satisfied `permit` policy exists → **allow** (report which permit)
4. Else → **default deny** (no policy matched)

The `system` subject bypasses this entirely (always allowed, step 0).

Policy creation order, storage order, and evaluation order are all irrelevant to the
outcome. The same set of policies always produces the same decision regardless of when
they were created or in what order they are evaluated.

## Rationale

**Cedar validates this model:** Cedar chose deny-overrides without priority and it scales
to production systems at AWS. The model works because administrators control specificity
through conditions, not through priority escalation. If a deny is too broad, the admin
narrows its conditions rather than creating a higher-priority allow.

**Debugging simplicity:** With priority-based ordering, answering "why was I denied?"
requires examining all policies, their priorities, and the resolution order. With
deny-overrides, the answer is always: "because this `forbid` policy matched." The admin
can then inspect the forbid's conditions to understand why.

**Safe concurrent authoring:** Multiple admins can create policies independently without
worrying about priority collisions. In priority-based systems, two admins creating
policies at priority 100 creates an ambiguous conflict. With deny-overrides, there is
no such conflict.

**Player lock safety:** The three-layer access model (metadata → locks → full policies)
relies on admin `forbid` policies always trumping player `permit` locks. Priority-based
ordering would require players to understand that their locks have lower priority — an
unnecessary cognitive burden. With deny-overrides, the rule is simple: if an admin says
no, the answer is no.

### How to Handle "Override" Scenarios

When an admin needs to allow access that a `forbid` blocks, the correct approach is to
narrow the `forbid`'s conditions, not to escalate priority.

**Comparison table for admins from priority-based systems:**

| Approach                  | Policy Example                                                                                                                                                           | Outcome                                                                |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------- |
| **Wrong: priority-based** | Keep broad forbid: `forbid when { principal.level < 5 }` <br>Add higher-priority permit: `permit(priority=100) when { principal.flags.containsAny(["vip"]) }`            | **Not supported** — deny-overrides means the forbid always wins        |
| **Correct: narrowing**    | Narrow the forbid to exclude VIPs: `forbid when { principal.level < 5 && !principal.flags.containsAny(["vip"]) }` <br>No separate permit needed — base permit handles it | VIPs under level 5 are no longer blocked; default permit grants access |

**Before (blocks all characters under level 5):**

```text
forbid(principal is character, action in ["enter"], resource is location)
when { resource.restricted && principal.level < 5 };
```

**After (exempts VIPs by narrowing the forbid condition):**

```text
forbid(principal is character, action in ["enter"], resource is location)
when { resource.restricted && principal.level < 5
    && !principal.flags.containsAny(["vip"]) };
```

**Key insight:** Don't think "add a higher-priority permit to override the deny." Instead,
think "narrow the deny condition itself so the unwanted cases no longer match."

## Consequences

**Positive:**

- Simple mental model: deny always wins, no exceptions
- Policy creation order is irrelevant — safer for concurrent authoring
- No priority escalation arms races between administrators
- `policy test` output is straightforward — shows which forbid blocked access
- Player locks are automatically subordinate to admin forbids (by design)

**Negative:**

- Cannot express "this allow overrides that deny" without modifying the deny
- Overly broad `forbid` policies must be narrowed rather than overridden
- Admins accustomed to firewall-style priority ordering face a learning curve

**Neutral:**

- The `policy test --verbose` command helps admins understand which policies matched
- Documentation should include the "narrowing forbids" pattern prominently
- This matches Cedar's behavior exactly, so Cedar documentation applies

## References

- [Full ABAC Architecture Design — Evaluation Algorithm](../specs/2026-02-05-full-abac-design.md)
- [Design Decision #4: Conflict Resolution](../specs/decisions/epic7/general/004-conflict-resolution.md)
- [Cedar Language Specification — Policy Evaluation](https://docs.cedarpolicy.com/)
