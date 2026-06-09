<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-kddop; do not edit manually; use `/adr update holomush-kddop` -->

# Require a provisioned KEK to boot; auto-generate keyfile, never the passphrase

**Date:** 2026-06-09
**Status:** Accepted
**Decision:** holomush-kddop
**Deciders:** Sean Brandt

## Context

The predecessor go-live spec (2026-06-08, §3.1) made a KEK-less deployment a supported degraded posture that MUST NOT fail to start. That means sensitive events (scene_pose/say/emit/ooc, comms page/whisper/pemit) can run in the clear with no operator signal, and skipping KEK provisioning is the path of least resistance. The KEK keyfile is sealed by a passphrase-derived (Argon2id) AEAD key; the at-rest security property comes from keeping the keyfile and the unlock passphrase separate.

## Decision

A server MUST refuse to boot unless it can obtain an unlock passphrase from one of: env `HOLOMUSH_KEK_PASSPHRASE`, file-ref `HOLOMUSH_KEK_PASSPHRASE_FILE`, or an interactive prompt (TTY only). If the configured keyfile is absent AND `--auto-gen-kek` is set, the server auto-generates and persists the sealed keyfile (never regenerating an existing one). The unlock passphrase is NEVER auto-generated or stored by the software.

## Rationale

- Removes the KEK-less degraded posture: crypto is unconditionally active in any production deployment.
- Auto-generating the keyfile when absent (never when present) makes first-start frictionless WITHOUT touching the security model — the passphrase still comes from the operator.
- The keyfile/passphrase separation IS the entire at-rest security property; auto-generating a passphrase and storing it beside the keyfile would defeat it.
- Reversing a merged decision is accepted as explicit blast radius (harness, E2E compose, dev stack, docs), mitigated by a single coordinated change.

## Alternatives Considered

- Keep the KEK-less degraded posture from the predecessor spec: rejected — sensitive events run in the clear with no signal; crypto becomes opt-in friction rather than a default guarantee.
- Auto-generate BOTH keyfile and passphrase and store both: rejected — destroys the at-rest property (a passphrase stored beside the keyfile it seals provides no protection if the keyfile path is compromised).

## Consequences

Positive: sensitive-event crypto is unconditionally active in every production deployment; the boot-refusal invariant is structurally testable via a boot-matrix unit test. Negative: integration harness, E2E compose, local dev stack, and operating docs all require KEK provisioning updates; supersedes the predecessor spec §3.1 KEK-less clause. Neutral: `--auto-gen-kek` follows the koanf-binding pattern of other boolean run flags.
