<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# ⚠️ Wrong template — please use the correct one for your PR type

Every PR must use a typed template. Using this default template is a reason for rejection.

Select the template that matches your PR:

| PR Type | When to use | Template link |
|---------|-------------|---------------|
| **Fix** | Correcting a bug, crash, or behavior that doesn't match documentation | [Use fix template](?template=fix.md) |
| **Enhancement** | Improving an existing feature — better output, expanded edge cases, performance | [Use enhancement template](?template=enhancement.md) |
| **Feature** | Adding something new — command, event type, plugin capability, RPC, or client surface | [Use feature template](?template=feature.md) |
| **Chore** | Internal maintenance — refactoring, test quality, CI/CD, tooling, tech debt | [Use chore template](?template=chore.md) |

---

## Not sure which type applies?

- If it **corrects broken behavior** → Fix
- If it **improves existing behavior** without adding new commands, events, or concepts → Enhancement
- If it **adds something that doesn't exist today** → Feature
- If it **changes no user-facing behavior at all** (refactor, test quality, CI, tooling) → Chore
- If you are not sure → open a [Discussion](https://github.com/holomush/holomush/discussions) first

---

### Reminder: issues must be approved before PRs

For **features**: the linked issue must have the `approved-feature` label before you open this PR.

For **enhancements**: the linked issue must have the `approved-enhancement` label before you open this PR.

For **fixes**: the linked issue must have the `confirmed-bug` label.

For **chores**: the linked issue must have the `approved-chore` label.

PRs that arrive without a linked, approved issue are closed without review.

> **No draft PRs.** Draft PRs are closed automatically. Only open a PR when your code is complete,
> `task pr-prep` is green, and the correct template is used. See [CONTRIBUTING.md](../CONTRIBUTING.md).

---

### Exemptions

Dependency, CI/tooling, and documentation-only PRs are exempt from the typed-template and
issue-first requirements — they are recognized from their changed file paths. Automated
dependency PRs (Renovate) are exempt by definition.

For any other cross-cutting PR that genuinely does not fit a typed template, paste the following
line into your PR body with a non-empty reason:

`<!-- pr-template-exempt: your reason here -->`

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the full process.
