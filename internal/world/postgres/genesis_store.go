// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/wmodel"
)

// genesisFeedOrigin is the defined origin feed_position the counter restarts at
// after an epoch advance (world_feed_counter.next_position seeds at 1).
const genesisFeedOrigin = 1

// Genesis envelope kinds. These literals MUST match the taxonomy-declared kind
// strings in internal/world/outbox/taxonomy.go exactly. They are local constants
// (not imported) because internal/world/postgres MUST NOT import
// internal/world/outbox (the writer-boundary edge; the eight-edge import guard) —
// the same local-mirror pattern internal/world/service.go and the character-genesis
// service use. A cutover snapshot emits each aggregate's CREATE kind; for
// characters that is the character_genesis kind (its cutover-snapshot site — the
// per-CREATE genesis site is the 05-15 application service).
const (
	genesisKindLocation  = "location_created"
	genesisKindExit      = "exit_created"
	genesisKindObject    = "object_created"
	genesisKindCharacter = "character_genesis"
	genesisSchemaVersion = 1
	// genesisActor is the actor stamped on a cutover genesis envelope: the snapshot
	// is a system-initiated bootstrap of pre-existing state.
	genesisActor = "system"
)

// GenesisStore emits the cutover genesis snapshot (one envelope per existing
// aggregate, checkpoint-idempotent) and manages the persistent per-game feed epoch
// (CurrentEpoch / AdvanceEpoch). It is the writer-boundary concrete implementation
// of the consumer-owned outbox.GenesisStore interface; the composition root
// injects it so package outbox never imports package postgres (round-4 A3). All
// genesis/epoch SQL lives here.
type GenesisStore struct {
	pool *pgxpool.Pool
}

// NewGenesisStore constructs a GenesisStore backed by the given pool.
func NewGenesisStore(pool *pgxpool.Pool) *GenesisStore {
	return &GenesisStore{pool: pool}
}

// genesisLocationPayload is the new-values-only cutover payload for a location
// (matches the taxonomy location payload shape).
type genesisLocationPayload struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// genesisExitPayload is the new-values-only cutover payload for an exit.
type genesisExitPayload struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	FromLocationID string `json:"from_location_id"`
	ToLocationID   string `json:"to_location_id"`
}

// genesisObjectPayload is the new-values-only cutover payload for an object.
type genesisObjectPayload struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// genesisCharacterPayload is the new-values-only cutover payload for a character
// (matches the taxonomy character_genesis payload shape).
type genesisCharacterPayload struct {
	CharacterID string  `json:"character_id"`
	PlayerID    string  `json:"player_id"`
	Name        string  `json:"name"`
	LocationID  *string `json:"location_id,omitempty"`
}

// genesisAggregate is one pre-existing aggregate to snapshot: its type, id, the
// taxonomy kind its genesis envelope carries, its committed version, and its
// new-values-only payload bytes.
type genesisAggregate struct {
	aggType wmodel.AggregateType
	id      ulid.ULID
	kind    string
	version int
	payload []byte
}

// EmitGenesisSnapshot emits one genesis envelope per existing location/exit/
// character/object at the current epoch, each atomic with its
// world_genesis_checkpoint row. A same-epoch re-run of an already-checkpointed
// aggregate is skipped before allocating a position. Implements
// outbox.GenesisStore.
func (g *GenesisStore) EmitGenesisSnapshot(ctx context.Context, gameID string) (wmodel.GenesisSnapshotResult, error) {
	epoch, err := g.CurrentEpoch(ctx, gameID)
	if err != nil {
		return wmodel.GenesisSnapshotResult{}, err
	}

	aggregates, err := g.enumerateAggregates(ctx)
	if err != nil {
		return wmodel.GenesisSnapshotResult{}, err
	}

	res := wmodel.GenesisSnapshotResult{Epoch: epoch}
	for _, a := range aggregates {
		emitted, emitEpoch, err := g.emitOneGenesis(ctx, gameID, a)
		if err != nil {
			return wmodel.GenesisSnapshotResult{}, err
		}
		res.Epoch = emitEpoch
		if emitted {
			res.Emitted++
		} else {
			res.Skipped++
		}
	}
	return res, nil
}

