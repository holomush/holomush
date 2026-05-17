// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package privacy_test

import (
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
)

// I-PRIV-7 placeholder: no plugin currently declares history_scope: custom.
// The full scenario will exercise a plugin whose history_scope semantics
// diverge from grid/scene; until that plugin lands, the test is skipped
// to record the invariant requirement explicitly. Replace Skip with the
// real assertion when a custom-scope plugin adopts the field.
var _ = Describe("I-PRIV-7: plugin-owned history_scope semantics", func() {
	It("exercises a plugin that declared custom history_scope (placeholder)", func() {
		Skip("no plugin currently declares history_scope: custom — re-enable when a plugin adopts this field")
	})
})
