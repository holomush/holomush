<!-- SPDX-License-Identifier: Apache-2.0 -->

# Security Policy

Thanks for helping keep HoloMUSH and its users safe.

## Reporting a vulnerability

**Please do not open a public issue for a security problem.**

Report it privately through GitHub:

1. Go to the [Security tab](https://github.com/holomush/holomush/security)
2. Click **Report a vulnerability**

This opens a private security advisory visible only to you and the maintainers.
Private vulnerability reporting is enabled on this repository, so the channel is
live — you do not need to request access or find an email address first.

If GitHub's private reporting is unavailable to you for any reason, open a
regular issue containing **no technical detail** — just ask a maintainer to open
a private channel — and we will take it from there.

### What to include

The more of this you can provide, the faster we can confirm and fix it:

- What the issue is, and what an attacker gains
- Steps to reproduce, or a proof of concept
- Affected version, commit SHA, or deployment (e.g. the public sandbox)
- Any configuration needed to trigger it

Partial reports are welcome. A vague suspicion reported privately is far more
useful to us than a detailed one reported publicly.

## Supported versions

HoloMUSH is pre-1.0 and under active development. Security fixes land on the
latest release and `main`; there are no backports to older tags.

| Version | Supported |
| ------- | --------- |
| Latest release + `main` | ✅ |
| Any earlier tag | ❌ — upgrade to the latest release |

We would rather state this plainly than imply a support window we do not
maintain.

## Scope

**In scope** — anything that lets an attacker read, alter, or destroy data they
should not reach, or take down a deployment:

- The core server, and the telnet and web gateways
- The plugin host and the Lua/binary plugin trust boundary — including any way a
  plugin escapes the limits its manifest declares
- Event-payload cryptography: key handling, the audit chain, and any path that
  discloses plaintext to a non-participant
- Access control (`internal/access`) — any bypass of the default-deny ABAC model,
  or a policy evaluated fail-open
- Authentication and session handling, including session fixation, hijacking, or
  privilege escalation across characters or players
- The public sandbox deployment at `game.holomush.dev`

**Out of scope:**

- Vulnerabilities in third-party dependencies — please report those upstream. If
  one materially affects HoloMUSH, tell us and we will prioritise the bump.
- Misconfiguration of a self-hosted instance that does not stem from an unsafe
  default we ship. If you believe the default itself is unsafe, that **is** in
  scope — say so.
- Findings from automated scanners with no demonstrated impact. A report we
  cannot reproduce or reason about is hard to act on.
- Social engineering of maintainers or players.

## What to expect

- **Acknowledgement.** We will confirm we received your report and say whether
  we have reproduced it.
- **Coordinated disclosure.** We will agree a disclosure timeline with you and
  publish a GitHub Security Advisory when a fix ships.
- **Credit.** You will be credited in the advisory unless you would rather not
  be. Tell us either way.

This is a small project, so we are not going to publish a response-time SLA we
might not honour. We will treat reports seriously and keep you informed of where
things stand rather than leaving you guessing.

## Safe harbour

We will not pursue or support action against anyone who makes a good-faith
effort to follow this policy while researching against the public sandbox at
`game.holomush.dev`. Good faith means:

- Do not access, modify, or delete data belonging to other players beyond the
  minimum needed to demonstrate the issue
- Do not degrade service for others — no denial-of-service testing, no load
  testing, no resource exhaustion
- Do not exfiltrate data. If you can read something you should not, stop and
  tell us what you could reach; you do not need to prove it by taking it.
- Give us reasonable time to fix the issue before disclosing it publicly

If you are unsure whether something is in bounds, ask first through the private
advisory channel.

## A note on scope of this file

This is the repository's **disclosure policy**. It is unrelated to the
per-phase `SECURITY.md` artifacts produced inside `.planning/` by the project's
development workflow, which are internal threat registers for a unit of work.
