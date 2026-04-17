// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRecordInvocationOutcomeSuccess(t *testing.T) {
	before := testutil.ToFloat64(InvocationsTotal.WithLabelValues("p1", "on_event", "success"))
	recordInvocationOutcome("p1", "on_event", outcomeSuccess)
	assert.Equal(t, before+1, testutil.ToFloat64(InvocationsTotal.WithLabelValues("p1", "on_event", "success")))
}

func TestRecordInvocationOutcomeTimeoutIncrementsBothCounters(t *testing.T) {
	beforeInv := testutil.ToFloat64(InvocationsTotal.WithLabelValues("p2", "on_event", "timeout"))
	beforeTo := testutil.ToFloat64(TimeoutsTotal.WithLabelValues("p2", "on_event"))
	recordInvocationOutcome("p2", "on_event", outcomeTimeout)
	assert.Equal(t, beforeInv+1, testutil.ToFloat64(InvocationsTotal.WithLabelValues("p2", "on_event", "timeout")))
	assert.Equal(t, beforeTo+1, testutil.ToFloat64(TimeoutsTotal.WithLabelValues("p2", "on_event")))
}

func TestRecordInvocationOutcomeRegistryFullIncrementsBothCounters(t *testing.T) {
	beforeInv := testutil.ToFloat64(InvocationsTotal.WithLabelValues("p3", "on_event", "registry_full"))
	beforeRf := testutil.ToFloat64(RegistryFullTotal.WithLabelValues("p3", "on_event"))
	recordInvocationOutcome("p3", "on_event", outcomeRegistryFull)
	assert.Equal(t, beforeInv+1, testutil.ToFloat64(InvocationsTotal.WithLabelValues("p3", "on_event", "registry_full")))
	assert.Equal(t, beforeRf+1, testutil.ToFloat64(RegistryFullTotal.WithLabelValues("p3", "on_event")))
}

func TestRecordInvocationOutcomeErrorOnlyIncrementsInvocations(t *testing.T) {
	beforeInv := testutil.ToFloat64(InvocationsTotal.WithLabelValues("p4", "on_event", "error"))
	beforeTo := testutil.ToFloat64(TimeoutsTotal.WithLabelValues("p4", "on_event"))
	beforeRf := testutil.ToFloat64(RegistryFullTotal.WithLabelValues("p4", "on_event"))
	recordInvocationOutcome("p4", "on_event", outcomeError)
	assert.Equal(t, beforeInv+1, testutil.ToFloat64(InvocationsTotal.WithLabelValues("p4", "on_event", "error")))
	assert.Equal(t, beforeTo, testutil.ToFloat64(TimeoutsTotal.WithLabelValues("p4", "on_event")))
	assert.Equal(t, beforeRf, testutil.ToFloat64(RegistryFullTotal.WithLabelValues("p4", "on_event")))
}
