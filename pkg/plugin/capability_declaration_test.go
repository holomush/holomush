// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

// focusOnlyProvider implements FocusClientAware (grants focus + stream.history).
type focusOnlyProvider struct{ ServiceProvider }

func (focusOnlyProvider) SetFocusClient(FocusClient) {}

// Verifies: INV-PLUGIN-54
func TestValidateDeclaredCapabilitiesFailsClosedOnUndeclared(t *testing.T) {
	// FocusClientAware grants focus + stream.history; declaring only focus must fail.
	err := validateDeclaredCapabilities(focusOnlyProvider{}, []string{"focus"})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CAPABILITY_NOT_DECLARED")
	assert.Contains(t, err.Error(), "stream.history")
}

// Verifies: INV-PLUGIN-54
func TestValidateDeclaredCapabilitiesPassesWhenAllDeclared(t *testing.T) {
	err := validateDeclaredCapabilities(focusOnlyProvider{}, []string{"focus", "stream.history"})
	require.NoError(t, err)
}

// emitOnlyProvider implements EventSinkAware (emit is exempt — no declaration needed).
type emitOnlyProvider struct{ ServiceProvider }

func (emitOnlyProvider) SetEventSink(EventSink) {}

// Verifies: INV-PLUGIN-54
func TestValidateDeclaredCapabilitiesExemptNeedsNoDeclaration(t *testing.T) {
	err := validateDeclaredCapabilities(emitOnlyProvider{}, nil)
	require.NoError(t, err, "emit is self-gated (exempt); needs no declaration")
}
