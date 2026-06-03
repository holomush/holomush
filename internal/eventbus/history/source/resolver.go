// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package source provides the SourceResolver abstraction that decouples the
// history dispatcher from hot/cold tier selection. SimpleResolver is the
// no-fallback binding; FallbackResolver (Task 11) implements INV-CRYPTO-22.
package source

import (
	"context"
	"errors"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
)

// Tier identifies which persistence layer a resolved event's envelope
// originates from.
type Tier string

const (
	// TierHot indicates the envelope came from the JetStream hot tier.
	TierHot Tier = "hot"
	// TierColdFallback indicates the envelope was retrieved from the
	// events_audit cold tier after the hot-tier DEK was missing or destroyed.
	TierColdFallback Tier = "cold_fallback"
)

// ErrMetadataOnly is returned by Resolve when both the hot-tier DEK and
// the cold-tier row are indecipherable. The caller MUST deliver a
// metadata-only event (MetadataOnly=true, Payload=nil).
var ErrMetadataOnly = errors.New("source: both tiers indecipherable; deliver metadata-only")

// ResolvedSource carries all inputs required by the dispatcher to decrypt
// and deliver an event after source resolution.
type ResolvedSource struct {
	Envelope   eventbus.Envelope
	Key        codec.Key
	KeyID      codec.KeyID
	KeyVersion uint32
	SourceTier Tier
}

// SourceResolver abstracts hot/cold tier selection from the dispatcher.
// Implementations MUST be safe for concurrent use.
type SourceResolver interface { //nolint:revive // matches plan-canonical name
	Resolve(ctx context.Context, hotEnvelope eventbus.Envelope) (ResolvedSource, error)
}

// ColdTierLookup is the narrow adapter the FallbackResolver depends on.
// Backed in production by cold_postgres.Reader.
type ColdTierLookup interface {
	LookupByID(ctx context.Context, eventID eventbus.EventID) (envelope eventbus.Envelope, found bool, err error)
}
