<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 92. Fatal Seed Bootstrap Failures

> [Back to Decision Index](../README.md)

**Question:** Should the server start in degraded mode if seed policy compilation
or installation fails during bootstrap?

**Context:** A single broken seed policy prevents the ABAC engine from initializing
correctly. The reviewer suggested adding a `--skip-seed-install` flag or degraded
startup mode to allow the server to start despite seed failures.

**Decision:** Seed bootstrap failures are **fatal**. The server MUST NOT start if
any seed policy fails to compile or install. No `--skip-seed-install` flag or
degraded startup mode will be implemented.

**Rationale:** A broken seed policy is a configuration error equivalent to a missing
database connection or invalid TLS certificate. Starting without correct access
control policies would leave the system in an undefined security state — either
denying all operations (default-deny) or potentially allowing unauthorized access
if the failure is partial. Fatal failure with a descriptive error message is the
safest and most debuggable behavior. Operators can fix the seed and restart.

**Cross-reference:** Review finding I5 (PR #69); T23 (Phase 7.4 — bootstrap
sequence).
