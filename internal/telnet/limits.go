// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import "time"

// Limits bounds per-connection resource use. Zero values in a Limits value
// are NOT interpreted as "unlimited" — callers MUST populate the struct
// explicitly or use DefaultLimits.
type Limits struct {
	// IdleReadTimeout is the deadline refreshed on every Read from the
	// underlying connection. A drip-fed Slowloris attacker hits this
	// ceiling and is disconnected.
	IdleReadTimeout time.Duration

	// WriteTimeout bounds a single send. Applied via SetWriteDeadline
	// before every write. A stuck-client write returns immediately with
	// a timeout error.
	WriteTimeout time.Duration

	// PreAuthTimeout is the total wall-clock budget from connect to
	// successful character selection. Fires once; a fire after auth is
	// a no-op.
	PreAuthTimeout time.Duration
}

// DefaultLimits are the production-safe defaults for a modest VPS hosting
// a hobby-to-mid-size MUSH.
var DefaultLimits = Limits{
	IdleReadTimeout: 5 * time.Minute,
	WriteTimeout:    30 * time.Second,
	PreAuthTimeout:  2 * time.Minute,
}
