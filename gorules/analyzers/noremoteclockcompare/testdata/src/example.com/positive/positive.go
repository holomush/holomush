// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package positive

import "time"

type sample struct {
	PublishedAt     time.Time
	IssuedAt        time.Time
	StartedAt       time.Time
	LastHeartbeatAt time.Time
	LocalAt         time.Time
}

func badTimeSincePublished(s sample) time.Duration {
	return time.Since(s.PublishedAt) // want `INV-58:`
}

func badTimeSinceIssued(s sample) time.Duration {
	return time.Since(s.IssuedAt) // want `INV-58:`
}

func badNowSubStarted(s sample) time.Duration {
	now := time.Now()
	return now.Sub(s.StartedAt) // want `INV-58:`
}

func badRemoteSubLocal(s sample) time.Duration {
	now := time.Now()
	return s.LastHeartbeatAt.Sub(now) // want `INV-58:`
}

func badNowBefore(s sample) bool {
	now := time.Now()
	return now.Before(s.PublishedAt) // want `INV-58:`
}

func badRemoteAfter(s sample) bool {
	now := time.Now()
	return s.IssuedAt.After(now) // want `INV-58:`
}
