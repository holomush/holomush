// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package wmodel

// GenesisSnapshotResult reports the outcome of a cutover genesis snapshot run: the
// epoch it ran at, how many genesis envelopes it newly emitted, and how many
// pre-existing aggregates it skipped because their durable
// world_genesis_checkpoint row already existed at this epoch (the idempotency
// path). It lives in the wmodel leaf so both the consumer-owned outbox.GenesisStore
// interface and the internal/world/postgres implementation share the type without
// forming an outbox <-> postgres import edge.
type GenesisSnapshotResult struct {
	// Epoch is the feed epoch the snapshot ran at.
	Epoch int64
	// Emitted is the number of genesis envelopes newly written this run.
	Emitted int
	// Skipped is the number of aggregates already checkpointed at this epoch
	// (idempotent no-ops that consumed no feed position).
	Skipped int
}

// EpochResetResult reports the outcome of a feed epoch advance / reset: the epoch
// transition, how many unpublished old-epoch outbox rows were quarantined (marked
// so the relay never publishes a stale-epoch position), and the origin position
// the counter restarted at. It lives in the wmodel leaf for the same
// cycle-neutrality reason as GenesisSnapshotResult.
type EpochResetResult struct {
	// PreviousEpoch is the epoch in effect before the advance.
	PreviousEpoch int64
	// NewEpoch is the epoch after the advance (PreviousEpoch + 1).
	NewEpoch int64
	// Quarantined is the count of unpublished old-epoch outbox rows marked so the
	// relay never publishes them under the new epoch.
	Quarantined int64
	// OriginPosition is the feed position the counter was reset to (the defined
	// origin), so positions restart cleanly at the new epoch.
	OriginPosition int64
}
