<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 34. Time-of-Day Attributes for Environment Provider

> [Back to Decision Index](../README.md)

**Review finding:** The original spec included only `env.maintenance_mode`
(since renamed to `env.maintenance` in the final spec) and `env.game_state`
as environment attributes. Time-based policy gating (e.g.,
restrict certain areas during night hours) was not possible.

**Decision:** Add `env.hour` (float64, 0-23), `env.minute` (float64, 0-59),
and `env.day_of_week` (string, e.g., `"monday"`) to the EnvironmentProvider.
These are numeric/string attributes resolved from `time.Now()` at evaluation
time, not duration-based.

**Rationale:** Time-of-day gating is the common use case for MUSH environments
(night-only areas, weekend events). Numeric hour/minute with string day_of_week
matches the DSL's existing comparison operators naturally — no new time type
needed.

**Note:** `game_state` was mentioned in the original spec but is not included
in the final EnvironmentProvider schema — HoloMUSH does not currently have a
game state management system. It MAY be added when that system is implemented.
