// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package outbox

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/world/wmodel"
)

// SkipService clears a poison halt WITHOUT leaving a wire gap. It owns BOTH the
// leased store AND the JetStream publisher (round-3 blocker #3 — a postgres store
// has no publisher, so skip cannot be a store method). It acquires the fenced
// lease, validates the halted row, persists-or-reuses a STABLE skip-marker event
// id (round-4 A1), PUBLISHES an operator-authorized marker at the poison row's
// OWN feed_position (preserving INV-WORLD-3 gap-free order), and only AFTER
// PubAck marks the poison row resolved via the lease — never a silent
// published_at bump (round-2 finding).
type SkipService struct {
	store     OutboxStore
	publisher eventbus.Publisher
	gameID    string
	logger    *slog.Logger
}

// NewSkipService constructs a SkipService from the leased store, the JetStream
// publisher, and the game id. Both dependencies are required (round-3 blocker #3).
func NewSkipService(store OutboxStore, publisher eventbus.Publisher, gameID string, logger *slog.Logger) *SkipService {
	if logger == nil {
		logger = slog.Default()
	}
	return &SkipService{store: store, publisher: publisher, gameID: gameID, logger: logger}
}

// skipMarkerPayload is the marker's intent payload: it records WHICH poison row
// was skipped so a consumer/operator can correlate.
type skipMarkerPayload struct {
	SkippedEventID string `json:"skipped_event_id"`
	Reason         string `json:"reason"`
}

// Skip publishes the same-position skip marker for the poison row at position and
// resolves it. It is retry-idempotent (round-4 A1): a re-invocation after a crash
// between PubAck and MarkSkipResolved reuses the persisted stable marker id, so
// consumers observe EXACTLY ONE event at (game_id, epoch, feed_position).
func (s *SkipService) Skip(ctx context.Context, position int64) error {
	lease, err := s.store.AcquireLease(ctx, s.gameID)
	if err != nil {
		return oops.Code("WORLD_OUTBOX_SKIP_ACQUIRE_LEASE_FAILED").
			With("game_id", s.gameID).With("position", position).Wrap(err)
	}
	defer func() {
		if rerr := lease.Release(ctx); rerr != nil {
			s.logger.WarnContext(ctx, "outbox skip: lease release failed", "err", rerr)
		}
	}()

	// Validate the halted row: the poison row MUST be the lowest unpublished
	// position (the current feed blocker). Skipping any other position would open
	// a wire gap.
	env, err := lease.NextUnpublished(ctx)
	if err != nil {
		return oops.Code("WORLD_OUTBOX_SKIP_NEXT_FAILED").Wrap(err)
	}
	if env == nil {
		return oops.Code("WORLD_OUTBOX_SKIP_NO_POISON").
			With("game_id", s.gameID).With("position", position).
			Errorf("no unpublished row at position %d — nothing to skip", position)
	}
	if env.FeedPosition != position {
		return oops.Code("WORLD_OUTBOX_SKIP_POSITION_MISMATCH").
			With("game_id", s.gameID).With("requested_position", position).
			With("blocking_position", env.FeedPosition).
			Errorf("position %d is not the current feed blocker (%d); refusing to open a wire gap",
				position, env.FeedPosition)
	}

	// Establish the STABLE marker identity BEFORE publishing (round-4 A1).
	markerID, ok, err := lease.SkipMarkerID(ctx, position)
	if err != nil {
		return oops.Code("WORLD_OUTBOX_SKIP_MARKER_READ_FAILED").Wrap(err)
	}
	if !ok {
		markerID = core.NewULID()
		if perr := lease.PersistSkipMarkerID(ctx, position, markerID); perr != nil {
			return oops.Code("WORLD_OUTBOX_SKIP_MARKER_PERSIST_FAILED").Wrap(perr)
		}
	}

	marker, err := s.buildMarker(*env, markerID)
	if err != nil {
		return err
	}
	ev, err := EnvelopeToEvent(marker)
	if err != nil {
		return oops.Code("WORLD_OUTBOX_SKIP_MARKER_WIRE_FAILED").Wrap(err)
	}
	// Publish the marker (Nats-Msg-Id = the stable marker id). A retry republishes
	// the SAME id; JetStream dedup + the idempotent consumer collapse it to one.
	if perr := s.publisher.Publish(ctx, ev); perr != nil {
		return oops.Code("WORLD_OUTBOX_SKIP_PUBLISH_FAILED").
			With("position", position).With("marker_id", markerID.String()).Wrap(perr)
	}

	// Only AFTER PubAck: mark the poison row resolved so the relay resumes.
	if merr := lease.MarkSkipResolved(ctx, position); merr != nil {
		return oops.Code("WORLD_OUTBOX_SKIP_RESOLVE_FAILED").
			With("position", position).Wrap(merr)
	}
	relaySkipResolved.WithLabelValues(s.gameID).Inc()
	s.logger.WarnContext(ctx, "outbox relay poison row skipped by operator",
		"game_id", s.gameID, "feed_position", position, "epoch", env.Epoch,
		"skipped_event_id", env.EventID.String(), "marker_id", markerID.String())
	return nil
}

// buildMarker constructs the skip-marker envelope: the poison row's own
// feed_position/epoch/aggregate, a distinct SkipMarkerKind, the stable marker id,
// and a payload naming the skipped event.
func (s *SkipService) buildMarker(poison wmodel.Envelope, markerID ulid.ULID) (wmodel.Envelope, error) {
	payload, err := json.Marshal(skipMarkerPayload{
		SkippedEventID: poison.EventID.String(),
		Reason:         "operator-authorized poison skip",
	})
	if err != nil {
		return wmodel.Envelope{}, oops.Code("WORLD_OUTBOX_SKIP_PAYLOAD_FAILED").Wrap(err)
	}
	return wmodel.Envelope{
		EventID:       markerID,
		GameID:        poison.GameID,
		Kind:          SkipMarkerKind,
		SchemaVersion: 1,
		Actor:         "operator",
		AggregateType: poison.AggregateType,
		AggregateID:   poison.AggregateID,
		Epoch:         poison.Epoch,
		FeedPosition:  poison.FeedPosition,
		Payload:       payload,
	}, nil
}
