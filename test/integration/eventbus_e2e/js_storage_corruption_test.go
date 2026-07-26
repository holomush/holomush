// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
)

// JS storage corruption specs — covers spec §8 "Embedded JS storage
// corruption -> Rebuild from PG audit; ULIDs stable". The JS-rebuild tool is
// not yet implemented, so this spec records the gap rather than asserting.
//
// Preserved ULIDs is the load-bearing invariant: the PG audit row id MUST
// equal the original publish ULID, so a rebuild republishing via the
// Publisher with `Nats-Msg-Id = audit.id` lands back on the stream with the
// same seq-semantics AND the same ULID — so pinned ULID cursors survive.
//
// What an implementation would have to prove: publish N events and let them
// project into events_audit, simulate JS loss by purging the EVENTS stream,
// run the rebuild tool, then assert the new JS stream carries the same N
// ULIDs in the same order.
//
// Follow-up: https://github.com/holomush/holomush/issues/4855 — JS storage
// rebuild from PG audit. That issue records this work as declined twice;
// deleting this file is a legitimate resolution, but that call belongs to a
// maintainer closing the issue, not to a sweep.
var _ = Describe("JS storage corruption rebuild from PG audit preserves ULIDs", func() {
	It("rebuild from PG audit preserves original ULIDs in same order", func() {
		Skip("#4855: JS storage rebuild from PG audit tool not yet implemented")
	})
})
