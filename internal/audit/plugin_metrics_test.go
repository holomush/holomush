// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/audit"
)

func TestRecordPluginAuditFailureDoesNotPanic(t *testing.T) {
	// Smoke test — the counter is process-global; we just verify the
	// helper is callable without panic.
	assert.NotPanics(t, func() {
		audit.RecordPluginAuditFailure()
	})
}
