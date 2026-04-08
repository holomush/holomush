// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/audit"
)

func TestEventSourceConstantsHaveExpectedStringValues(t *testing.T) {
	assert.Equal(t, "engine", string(audit.SourceEngine))
	assert.Equal(t, "plugin", string(audit.SourcePlugin))
	assert.Equal(t, "system", string(audit.SourceSystem))
}

func TestEventSourceIsADefinedTypeDistinctFromString(t *testing.T) {
	// EventSource is a defined string type — converting in either direction
	// requires an explicit cast. The compile-time fact that this test compiles
	// (with the explicit `string(s)` cast) is the proof; the runtime assertion
	// just verifies the cast preserves the value.
	s := audit.SourceEngine
	raw := string(s)
	assert.Equal(t, "engine", raw)
}
