// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build ruleguard
// +build ruleguard

// Package gorules contains custom go-ruleguard rules loaded by gocritic.
//
// These rules enforce project invariants that cannot be expressed via
// standard linters. The file is build-tagged so it never compiles with
// the rest of the project — gocritic loads it via its ruleguard checker
// configured in .golangci.yaml.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// EventIDMustBeMonotonic ensures core.Event{} literals use core.NewULID()
// (monotonic-within-millisecond entropy), not idgen.New() (fresh random
// per call). Non-monotonic event IDs silently break PostgresEventStore.Replay
// (WHERE id > afterID ORDER BY id) and PostgresSessionStore cursor
// monotonicity. See internal/core/ulid.go for the invariant documentation.
func EventIDMustBeMonotonic(m dsl.Matcher) {
	m.Match(`core.Event{$*_, ID: idgen.New(), $*_}`).
		Report(`event IDs must use core.NewULID() (monotonic), not idgen.New() (random) — see internal/core/ulid.go`)
}

// ULIDMakeForbidden forbids ulid.Make() in production code. ulid.Make()
// uses math/rand internally, violating the project-wide crypto/rand rule.
// Use idgen.New() for entity IDs or core.NewULID() for event IDs. This
// rule replaces the bash check previously at Taskfile.yaml:346.
func ULIDMakeForbidden(m dsl.Matcher) {
	m.Match(`ulid.Make()`).
		Report(`use idgen.New() for entity IDs or core.NewULID() for event IDs; ulid.Make() uses math/rand`)
}
