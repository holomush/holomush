// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// TestINV_CRYPTO_63_BuiltinEventTypesRegistered asserts that
// crypto.system.operator_read and crypto.system.operator_read_completed are
// registered with the correct field values by registerBuiltinTypes (INV-CRYPTO-63).
// Both types MUST have Category="system", Format="audit",
// DisplayTarget=EVENT_CHANNEL_AUDIT_ONLY, Source="builtin".
func TestINV_CRYPTO_63_BuiltinEventTypesRegistered(t *testing.T) {
	r := NewVerbRegistry()
	require.NoError(t, registerBuiltinTypes(r, "test"))

	cases := []string{
		"crypto.system.operator_read",
		"crypto.system.operator_read_completed",
	}
	for _, eventType := range cases {
		t.Run(eventType, func(t *testing.T) {
			reg, ok := r.Lookup(eventType)
			require.True(t, ok, "event type %q must be registered", eventType)
			assert.Equal(t, "system", reg.Category,
				"operator_read builtin MUST have Category=system")
			assert.Equal(t, "audit", reg.Format,
				"operator_read builtin MUST have Format=audit")
			assert.Equal(t, corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY, reg.DisplayTarget,
				"operator_read builtin MUST have DisplayTarget=AUDIT_ONLY")
			assert.Equal(t, "builtin", reg.Source,
				"operator_read builtin MUST have Source=builtin")
		})
	}
}

// TestBuiltinRekeyRowUnchanged is a golden-test assertion that the existing
// crypto.system.rekey registration is not disturbed by the INV-CRYPTO-63 additions.
func TestBuiltinRekeyRowUnchanged(t *testing.T) {
	r := NewVerbRegistry()
	require.NoError(t, registerBuiltinTypes(r, "test"))

	reg, ok := r.Lookup("crypto.system.rekey")
	require.True(t, ok, "crypto.system.rekey must still be registered after INV-CRYPTO-63 additions")
	assert.Equal(t, "system", reg.Category)
	assert.Equal(t, "audit", reg.Format)
	assert.Equal(t, corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY, reg.DisplayTarget)
	assert.Equal(t, "builtin", reg.Source)
}
