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
// checks (e.g., INV-CLUSTER-3 duplicate-MemberID detection) MUST NOT fire.
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

// Custom type with same-named field but non-time.Time type. The
// analyzer's type-check (isTimeTimeType) MUST filter this out.
type fakeMember struct {
	PublishedAt int64 // intentionally NOT time.Time
}

// Custom type with same-named methods (Before/After/Sub) on a
// non-time.Time receiver. The analyzer's isTimeMethod check MUST
// filter these out — they are not the time.Time methods INV-CLUSTER-8
// guards against.
type customOrdered struct {
	IssuedAt int64
}

func (c customOrdered) Before(other customOrdered) bool { return c.IssuedAt < other.IssuedAt }
func (c customOrdered) After(other customOrdered) bool  { return c.IssuedAt > other.IssuedAt }
func (c customOrdered) Compare(other customOrdered) int {
	switch {
	case c.IssuedAt < other.IssuedAt:
		return -1
	case c.IssuedAt > other.IssuedAt:
		return 1
	default:
		return 0
	}
}

func okCustomOrderedMethod(a, b customOrdered) bool {
	return a.Before(b)
}

func okCustomOrderedAfter(a, b customOrdered) bool {
	return a.After(b)
}

// Compare on a non-time.Time receiver MUST NOT fire — same isTimeMethod
// guard that filters Before/After also filters Compare.
func okCustomOrderedCompare(a, b customOrdered) int {
	return a.Compare(b)
}

// Reference fakeMember so the negative fixture compiles.
var _ = fakeMember{}

// Note: nolint directives are honored by golangci-lint's nolintlint
// layer, not by the analyzer itself, so the carved-out call sites in
// internal/cluster/heartbeat.go and internal/eventbus/crypto/invalidation/
// coordinator.go are silenced at the linter integration level. The
// positive fixture proves the analyzer fires; live lint runs prove the
// nolint annotations work end-to-end.
