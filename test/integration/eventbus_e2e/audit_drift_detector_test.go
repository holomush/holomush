// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
)

// Audit drift detector specs — covers spec §8 "Audit drift detector ->
// Tampered row reported with id". The detector is not yet implemented, so
// this spec records the gap rather than asserting behaviour.
//
// What an implementation would have to prove:
//
//  1. Publish a canonical event and wait for it to be projected into
//     events_audit.
//  2. Tamper the row (e.g. set codec='not-a-real-codec' or corrupt the
//     payload).
//  3. Invoke the drift detector and assert the tampered row's id is
//     returned with a diagnostic reason naming the codec.
//
// Follow-up: https://github.com/holomush/holomush/issues/4853 — eventbus
// audit drift detector.
var _ = Describe("Audit drift detector reports tampered row", func() {
	It("detects tampered row and reports id with diagnostic reason", func() {
		Skip("#4853: eventbus audit drift detector not yet implemented")
	})
})
