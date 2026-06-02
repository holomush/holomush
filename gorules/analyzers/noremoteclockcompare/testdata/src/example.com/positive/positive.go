// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package positive

import "time"

type sample struct {
	PublishedAt     time.Time
	IssuedAt        time.Time
	StartedAt       time.Time
	LastPublishedAt time.Time
	LocalAt         time.Time
}

func badTimeSincePublished(s sample) time.Duration {
	return time.Since(s.PublishedAt) // want `INV-CLUSTER-8:`
}

func badTimeSinceIssued(s sample) time.Duration {
	return time.Since(s.IssuedAt) // want `INV-CLUSTER-8:`
}

func badNowSubStarted(s sample) time.Duration {
	now := time.Now()
	return now.Sub(s.StartedAt) // want `INV-CLUSTER-8:`
}

func badRemoteSubLocal(s sample) time.Duration {
	now := time.Now()
	return s.LastPublishedAt.Sub(now) // want `INV-CLUSTER-8:`
}

func badNowBefore(s sample) bool {
	now := time.Now()
	return now.Before(s.PublishedAt) // want `INV-CLUSTER-8:`
}

func badRemoteAfter(s sample) bool {
	now := time.Now()
	return s.IssuedAt.After(now) // want `INV-CLUSTER-8:`
}

// Transformed-operand cases: the remote selector is hidden inside a
// time.Time-returning method call (.UTC(), .Add(...), etc.) but still
// feeds a forbidden comparison.
func badTimeSincePublishedUTC(s sample) time.Duration {
	return time.Since(s.PublishedAt.UTC()) // want `INV-CLUSTER-8:`
}

func badNowSubStartedAdd(s sample) time.Duration {
	now := time.Now()
	return now.Sub(s.StartedAt.Add(time.Second)) // want `INV-CLUSTER-8:`
}

func badParenWrapped(s sample) time.Duration {
	now := time.Now()
	return now.Sub((s.IssuedAt)) // want `INV-CLUSTER-8:`
}

// time.Until is symmetric to time.Since (now-t vs t-now); also forbidden
// when the argument is a remote-sourced time.Time selector.
func badTimeUntilPublished(s sample) time.Duration {
	return time.Until(s.PublishedAt) // want `INV-CLUSTER-8:`
}

// Go 1.20+ time.Time.Compare returns -1/0/+1 — same protocol-decision
// hazard as Before/After when one operand is remote-sourced.
func badNowCompareIssued(s sample) int {
	now := time.Now()
	return now.Compare(s.IssuedAt) // want `INV-CLUSTER-8:`
}

func badRemoteCompareNow(s sample) int {
	now := time.Now()
	return s.LastPublishedAt.Compare(now) // want `INV-CLUSTER-8:`
}
