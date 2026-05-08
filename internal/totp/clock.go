// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package totp provides per-player TOTP enrollment, verification, and
// recovery for Phase 5 break-glass auth. PG-only side effects; audit
// emission is the calling layer's responsibility per spec §"Audit events
// emitted" / "Emission ownership and the host-shell-CLI gap".
package totp

import "time"

// Clock abstracts time.Now for testability. Avoids a third-party clock
// dependency to keep the package's go.mod surface small.
type Clock interface {
	Now() time.Time
}

// NewRealClock returns a Clock backed by time.Now.
func NewRealClock() Clock { return realClock{} }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// FakeClock is a test double — NOT goroutine-safe.
type FakeClock struct{ t time.Time }

// NewFakeClock returns a FakeClock starting at the given time.
func NewFakeClock(start time.Time) *FakeClock { return &FakeClock{t: start} }

// Now returns the current fake time.
func (c *FakeClock) Now() time.Time { return c.t }

// Advance moves the fake clock forward by d.
func (c *FakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }
