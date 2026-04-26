// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestValidateCryptoAcceptsValidManifest(t *testing.T) {
	m := &plugins.Manifest{
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
	}
	require.NoError(t, plugins.ValidateCrypto(m))
}

func TestValidateCryptoRejectsUnknownSensitivity(t *testing.T) {
	m := &plugins.Manifest{
		Name: "x",
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "foo", Sensitivity: "kinda"},
			},
		},
	}
	err := plugins.ValidateCrypto(m)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_INVALID_SENSITIVITY")
}

func TestValidateCryptoRejectsDuplicateEmitEventType(t *testing.T) {
	m := &plugins.Manifest{
		Name: "x",
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "foo", Sensitivity: plugins.SensitivityMay},
				{EventType: "foo", Sensitivity: plugins.SensitivityAlways},
			},
		},
	}
	err := plugins.ValidateCrypto(m)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_DUPLICATE_EMIT")
}

func TestValidateCryptoRejectsWildcardInRequestsDecryption(t *testing.T) {
	m := &plugins.Manifest{
		Name: "x",
		Crypto: &plugins.CryptoSection{
			Consumes: []plugins.CryptoConsume{{
				Subjects:           []string{"events.>"},
				RequestsDecryption: []string{"*"},
			}},
		},
	}
	err := plugins.ValidateCrypto(m)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_WILDCARD_DECRYPT")
}

func TestValidateCryptoRejectsUnqualifiedRequestsDecryption(t *testing.T) {
	m := &plugins.Manifest{
		Name: "x",
		Crypto: &plugins.CryptoSection{
			Consumes: []plugins.CryptoConsume{{
				Subjects:           []string{"events.>"},
				RequestsDecryption: []string{"whisper"}, // missing plugin: prefix
			}},
		},
	}
	err := plugins.ValidateCrypto(m)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_UNQUALIFIED_REF")
}

func TestValidateCryptoRejectsRefToNonDependencyPlugin(t *testing.T) {
	m := &plugins.Manifest{
		Name:         "consumer",
		Dependencies: map[string]string{}, // declares no deps
		Crypto: &plugins.CryptoSection{
			Consumes: []plugins.CryptoConsume{{
				Subjects:           []string{"events.>"},
				RequestsDecryption: []string{"core-communication:whisper"}, // not in deps
			}},
		},
	}
	err := plugins.ValidateCrypto(m)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_REF_NOT_REQUIRED")
}

func TestValidateCryptoAcceptsSelfReference(t *testing.T) {
	// A plugin's consumes block MAY request decryption for its OWN emitted
	// event types without listing itself in dependencies (self-reference).
	m := &plugins.Manifest{
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
	}
	require.NoError(t, plugins.ValidateCrypto(m))
}

func TestValidateCryptoNilSectionIsAccepted(t *testing.T) {
	m := &plugins.Manifest{Name: "x"}
	assert.NoError(t, plugins.ValidateCrypto(m))
}

func TestValidateCryptoRejectsTokenLevelWildcardInRequestsDecryption(t *testing.T) {
	// "core-communication:*" parses as a syntactically-qualified ref but
	// contains a NATS-style token wildcard — must be rejected at the
	// validator, not silently accepted.
	m := &plugins.Manifest{
		Name:         "consumer",
		Dependencies: map[string]string{"core-communication": ">= 1.0.0"},
		Crypto: &plugins.CryptoSection{
			Consumes: []plugins.CryptoConsume{{
				Subjects:           []string{"events.>"},
				RequestsDecryption: []string{"core-communication:*"},
			}},
		},
	}
	err := plugins.ValidateCrypto(m)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_WILDCARD_DECRYPT")
}
