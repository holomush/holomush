// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package source

import (
	"context"
	"errors"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// FallbackResolver implements SourceResolver with the INV-CRYPTO-22 hot→cold-tier
// fallback algorithm. On a DEK_NOT_FOUND or DEK_DESTROYED error from the hot
// tier, it attempts to locate the event in the cold tier and resolve the
// cold-tier DEK. A double miss returns ErrMetadataOnly; non-typed errors
// propagate unchanged.
type FallbackResolver struct {
	DEKManager dek.Manager
	ColdReader ColdTierLookup
	Metrics    *Metrics
	Logger     *slog.Logger
}

// NewFallbackResolver constructs a FallbackResolver.
func NewFallbackResolver(m dek.Manager, c ColdTierLookup, met *Metrics, l *slog.Logger) *FallbackResolver {
	return &FallbackResolver{DEKManager: m, ColdReader: c, Metrics: met, Logger: l}
}

// Resolve implements SourceResolver per spec §5.3 (INV-CRYPTO-22 algorithm).
//
// Decision tree:
//  1. Identity codec → return TierHot immediately (no DEK lookup).
//  2. Hot DEK resolves → return TierHot with the resolved key.
//  3. Hot DEK miss (DEK_NOT_FOUND / DEK_DESTROYED) → fall through to cold tier.
//  4. Non-typed hot error → propagate unchanged (transient DB failure etc.).
//  5. Cold lookup error → wrap with EVENTBUS_SOURCE_COLD_LOOKUP_FAILED.
//  6. Cold row not found → ErrMetadataOnly.
//  7. Cold DEK resolves → return TierColdFallback with the cold envelope + key.
//  8. Cold DEK also missing → ErrMetadataOnly.
func (r *FallbackResolver) Resolve(ctx context.Context, hot eventbus.Envelope) (ResolvedSource, error) {
	// Step 1: identity codec bypasses DEK resolution entirely.
	if hot.Codec() == codec.NameIdentity {
		return ResolvedSource{Envelope: hot, SourceTier: TierHot}, nil
	}

	// Step 2: attempt hot-tier DEK resolution.
	key, err := r.DEKManager.Resolve(ctx, hot.KeyID(), hot.KeyVersion())
	if err == nil {
		return ResolvedSource{
			Envelope:   hot,
			Key:        key,
			KeyID:      hot.KeyID(),
			KeyVersion: hot.KeyVersion(),
			SourceTier: TierHot,
		}, nil
	}

	// Step 3: non-typed errors propagate (transient DB failure, etc.).
	if !isDEKMissing(err) {
		return ResolvedSource{}, err //nolint:wrapcheck // pass-through for non-DEK-missing errors; callers inspect directly
	}

	// Hot DEK is confirmed missing or destroyed — increment counter and fall through.
	r.Metrics.HotDEKMiss.Inc()

	// Step 4: query the cold tier.
	coldEnv, found, lookupErr := r.ColdReader.LookupByID(ctx, hot.EventID())
	if lookupErr != nil {
		return ResolvedSource{}, oops.Code("EVENTBUS_SOURCE_COLD_LOOKUP_FAILED").Wrap(lookupErr)
	}
	if !found {
		r.Metrics.ColdDEKMiss.Inc()
		r.Logger.WarnContext(ctx, "event indecipherable: hot DEK destroyed, no cold-tier row",
			"event_id", hot.EventID().String(),
			"hot_dek_ref", uint64(hot.KeyID()))
		return ResolvedSource{}, ErrMetadataOnly
	}

	// Step 5: attempt to resolve the cold-tier DEK.
	// Mirror the hot-tier classification: typed DEK_NOT_FOUND / DEK_DESTROYED
	// → ErrMetadataOnly; any other error is a transient DB failure and MUST
	// propagate so callers can distinguish retryable from permanent misses.
	coldKey, err := r.DEKManager.Resolve(ctx, coldEnv.KeyID(), coldEnv.KeyVersion())
	if err != nil {
		if !isDEKMissing(err) {
			return ResolvedSource{}, oops.Code("DEK_RESOLVE_TRANSIENT").Wrap(err)
		}
		r.Metrics.ColdDEKMiss.Inc()
		return ResolvedSource{}, ErrMetadataOnly
	}

	// Both tiers resolved — return the cold-tier substituted envelope.
	r.Metrics.ColdFallbackSuccess.Inc()
	return ResolvedSource{
		Envelope:   coldEnv,
		Key:        coldKey,
		KeyID:      coldEnv.KeyID(),
		KeyVersion: coldEnv.KeyVersion(),
		SourceTier: TierColdFallback,
	}, nil
}

// isDEKMissing returns true for the typed DEK_NOT_FOUND / DEK_DESTROYED codes
// produced by dek.Manager.Resolve. Any other error kind is a transient failure
// and must propagate without triggering the cold-tier fallback.
func isDEKMissing(err error) bool {
	var oe oops.OopsError
	if !errors.As(err, &oe) {
		return false
	}
	code := oe.Code()
	return code == "DEK_NOT_FOUND" || code == "DEK_DESTROYED"
}
