// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package source

import (
	"context"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// SimpleResolver is the no-fallback binding. Resolve errors propagate
// unchanged. Used by test code and by paths that explicitly want fail-
// closed behavior (e.g., emit-time fence).
type SimpleResolver struct {
	DEKManager dek.Manager
}

// NewSimpleResolver constructs a SimpleResolver backed by m.
func NewSimpleResolver(m dek.Manager) *SimpleResolver {
	return &SimpleResolver{DEKManager: m}
}

// Resolve implements SourceResolver. Identity-codec envelopes bypass DEK
// resolution and return TierHot immediately. For all other codecs, Resolve
// delegates to DEKManager.Resolve and propagates any error to the caller.
func (r *SimpleResolver) Resolve(ctx context.Context, env eventbus.Envelope) (ResolvedSource, error) {
	if env.Codec() == codec.NameIdentity {
		return ResolvedSource{Envelope: env, SourceTier: TierHot}, nil
	}
	key, err := r.DEKManager.Resolve(ctx, env.KeyID(), env.KeyVersion())
	if err != nil {
		return ResolvedSource{}, err //nolint:wrapcheck // SimpleResolver is a pass-through; callers inspect the dek.Manager error directly
	}
	return ResolvedSource{
		Envelope:   env,
		Key:        key,
		KeyID:      env.KeyID(),
		KeyVersion: env.KeyVersion(),
		SourceTier: TierHot,
	}, nil
}
