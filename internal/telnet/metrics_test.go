// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRecordConnectionActiveTracksDelta(t *testing.T) {
	before := testutil.ToFloat64(ConnectionsActive)
	IncConnectionsActive()
	assert.Equal(t, before+1, testutil.ToFloat64(ConnectionsActive))
	DecConnectionsActive()
	assert.Equal(t, before, testutil.ToFloat64(ConnectionsActive))
}

func TestRecordConnectionRefusedIncrements(t *testing.T) {
	before := testutil.ToFloat64(ConnectionsRefusedTotal)
	RecordConnectionRefused()
	assert.Equal(t, before+1, testutil.ToFloat64(ConnectionsRefusedTotal))
}

func TestRecordPreAuthTimeoutIncrements(t *testing.T) {
	before := testutil.ToFloat64(PreAuthTimeoutsTotal)
	RecordPreAuthTimeout()
	assert.Equal(t, before+1, testutil.ToFloat64(PreAuthTimeoutsTotal))
}

func TestRecordIdleTimeoutIncrements(t *testing.T) {
	before := testutil.ToFloat64(IdleTimeoutsTotal)
	RecordIdleTimeout()
	assert.Equal(t, before+1, testutil.ToFloat64(IdleTimeoutsTotal))
}
