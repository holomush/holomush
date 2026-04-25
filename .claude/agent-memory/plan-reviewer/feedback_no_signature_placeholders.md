---
name: Reject plans containing "COPY VERBATIM FROM existing code" placeholders for type signatures
description: Interface signatures and function parameter lists must be inlined; deferring them to "look it up at execution time" is a placeholder antipattern
type: feedback
---

# Reject plans containing "COPY VERBATIM FROM existing code" placeholders for type signatures

A plan is not allowed to leave interface method signatures or function parameter lists as `/* COPY VERBATIM FROM existing.go */` comments — even when the source is "right there." This is the placeholder antipattern the writing-plans skill explicitly bans.

**Why:** On plan `2026-04-23-plugin-history-authz-plan.md`, two literal `/* COPY PARAMS FROM *SceneAuditStore.queryLog */` placeholders were left in Task 9 — one in the interface declaration, one in the fake's method body. The plan acknowledged the dependency three times but never resolved it. An interface whose method signature has to be derived at execution time is a TODO, not an interface. Tests that instantiate the fake assume it satisfies the interface; if the signature guess is wrong, every test in the task breaks at compile time.

**How to apply:** When reviewing a plan, search for `/* COPY`, `TBD`, `fill in`, `verify against`, `match the existing signature`. Each occurrence in code-blocks is a blocking finding. The plan author has access to the same `Read` tool I do — the signature should be inlined, not deferred. Even when the plan says "this is verifiable today; nothing about the spec changes it," the reviewer's job is to flag it: the executor following the plan should not have to do the discovery.
