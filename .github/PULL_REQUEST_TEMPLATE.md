<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Pick a PR template

Most PRs need a typed template. Pick the one that matches your change:

| PR Type | When to use | Template link |
|---------|-------------|---------------|
| **Fix** | Correcting a bug, crash, or behavior that doesn't match documentation | [Use fix template](?expand=1&template=fix.md) |
| **Enhancement** | Improving an existing feature — better output, expanded edge cases, performance | [Use enhancement template](?expand=1&template=enhancement.md) |
| **Feature** | Adding something new — command, event type, plugin capability, RPC, or client surface | [Use feature template](?expand=1&template=feature.md) |
| **Chore** | Internal maintenance — refactoring, test quality, tech debt | [Use chore template](?expand=1&template=chore.md) |

If you are reading this in the PR body you opened, you are on the default template. Pick
one above, or read on if your change is exempt.

---

## Not sure which type applies?

- If it **corrects broken behavior** → Fix
- If it **improves existing behavior** without adding new commands, events, or concepts → Enhancement
- If it **adds something that doesn't exist today** → Feature
- If it **changes no user-facing behavior at all** (refactor, test quality, tech debt) → Chore
- If you are not sure → open a [Discussion](https://github.com/holomush/holomush/discussions) first

---

## Reminder: issues must be approved before PRs

The linked issue needs the matching label before you open the PR — `confirmed-bug` for a
fix, `approved-enhancement`, `approved-feature`, or `approved-chore`. A PR that arrives
without a linked, approved issue is closed without review.

**No draft PRs.** Open a PR only when the code is complete, `task pr-prep` is green, and
the correct template is used.

---

## Exemptions

Three kinds of PR skip the typed template and the issue-first gate:

- **Documentation-only** — the diff is confined to `site/**`, `docs/**`, `**/*.md`,
  `.planning/**`, `.claude/{agents,commands,rules,agent-memory}/**`, `LICENSE`, or
  `LICENSE_HEADER`.
- **Dependency-only** — the diff is confined to dependency manifests and lockfiles, defined
  by file *shape* rather than by a fixed enumeration: `**/go.mod`, `**/go.sum`,
  `**/package.json`, `**/pnpm-lock.yaml`, `**/pnpm-workspace.yaml`, `**/bun.lock`,
  `**/uv.lock`, `compose*.yaml`, `Dockerfile`, or `**/Dockerfile`. The authoritative version
  of this list is `DEPENDENCY_ONLY_PATHS` in `Taskfile.yaml`. The rule is path-derived: a
  Renovate PR is exempt if and only if its diff stays inside those shapes — being a Renovate
  PR is not itself an exemption. A dependency PR that **also** carries regenerated code
  (`pkg/proto/**/*.pb.go`, `web/**/*_pb.ts`) is **not** exempt: the buf codegen bumps in
  `.github/renovate.json` set `automerge: false` precisely so a human runs `task proto` /
  `task web:generate` and commits the regenerated stubs, which are real source diffs.
- **Repo configuration-only** — the diff is confined to `.github/**`: workflows, composite
  actions, the issue and PR templates, and Renovate config. A `CODEOWNERS` change is **not**
  exempt.

`Taskfile.yaml` and `scripts/**` are **not** exempt, with one lockfile carve-out — they
define `task pr-prep` and the checks CI runs, so changing them changes the gate itself. The
carve-out: a **lockfile** under `scripts/` matching the dependency-only shapes above (for
example `scripts/uv.lock`) **is** exempt, because a lockfile is not a gate definition. Any
non-lockfile change under `scripts/` stays gated.

If your diff touches anything outside those paths, it is not exempt — you still need a
linked, approved issue.

**If your PR is exempt,** replace this text with a short summary of the change and why it
is exempt. You do not need a linked issue or a typed template.

### Cross-cutting PRs

For a PR where no typed template fits the *shape* of the change, add this line with a real
reason and use the closest template:

`<!-- pr-template-exempt: your reason here -->`

The marker explains a template mismatch only. It is informational, it does not waive the
issue-first gate, and a maintainer may reject the claim.

See [CONTRIBUTING.md](https://github.com/holomush/holomush/blob/main/CONTRIBUTING.md) for
the full process.
