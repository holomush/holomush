// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestValidateCrypto(t *testing.T) {
	tests := []struct {
		name        string
		manifest    *plugins.Manifest
		wantErrCode string // empty = expect success
	}{
		{
			name: "accepts a manifest with valid emits and consumes",
			manifest: &plugins.Manifest{
				Name: "core-communication",
				Crypto: &plugins.CryptoSection{
					Emits: []plugins.CryptoEmit{
						{EventType: "whisper", Sensitivity: plugins.SensitivityAlways},
						{EventType: "say", Sensitivity: plugins.SensitivityNever},
					},
					Consumes: []plugins.CryptoConsume{{
						Subjects:           []string{"events.*.character.*.whisper"},
						RequestsDecryption: []string{"core-communication:whisper"},
					}},
				},
				Dependencies: map[string]string{}, // self-reference allowed
			},
		},
		{
			name:     "accepts a manifest with no crypto section",
			manifest: &plugins.Manifest{Name: "x"},
		},
		{
			// A plugin's consumes block MAY request decryption for its OWN
			// emitted event types without listing itself in dependencies.
			name: "accepts self-reference in consumes",
			manifest: &plugins.Manifest{
				Name: "core-communication",
				Crypto: &plugins.CryptoSection{
					Emits: []plugins.CryptoEmit{
						{EventType: "whisper", Sensitivity: plugins.SensitivityAlways},
					},
					Consumes: []plugins.CryptoConsume{{
						Subjects:           []string{"events.*.character.*.whisper"},
						RequestsDecryption: []string{"core-communication:whisper"},
					}},
				},
			},
		},
		{
			name: "rejects unknown sensitivity value",
			manifest: &plugins.Manifest{
				Name: "x",
				Crypto: &plugins.CryptoSection{
					Emits: []plugins.CryptoEmit{
						{EventType: "foo", Sensitivity: "kinda"},
					},
				},
			},
			wantErrCode: "PLUGIN_CRYPTO_INVALID_SENSITIVITY",
		},
		{
			name: "rejects whitespace-only event_type",
			manifest: &plugins.Manifest{
				Name: "x",
				Crypto: &plugins.CryptoSection{
					Emits: []plugins.CryptoEmit{
						{EventType: "   ", Sensitivity: plugins.SensitivityAlways},
					},
				},
			},
			wantErrCode: "PLUGIN_CRYPTO_EMPTY_EVENT_TYPE",
		},
		{
			name: "rejects duplicate emit event_type",
			manifest: &plugins.Manifest{
				Name: "x",
				Crypto: &plugins.CryptoSection{
					Emits: []plugins.CryptoEmit{
						{EventType: "foo", Sensitivity: plugins.SensitivityMay},
						{EventType: "foo", Sensitivity: plugins.SensitivityAlways},
					},
				},
			},
			wantErrCode: "PLUGIN_CRYPTO_DUPLICATE_EMIT",
		},
		{
			name: "rejects bare wildcard in requests_decryption",
			manifest: &plugins.Manifest{
				Name: "x",
				Crypto: &plugins.CryptoSection{
					Consumes: []plugins.CryptoConsume{{
						Subjects:           []string{"events.>"},
						RequestsDecryption: []string{"*"},
					}},
				},
			},
			wantErrCode: "PLUGIN_CRYPTO_WILDCARD_DECRYPT",
		},
		{
			// "core-communication:*" parses as a syntactically-qualified ref
			// but contains a NATS-style token wildcard — must be rejected.
			name: "rejects token-level wildcard in qualified requests_decryption",
			manifest: &plugins.Manifest{
				Name:         "consumer",
				Dependencies: map[string]string{"core-communication": ">= 1.0.0"},
				Crypto: &plugins.CryptoSection{
					Consumes: []plugins.CryptoConsume{{
						Subjects:           []string{"events.>"},
						RequestsDecryption: []string{"core-communication:*"},
					}},
				},
			},
			wantErrCode: "PLUGIN_CRYPTO_WILDCARD_DECRYPT",
		},
		{
			name: "rejects unqualified plugin reference in requests_decryption",
			manifest: &plugins.Manifest{
				Name: "x",
				Crypto: &plugins.CryptoSection{
					Consumes: []plugins.CryptoConsume{{
						Subjects:           []string{"events.>"},
						RequestsDecryption: []string{"whisper"}, // missing plugin: prefix
					}},
				},
			},
			wantErrCode: "PLUGIN_CRYPTO_UNQUALIFIED_REF",
		},
		{
			name: "rejects ref to plugin not in dependencies",
			manifest: &plugins.Manifest{
				Name:         "consumer",
				Dependencies: map[string]string{}, // declares no deps
				Crypto: &plugins.CryptoSection{
					Consumes: []plugins.CryptoConsume{{
						Subjects:           []string{"events.>"},
						RequestsDecryption: []string{"core-communication:whisper"},
					}},
				},
			},
			wantErrCode: "PLUGIN_CRYPTO_REF_NOT_REQUIRED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := plugins.ValidateCrypto(tt.manifest)
			if tt.wantErrCode == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, tt.wantErrCode)
		})
	}
}

// TestCryptoValidatorRejectsReadbackOnNeverType ensures that readback:true on a
// sensitivity:never emit is rejected with PLUGIN_CRYPTO_READBACK_ON_NEVER.
func TestCryptoValidatorRejectsReadbackOnNeverType(t *testing.T) {
	t.Parallel()
	err := plugins.ValidateCrypto(&plugins.Manifest{
		Name: "core-scenes",
		Crypto: &plugins.CryptoSection{Emits: []plugins.CryptoEmit{
			{EventType: "scene_join_ic", Sensitivity: plugins.SensitivityNever, Readback: true},
		}},
	})
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_READBACK_ON_NEVER")
}

// TestCryptoValidatorAllowsReadbackOnAlwaysType verifies that readback:true is
// accepted when paired with sensitivity:always.
func TestCryptoValidatorAllowsReadbackOnAlwaysType(t *testing.T) {
	t.Parallel()
	err := plugins.ValidateCrypto(&plugins.Manifest{
		Name: "core-scenes",
		Crypto: &plugins.CryptoSection{Emits: []plugins.CryptoEmit{
			{EventType: "scene_pose", Sensitivity: plugins.SensitivityAlways, Readback: true},
		}},
	})
	require.NoError(t, err)
}

// TestValidateCryptoNormalizesEventTypeForResolveLookup pins the round-trip
// between ValidateCrypto and ResolveCryptoRefs. ValidateCrypto MUST trim
// stored event_type values so that a self-reference like "p:whisper" resolves
// against an emit declared as " whisper " (with trailing whitespace).
// Without normalization the lookup at crypto_validator.go::ResolveCryptoRefs
// returns PLUGIN_CRYPTO_UNKNOWN_EVENT_REF for a legitimately declared event.
func TestValidateCryptoNormalizesEventTypeForResolveLookup(t *testing.T) {
	m := &plugins.Manifest{
		Name: "p",
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "  whisper  ", Sensitivity: plugins.SensitivityAlways},
			},
			Consumes: []plugins.CryptoConsume{{
				Subjects:           []string{"events.>"},
				RequestsDecryption: []string{"p:whisper"},
			}},
		},
	}
	require.NoError(t, plugins.ValidateCrypto(m))
	require.Equal(t, "whisper", m.Crypto.Emits[0].EventType,
		"ValidateCrypto must write back the trimmed event_type so ResolveCryptoRefs can match it")
	require.NoError(t, plugins.ResolveCryptoRefs(m, map[string][]plugins.CryptoEmit{}))
}
