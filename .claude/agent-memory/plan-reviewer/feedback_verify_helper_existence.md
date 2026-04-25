---
name: Verify helper existence before approving "extend the existing test file" plans
description: HoloMUSH plans frequently say "extend X test file" but invent helpers/methods that don't exist on the target types
type: feedback
---

# Verify helper existence before approving "extend the existing test file" plans

When a plan says "extend the existing test file" or "reuse the existing helper":

**Why:** Two blocking findings on plan `2026-04-23-plugin-history-authz-plan.md` (P1 bug). Plan called `store.CreateScene` and `store.JoinScene` — neither exists on `*SceneStore`. `JoinScene` is on `*SceneServiceImpl`. The store-test helper is `newTestStore`, not the plan's invented `setupSceneStore`. Separately, plan prescribed Ginkgo `Describe`/`It` blocks for `test/integration/eventbus_e2e/plugin_audit_isolation_test.go`, which is a plain `testing.T` test with zero Ginkgo. Six load-bearing tests around invented suite fields and seed helpers.

**How to apply:**

1. For every method call the plan makes on an existing type (e.g. `store.X(...)`), grep: `rg "func \(.*SceneStore\) X" plugins/`. If zero hits, finding is blocking.
2. For every helper the plan references (e.g. `setupFoo`, `seedX`), grep the test directory: `rg "func setupFoo" path/to/tests/`. Zero hits = invented helper.
3. For every "extend the Ginkgo suite" instruction, verify the file actually has `var _ = Describe(...)`. Plain `func TestX(t *testing.T)` files are NOT Ginkgo suites.
4. Plans that hand-wave with "if any are not yet defined, add small wrappers" for 6+ helpers are deferring design to execution — that's the placeholder pattern the writing-plans skill forbids.
