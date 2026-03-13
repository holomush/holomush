// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// TestMetrics_MetricsRegistered verifies all metric descriptors are registered.
func TestMetrics_MetricsRegistered(t *testing.T) {
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	registered := make(map[string]bool)
	for _, family := range families {
		registered[family.GetName()] = true
	}

	expectedMetrics := []string{
		"abac_evaluate_duration_seconds",
		"abac_policy_evaluations_total",
	}

	for _, name := range expectedMetrics {
		assert.True(t, registered[name], "metric %q should be registered", name)
	}
}

// TestMetrics_RecordEvaluationMetrics verifies the helper function increments counters.
func TestMetrics_RecordEvaluationMetrics(t *testing.T) {
	initialCount := testutil.ToFloat64(policyEvaluations.WithLabelValues("unknown", "allow"))

	RecordEvaluationMetrics(50*time.Millisecond, types.EffectAllow)

	newCount := testutil.ToFloat64(policyEvaluations.WithLabelValues("unknown", "allow"))
	assert.Equal(t, initialCount+1, newCount)
}

// TestMetrics_EvaluateDuration_Recorded verifies that engine.Evaluate() records metrics.
func TestMetrics_EvaluateDuration_Recorded(t *testing.T) {
	dslText := `permit(principal is character, action in ["read"], resource is location);`

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{})

	req := types.AccessRequest{
		Subject:  "character:char-123",
		Action:   "read",
		Resource: "location:loc-456",
	}
	_, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	count := testutil.CollectAndCount(evaluateDuration)
	assert.GreaterOrEqual(t, count, 1, "histogram should have at least one observation")
}

// TestMetrics_EffectLabels verifies different effects produce different counter labels.
func TestMetrics_EffectLabels(t *testing.T) {
	tests := []struct {
		name   string
		effect types.Effect
		label  string
	}{
		{"allow", types.EffectAllow, "allow"},
		{"deny", types.EffectDeny, "deny"},
		{"default_deny", types.EffectDefaultDeny, "default_deny"},
		{"system_bypass", types.EffectSystemBypass, "system_bypass"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initial := testutil.ToFloat64(policyEvaluations.WithLabelValues("unknown", tt.label))

			RecordEvaluationMetrics(10*time.Millisecond, tt.effect)

			updated := testutil.ToFloat64(policyEvaluations.WithLabelValues("unknown", tt.label))
			assert.Equal(t, initial+1, updated)
		})
	}
}
