<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Confine the sentry-go Error-Event (CaptureException) Touchpoint to pkg/errutil

**Date:** 2026-05-23
**Status:** Accepted
**Decision:** holomush-1wbzn
**Deciders:** HoloMUSH Contributors

## Context

Sentry distinguishes two products: **Logs** (a searchable, trace-correlated
structured-log stream) and **Issues** (grouped, alertable error events with
stacktraces). The OTel-native log pipeline (ADR `holomush-ci829`) places
ERROR-level records into Sentry **Logs** as ordinary entries. It does **not**
create grouped Issues — those require `sentry.CaptureException`, a Sentry-SDK
call.

That call is the one place where error reporting must touch the vendor SDK,
which is in direct tension with the import-graph perimeter established by
INV-L1 (no sentry-go outside `internal/telemetry`). The question is a
trust-ownership decision: *where does the error-event SDK touchpoint live*,
and is it in scope for the log-surfacing work?

## Alternatives Considered

1. **Include `CaptureException` wiring in this work**, inline in
   `internal/telemetry` or scattered at error sites. Fewer PRs and ERROR logs
   immediately become Issues — but it spreads the sentry-go touchpoint and
   couples the error-reporting path to the log pipeline, making both harder to
   test and replace.
2. **Defer behind a future `pkg/errutil` wrapper (chosen).** Keeps the
   sentry-go error-event touchpoint in exactly one package; the boundary is
   explicit and testable; the log pipeline ships complete without blocking on
   the Issues story.

## Decision

Error-event capture (`sentry.CaptureException` → Sentry Issues) is **deferred
to a separate bead** and, when implemented, MUST live only behind a
`pkg/errutil` wrapper. `pkg/errutil` already owns the structured-error logging
seam (`LogError` / `LogErrorContext`); it becomes the single owner of any
sentry-go error-event API surface. No other package may import sentry-go for
error capture.

## Rationale

- Prevents the sentry-go import footprint from spreading beyond
  `internal/telemetry`; the INV-L1 perimeter stays clean and the future
  wrapper is the only additional permitted importer.
- `pkg/errutil` is the canonical seam for future SDK replacement or
  mock injection in tests.
- This work delivers a complete, shippable log pipeline; the Issues story is
  independently releasable.

## Consequences

**Positive:**

- The import-graph for error-event capture is defined *before* implementation
  begins, so the follow-up cannot accidentally scatter the SDK touchpoint.
- `pkg/errutil` is the single owner of any sentry-go error-event surface.

**Negative:**

- Operators see ERROR logs in the Sentry **Logs** stream but not as grouped
  **Issues** until the follow-up bead ships.

**Neutral:**

- The follow-up bead must import sentry-go only through `pkg/errutil`, which
  this decision mandates.

## References

- Spec: `docs/superpowers/specs/2026-05-23-otel-native-log-surfacing-design.md` §1 (Non-Goals), §12 (INV-L1)
- Related: holomush-ci829 (OTel-native binding / INV-L1), holomush-0wun (trace dual-export follow-ups)
