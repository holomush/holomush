// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
)

// Multi-protocol fan-out specs — covers spec §8 "Multi-protocol fan-out ->
// Telnet + web in same scene see same pose". The Go harness cannot stand up
// the telnet and web adapters without docker compose infrastructure, so this
// spec records the gap rather than asserting.
//
// The assertion shape is: two distinct protocol adapters subscribed to the
// same scene subject both receive the same pose event with the same ULID and
// stream seq. The JetStream invariant (all subscribers of a subject get the
// same seq in the same order) already backstops this; the spec exists to
// verify the protocol-translation layer introduces no dedup bugs on the way
// out — telnet seeing rendered text and web seeing the JSON envelope, both
// carrying the one published ULID.
//
// Follow-up: https://github.com/holomush/holomush/issues/4856 —
// multi-protocol fan-out e2e harness. That issue records this work as
// declined twice; deleting this file, or re-siting the coverage in the
// Playwright E2E suite where both surfaces are reachable, is a legitimate
// resolution — but that call belongs to a maintainer, not to a sweep.
var _ = Describe("Multi-protocol fan-out telnet and web see same pose", func() {
	It("telnet and web in same scene see same pose event", func() {
		Skip("#4856: telnet + web adapter harness not reachable from the Go integration suite")
	})
})
