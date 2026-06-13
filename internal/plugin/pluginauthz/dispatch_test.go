// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginauthz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

func TestDispatchContextRoundTrips(t *testing.T) {
	dc := pluginauthz.DispatchContext{Subject: "character:01ABC", Attributes: map[string]string{"location": "01LOC"}}
	ctx := pluginauthz.WithDispatch(context.Background(), dc)
	got, ok := pluginauthz.DispatchForHost(ctx)
	require.True(t, ok)
	assert.Equal(t, dc.Subject, got.Subject)
	assert.Equal(t, "01LOC", got.Attributes["location"])
}

func TestDispatchAbsentByDefault(t *testing.T) {
	_, ok := pluginauthz.DispatchForHost(context.Background())
	assert.False(t, ok, "absent dispatch context must be detectable for fail-closed")
}
