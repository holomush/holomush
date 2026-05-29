// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import type { Attributes } from '@opentelemetry/api';

/**
 * Build the OpenTelemetry attribute bag for a `command.roundtrip` span.
 *
 * Kept pure (no `$env`/SDK imports) so the per-command telemetry can be
 * asserted in unit tests. `connection_id` is exactly the field that goes
 * empty in the holomush-dble7 scene-focus bug class; recording it — plus a
 * boolean `connection_id.present` witness — makes that failure visible on the
 * span. The witness lets a backend query distinguish "absent" from
 * "present-but-empty" without string comparison. (Unlike the host-side ABAC
 * `has_X` convention, which OMITS an absent key to avoid fail-open DSL
 * semantics, the value is emitted as `''` alongside the witness — span
 * attributes carry no such fail-open risk.)
 *
 * `command.name` is the first whitespace-delimited token (the verb), a
 * low-cardinality attribute suitable for grouping; `command.input` carries the
 * full input verbatim, preserving the pre-existing span attribute.
 */
export function commandRoundtripAttributes(
  command: string,
  connectionId: string,
): Attributes {
  // split(/\s+/, 1) on a trimmed string always yields a 1-element array
  // (`['']` for empty input), so [0] is always a string.
  const name = command.trim().split(/\s+/, 1)[0];
  return {
    'command.input': command,
    'command.name': name,
    'connection_id': connectionId,
    'connection_id.present': connectionId !== '',
  };
}
