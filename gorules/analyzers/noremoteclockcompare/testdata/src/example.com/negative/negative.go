// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package negative

import "time"

type sample struct {
	PublishedAt time.Time
	IssuedAt    time.Time
	StartedAt   time.Time
	LocalAt     time.Time
}

// Local-only field comparisons MUST NOT fire.
func okLocalSub(a, b time.Time) time.Duration {
	return a.Sub(b)
}

// Local-named selector field MUST NOT fire (LocalAt is not on the allowlist).
func okLocalSelectorSub(s sample) time.Duration {
	now := time.Now()
	return now.Sub(s.LocalAt)
}

// IsZero / Equal are not in timeMethodAllowlist; remote-remote identity
// checks (e.g., INV-53 duplicate-MemberID detection) MUST NOT fire.
func okRemoteEqual(a, b sample) bool {
	if a.StartedAt.IsZero() {
		return false
	}
	return a.StartedAt.Equal(b.StartedAt)
}

// time.Since against a non-allowlisted selector MUST NOT fire.
func okTimeSinceLocal(s sample) time.Duration {
	return time.Since(s.LocalAt)
}

// Note: nolint directives are honored by golangci-lint's nolintlint
// layer, not by the analyzer itself, so the carved-out call sites in
// internal/cluster/heartbeat.go and internal/eventbus/crypto/invalidation/
// coordinator.go are silenced at the linter integration level. The
// positive fixture proves the analyzer fires; live lint runs prove the
// nolint annotations work end-to-end.
