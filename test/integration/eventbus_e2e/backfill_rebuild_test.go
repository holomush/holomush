// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
)

// Backfill rebuild specs — covers spec §8 "Backfill rebuild ->
// bin/holomush audit-backfill produces matching counts". The
// audit-backfill CLI subcommand does not exist yet, so this spec records
// the gap rather than asserting behaviour.
//
// What an implementation would have to prove: publish N events while the
// audit projection is NOT running (so events_audit stays empty while
// JetStream accumulates the stream), run `bin/holomush audit-backfill`,
// then assert the events_audit row count equals the JS stream LastSeq.
//
// Follow-up: https://github.com/holomush/holomush/issues/4854 — holomush
// audit-backfill CLI subcommand.
var _ = Describe("Audit backfill produces matching counts", func() {
	It("bin/holomush audit-backfill produces matching counts", func() {
		Skip("#4854: holomush audit-backfill CLI subcommand not yet implemented")
	})
})
