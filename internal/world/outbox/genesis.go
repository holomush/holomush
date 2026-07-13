// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package outbox

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world/wmodel"
)

// GenesisStore is the CONSUMER-OWNED storage seam for genesis snapshot emission
// and feed-epoch management (round-4 A3). It is declared HERE, in package outbox,
// and IMPLEMENTED by internal/world/postgres/genesis_store.go, which is INJECTED
// at the composition root — so internal/world/outbox does NOT import
// internal/world/postgres (the round-4 A3 forbidden edge that the round-3 direct
// wiring introduced; the 05-07 Task-4 eight-edge import guard fails CI if it
// re-forms). The writer SQL lives only in the postgres impl; this package reaches
// it exclusively through this interface, exactly like the OutboxStore/Lease seam.
type GenesisStore interface {
	// EmitGenesisSnapshot emits exactly one genesis envelope per existing aggregate
	// (location/exit/character/object) at the current feed epoch, each committed
	// atomically WITH its world_genesis_checkpoint row. A same-epoch re-run of an
	// already-checkpointed aggregate is skipped BEFORE any feed position is
	// allocated (no gap), so the operation is idempotent per (game, epoch); the
	// checkpoint PK survives outbox pruning. Returns the per-run tally.
	EmitGenesisSnapshot(ctx context.Context, gameID string) (wmodel.GenesisSnapshotResult, error)

	// CurrentEpoch returns the game's current persistent feed epoch.
	CurrentEpoch(ctx context.Context, gameID string) (int64, error)

	// AdvanceEpoch performs the ONE-LOCKED, COMPLETE feed epoch reset (round-6
	// Codex MEDIUM): under the per-game counter row lock it quarantines any
	// unpublished old-epoch outbox row (so the relay never publishes a stale-epoch
	// position), increments the epoch, resets next_position to the defined origin
	// (positions restart, never inherit the old counter), records reset metadata,
	// and fires the relay wakeup — all under the single lock. Returns the reset
	// outcome. For a DB restore/backfill, a subsequent EmitGenesisSnapshot then
	// re-emits at the new epoch (the checkpoint key includes epoch).
	AdvanceEpoch(ctx context.Context, gameID string) (wmodel.EpochResetResult, error)
}

// GenesisService is the thin orchestration over an injected GenesisStore — the
// consumer-owned counterpart to SkipService. It owns no SQL; it drives the store
// through the GenesisStore interface so package outbox never imports
// internal/world/postgres (round-4 A3). Its two operations back the real
// `holomush world genesis` / `world epoch-reset` operator entry points.
type GenesisService struct {
	store  GenesisStore
	gameID string
	logger *slog.Logger
}

// NewGenesisService constructs a GenesisService from the injected store, the game
// id, and a logger (defaulting to slog.Default when nil).
func NewGenesisService(store GenesisStore, gameID string, logger *slog.Logger) *GenesisService {
	if logger == nil {
		logger = slog.Default()
	}
	return &GenesisService{store: store, gameID: gameID, logger: logger}
}

// EmitSnapshot emits the cutover genesis snapshot for the game (idempotent,
// checkpoint-keyed) and logs the tally. It is the `holomush world genesis` entry.
func (s *GenesisService) EmitSnapshot(ctx context.Context) (wmodel.GenesisSnapshotResult, error) {
	res, err := s.store.EmitGenesisSnapshot(ctx, s.gameID)
	if err != nil {
		return wmodel.GenesisSnapshotResult{}, oops.Code("WORLD_GENESIS_SNAPSHOT_FAILED").
			With("game_id", s.gameID).Wrap(err)
	}
	s.logger.InfoContext(ctx, "world genesis snapshot emitted",
		"game_id", s.gameID, "epoch", res.Epoch, "emitted", res.Emitted, "skipped", res.Skipped)
	return res, nil
}

// ResetEpoch advances the persistent feed epoch for the game (the one-locked,
// complete reset) and logs the reset metadata. It is the `holomush world
// epoch-reset` entry, for restarting the feed cleanly after a DB restore/backfill.
func (s *GenesisService) ResetEpoch(ctx context.Context) (wmodel.EpochResetResult, error) {
	res, err := s.store.AdvanceEpoch(ctx, s.gameID)
	if err != nil {
		return wmodel.EpochResetResult{}, oops.Code("WORLD_EPOCH_RESET_FAILED").
			With("game_id", s.gameID).Wrap(err)
	}
	s.logger.WarnContext(ctx, "world feed epoch reset",
		"game_id", s.gameID,
		"previous_epoch", res.PreviousEpoch,
		"new_epoch", res.NewEpoch,
		"quarantined_rows", res.Quarantined,
		"origin_position", res.OriginPosition)
	return res, nil
}
