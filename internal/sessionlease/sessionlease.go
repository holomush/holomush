// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package sessionlease is the dependency-free home for the session-lease
// refresh interval, so gateway packages (internal/telnet, internal/web) can
// read it without importing internal/session (INV-EVENTBUS-1).
package sessionlease

import "time"

// DefaultRefreshInterval is the cadence at which a live gateway connection
// refreshes its server-side lease (CoreService.RefreshConnection → last_seen_at).
// Both the web StreamEvents heartbeat (internal/web) and the telnet lease-refresh
// ticker (internal/telnet) default to this value — this constant is their single
// source of truth.
//
// It is the floor the session reaper's LeaseTTL and BootGrace must clear: a
// healthy connection only touches last_seen_at once per interval, so a LeaseTTL
// at or below it can lapse between refreshes, and a BootGrace below it lets the
// post-restart sweep fire before any surviving gateway re-asserts (I-LIVE-4).
// parseSessionConfig (cmd/holomush) rejects lease/grace below 2× this value.
const DefaultRefreshInterval = 15 * time.Second