// emitOneGenesis emits (or idempotently skips) the genesis envelope for one
// aggregate in ONE transaction. Under the per-game counter FOR UPDATE lock (which
// serializes all envelope writers for the game) it inserts the checkpoint row; ONLY
// when the checkpoint insert actually inserted (no PK conflict) does it allocate a
// position and write the envelope — so a same-epoch re-run conflicts on the
// checkpoint PK and skips BEFORE consuming a position (no gap), and a concurrent
// double-run serializes on the lock and the loser sees the PK conflict. The
// checkpoint table is never pruned, so the identity survives outbox pruning.
func (g *GenesisStore) emitOneGenesis(ctx context.Context, gameID string, a genesisAggregate) (emitted bool, epoch int64, err error) {
	intent := wmodel.NewEnvelopeIntent(wmodel.IntentParams{
		GameID:        gameID,
		Kind:          a.kind,
		SchemaVersion: genesisSchemaVersion,
		Actor:         genesisActor,
		AggregateType: a.aggType,
		AggregateID:   a.id,
		Payload:       a.payload,
	})

	txErr := withTx(ctx, g.pool, func(txCtx context.Context) error {
		e := execerFromCtx(txCtx, g.pool)
		q := querierFromCtx(txCtx, g.pool)

		if _, serr := e.Exec(txCtx, `SET LOCAL lock_timeout = '`+feedCounterLockTimeout+`'`); serr != nil {
			return oops.With("operation", "genesis set lock_timeout").With("game_id", gameID).Wrap(serr)
		}
		if _, ierr := e.Exec(txCtx,
			`INSERT INTO world_feed_counter (game_id, next_position, epoch)
			 VALUES ($1, 1, 1) ON CONFLICT (game_id) DO NOTHING`, gameID); ierr != nil {
			return oops.With("operation", "genesis counter init").With("game_id", gameID).Wrap(ierr)
		}

		var position int64
		if serr := q.QueryRow(txCtx,
			`SELECT next_position, epoch FROM world_feed_counter WHERE game_id = $1 FOR UPDATE`,
			gameID).Scan(&position, &epoch); serr != nil {
			var pgErr *pgconn.PgError
			if errors.As(serr, &pgErr) && pgErr.Code == pgLockNotAvailable {
				return oops.Code(world.CodeFeedLockTimeout).With("game_id", gameID).Wrap(world.ErrFeedLockTimeout)
			}
			return oops.With("operation", "genesis counter lock").With("game_id", gameID).Wrap(serr)
		}

		// Idempotency key: insert the durable checkpoint BEFORE allocating a
		// position. A same-epoch re-run conflicts here and inserts 0 rows.
		tag, cerr := e.Exec(txCtx,
			`INSERT INTO world_genesis_checkpoint (game_id, epoch, aggregate_type, aggregate_id)
			 VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
			gameID, epoch, string(a.aggType), a.id.String())
		if cerr != nil {
			return oops.With("operation", "genesis checkpoint insert").
				With("game_id", gameID).With("aggregate_id", a.id.String()).Wrap(cerr)
		}
		if tag.RowsAffected() == 0 {
			// Already emitted at this epoch — skip WITHOUT consuming a position
			// (no gap). The FOR UPDATE lock released on commit; next_position
			// intentionally NOT incremented.
			emitted = false
			return nil
		}

		// Newly checkpointed — allocate this position and advance the counter.
		if _, uerr := e.Exec(txCtx,
			`UPDATE world_feed_counter SET next_position = next_position + 1 WHERE game_id = $1`,
			gameID); uerr != nil {
			return oops.With("operation", "genesis counter advance").With("game_id", gameID).Wrap(uerr)
		}

		env := wmodel.Finalize(intent, primaryDeltaVersioned(a.aggType, a.id, false, 0, a.version), epoch, position)
		if ierr := insertOutboxRow(txCtx, e, env); ierr != nil {
			return ierr
		}
		emitted = true
		return nil
	})
	if txErr != nil {
		return false, 0, txErr
	}
	return emitted, epoch, nil
}

// CurrentEpoch returns the game's current feed epoch, initializing the counter row
// if the game has no writes yet. Implements outbox.GenesisStore.
func (g *GenesisStore) CurrentEpoch(ctx context.Context, gameID string) (int64, error) {
	e := execerFromCtx(ctx, g.pool)
	q := querierFromCtx(ctx, g.pool)
	if _, err := e.Exec(ctx,
		`INSERT INTO world_feed_counter (game_id, next_position, epoch)
		 VALUES ($1, 1, 1) ON CONFLICT (game_id) DO NOTHING`, gameID); err != nil {
		return 0, oops.With("operation", "genesis current-epoch init").With("game_id", gameID).Wrap(err)
	}
	var epoch int64
	if err := q.QueryRow(ctx,
		`SELECT epoch FROM world_feed_counter WHERE game_id = $1`, gameID).Scan(&epoch); err != nil {
		return 0, oops.With("operation", "genesis current-epoch read").With("game_id", gameID).Wrap(err)
	}
	return epoch, nil
}

// AdvanceEpoch performs the one-locked, complete feed epoch reset. Implements
// outbox.GenesisStore. Under the per-game counter FOR UPDATE lock it: quarantines
// unpublished old-epoch outbox rows (marks published_at so the relay's
// NextUnpublished scan never returns a stale-epoch position — avoiding the
// ambiguous-ordering hazard of old rows surviving an epoch change), increments the
// epoch, resets next_position to the defined origin (so positions restart, never
// inherit the old counter — the round-5 gap), and fires the relay wakeup. All
// under the single lock, so no writer allocates against a half-reset counter.
func (g *GenesisStore) AdvanceEpoch(ctx context.Context, gameID string) (wmodel.EpochResetResult, error) {
	var res wmodel.EpochResetResult
	txErr := withTx(ctx, g.pool, func(txCtx context.Context) error {
		e := execerFromCtx(txCtx, g.pool)
		q := querierFromCtx(txCtx, g.pool)

		if _, serr := e.Exec(txCtx, `SET LOCAL lock_timeout = '`+feedCounterLockTimeout+`'`); serr != nil {
			return oops.With("operation", "epoch-reset set lock_timeout").With("game_id", gameID).Wrap(serr)
		}
		if _, ierr := e.Exec(txCtx,
			`INSERT INTO world_feed_counter (game_id, next_position, epoch)
			 VALUES ($1, 1, 1) ON CONFLICT (game_id) DO NOTHING`, gameID); ierr != nil {
			return oops.With("operation", "epoch-reset counter init").With("game_id", gameID).Wrap(ierr)
		}

		var position, oldEpoch int64
		if serr := q.QueryRow(txCtx,
			`SELECT next_position, epoch FROM world_feed_counter WHERE game_id = $1 FOR UPDATE`,
			gameID).Scan(&position, &oldEpoch); serr != nil {
			var pgErr *pgconn.PgError
			if errors.As(serr, &pgErr) && pgErr.Code == pgLockNotAvailable {
				return oops.Code(world.CodeFeedLockTimeout).With("game_id", gameID).Wrap(world.ErrFeedLockTimeout)
			}
			return oops.With("operation", "epoch-reset counter lock").With("game_id", gameID).Wrap(serr)
		}

		// Quarantine unpublished old-epoch rows: mark published_at so the relay's
		// "next unpublished in (epoch, position) order" scan never returns them.
		qtag, qerr := e.Exec(txCtx,
			`UPDATE outbox
			   SET published_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
			 WHERE game_id = $1 AND epoch = $2 AND published_at IS NULL`,
			gameID, oldEpoch)
		if qerr != nil {
			return oops.With("operation", "epoch-reset quarantine").With("game_id", gameID).Wrap(qerr)
		}

		newEpoch := oldEpoch + 1
		if _, uerr := e.Exec(txCtx,
			`UPDATE world_feed_counter SET epoch = $2, next_position = $3 WHERE game_id = $1`,
			gameID, newEpoch, genesisFeedOrigin); uerr != nil {
			return oops.With("operation", "epoch-reset advance").With("game_id", gameID).Wrap(uerr)
		}

		// Coordinate the relay: a transaction-side wakeup so a LISTENing relay
		// re-scans against the reset counter (published rows quarantined, new
		// epoch positions starting at the origin).
		if _, nerr := e.Exec(txCtx, `SELECT pg_notify($1, $2)`, OutboxNotifyChannel, gameID); nerr != nil {
			return oops.With("operation", "epoch-reset relay notify").With("game_id", gameID).Wrap(nerr)
		}

		res = wmodel.EpochResetResult{
			PreviousEpoch:  oldEpoch,
			NewEpoch:       newEpoch,
			Quarantined:    qtag.RowsAffected(),
			OriginPosition: genesisFeedOrigin,
		}
		return nil
	})
	if txErr != nil {
		return wmodel.EpochResetResult{}, txErr
	}
	return res, nil
}

// enumerateAggregates reads every existing location/exit/character/object and
// builds its genesis descriptor (kind + new-values-only payload). Non-transactional
// reads on the pool: genesis is an operator cutover op, and each per-aggregate
// emit re-checks the durable checkpoint under lock, so a row created after this
// enumeration is simply picked up by a later run (idempotent).
func (g *GenesisStore) enumerateAggregates(ctx context.Context) ([]genesisAggregate, error) {
	var out []genesisAggregate

	locs, err := g.enumerateLocations(ctx)
	if err != nil {
		return nil, err
	}
	out = append(out, locs...)
	exits, err := g.enumerateExits(ctx)
	if err != nil {
		return nil, err
	}
	out = append(out, exits...)
	objs, err := g.enumerateObjects(ctx)
	if err != nil {
		return nil, err
	}
	out = append(out, objs...)
	chars, err := g.enumerateCharacters(ctx)
	if err != nil {
		return nil, err
	}
	out = append(out, chars...)
	return out, nil
}

func (g *GenesisStore) enumerateLocations(ctx context.Context) ([]genesisAggregate, error) {
	rows, err := g.pool.Query(ctx,
		`SELECT id, name, description, version FROM locations WHERE archived_at IS NULL`)
	if err != nil {
		return nil, oops.With("operation", "genesis enumerate locations").Wrap(err)
	}
	defer rows.Close()
	var out []genesisAggregate
	for rows.Next() {
		var idStr, name, description string
		var version int
		if serr := rows.Scan(&idStr, &name, &description, &version); serr != nil {
			return nil, oops.With("operation", "genesis scan location").Wrap(serr)
		}
		id, perr := ulid.Parse(idStr)
		if perr != nil {
			return nil, oops.With("operation", "genesis parse location id").With("id", idStr).Wrap(perr)
		}
		payload, merr := json.Marshal(genesisLocationPayload{ID: idStr, Name: name, Description: description})
		if merr != nil {
			return nil, oops.With("operation", "genesis marshal location payload").Wrap(merr)
		}
		out = append(out, genesisAggregate{aggType: wmodel.AggregateLocation, id: id, kind: genesisKindLocation, version: version, payload: payload})
	}
	if rows.Err() != nil {
		return nil, oops.With("operation", "genesis iterate locations").Wrap(rows.Err())
	}
	return out, nil
}

func (g *GenesisStore) enumerateExits(ctx context.Context) ([]genesisAggregate, error) {
	rows, err := g.pool.Query(ctx,
		`SELECT id, name, from_location_id, to_location_id, version FROM exits`)
	if err != nil {
		return nil, oops.With("operation", "genesis enumerate exits").Wrap(err)
	}
	defer rows.Close()
	var out []genesisAggregate
	for rows.Next() {
		var idStr, name, fromLoc, toLoc string
		var version int
		if serr := rows.Scan(&idStr, &name, &fromLoc, &toLoc, &version); serr != nil {
			return nil, oops.With("operation", "genesis scan exit").Wrap(serr)
		}
		id, perr := ulid.Parse(idStr)
		if perr != nil {
			return nil, oops.With("operation", "genesis parse exit id").With("id", idStr).Wrap(perr)
		}
		payload, merr := json.Marshal(genesisExitPayload{ID: idStr, Name: name, FromLocationID: fromLoc, ToLocationID: toLoc})
		if merr != nil {
			return nil, oops.With("operation", "genesis marshal exit payload").Wrap(merr)
		}
		out = append(out, genesisAggregate{aggType: wmodel.AggregateExit, id: id, kind: genesisKindExit, version: version, payload: payload})
	}
	if rows.Err() != nil {
		return nil, oops.With("operation", "genesis iterate exits").Wrap(rows.Err())
	}
	return out, nil
}

func (g *GenesisStore) enumerateObjects(ctx context.Context) ([]genesisAggregate, error) {
	rows, err := g.pool.Query(ctx,
		`SELECT id, name, description, version FROM objects`)
	if err != nil {
		return nil, oops.With("operation", "genesis enumerate objects").Wrap(err)
	}
	defer rows.Close()
	var out []genesisAggregate
	for rows.Next() {
		var idStr, name, description string
		var version int
		if serr := rows.Scan(&idStr, &name, &description, &version); serr != nil {
			return nil, oops.With("operation", "genesis scan object").Wrap(serr)
		}
		id, perr := ulid.Parse(idStr)
		if perr != nil {
			return nil, oops.With("operation", "genesis parse object id").With("id", idStr).Wrap(perr)
		}
		payload, merr := json.Marshal(genesisObjectPayload{ID: idStr, Name: name, Description: description})
		if merr != nil {
			return nil, oops.With("operation", "genesis marshal object payload").Wrap(merr)
		}
		out = append(out, genesisAggregate{aggType: wmodel.AggregateObject, id: id, kind: genesisKindObject, version: version, payload: payload})
	}
	if rows.Err() != nil {
		return nil, oops.With("operation", "genesis iterate objects").Wrap(rows.Err())
	}
	return out, nil
}

func (g *GenesisStore) enumerateCharacters(ctx context.Context) ([]genesisAggregate, error) {
	rows, err := g.pool.Query(ctx,
		`SELECT id, player_id, name, location_id, version FROM characters`)
	if err != nil {
		return nil, oops.With("operation", "genesis enumerate characters").Wrap(err)
	}
	defer rows.Close()
	var out []genesisAggregate
	for rows.Next() {
		var idStr, playerID, name string
		var locationID *string
		var version int
		if serr := rows.Scan(&idStr, &playerID, &name, &locationID, &version); serr != nil {
			return nil, oops.With("operation", "genesis scan character").Wrap(serr)
		}
		id, perr := ulid.Parse(idStr)
		if perr != nil {
			return nil, oops.With("operation", "genesis parse character id").With("id", idStr).Wrap(perr)
		}
		payload, merr := json.Marshal(genesisCharacterPayload{CharacterID: idStr, PlayerID: playerID, Name: name, LocationID: locationID})
		if merr != nil {
			return nil, oops.With("operation", "genesis marshal character payload").Wrap(merr)
		}
		out = append(out, genesisAggregate{aggType: wmodel.AggregateCharacter, id: id, kind: genesisKindCharacter, version: version, payload: payload})
	}
	if rows.Err() != nil {
		return nil, oops.With("operation", "genesis iterate characters").Wrap(rows.Err())
	}
	return out, nil
}
