// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package cryptowiring holds the plugin-manifest-derived crypto/audit wiring
// shared by production boot (cmd/holomush) and the integration harness
// (internal/testsupport/integrationtest). Extracting these derivations keeps
// the harness faithful to prod's exact ownership/sensitivity routing.
package cryptowiring

import (
	"context"

	"github.com/holomush/holomush/internal/eventbus/codec"
)

// KeySelector returns a new identity codec.KeySelector. Callers MUST call this
// once and thread the SAME instance into both audit.PluginConsumerManager
// (WithKeySelector) and history.NewReader (WithCodecSelector): INV-P7-9 requires
// pointer-identity across the two sinks, which is the caller's responsibility,
// not a guarantee of this constructor (it allocates a fresh value per call).
func KeySelector() codec.KeySelector { return &identityKeySelector{} }

type identityKeySelector struct{}

func (identityKeySelector) SelectForEncrypt(_ context.Context, _ string) (codec.Name, codec.KeyLabel, error) {
	return codec.NameIdentity, "", nil
}

func (identityKeySelector) SelectForDecrypt(_ context.Context, _ codec.Name, _ codec.KeyID) (codec.Key, error) {
	return codec.NoKey, nil
}
