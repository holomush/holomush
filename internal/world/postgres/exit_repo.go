// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package postgres provides PostgreSQL implementations of world repositories.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
)

// ExitRepository implements world.ExitRepository using PostgreSQL.
type ExitRepository struct {
	pool *pgxpool.Pool
}

// NewExitRepository creates a new ExitRepository.
func NewExitRepository(pool *pgxpool.Pool) *ExitRepository {
	return &ExitRepository{pool: pool}
}

// Get retrieves an exit by ID.
func (r *ExitRepository) Get(ctx context.Context, id ulid.ULID) (*world.Exit, error) {
	exit, err := r.scanExit(ctx, `
		SELECT id, from_location_id, to_location_id, name, aliases, bidirectional,
		       return_name, visibility, visible_to, locked, lock_type, lock_data, created_at
		FROM exits WHERE id = $1
	`, id.String())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.With("id", id.String()).Wrap(ErrNotFound)
	}
	if err != nil {
		return nil, oops.With("operation", "get exit").With("id", id.String()).Wrap(err)
	}
	return exit, nil
}

// Create persists a new exit.
// If bidirectional, also creates the return exit atomically within a transaction.
func (r *ExitRepository) Create(ctx context.Context, exit *world.Exit) error {
	// Assign ID if not set
	if exit.ID.Compare(ulid.ULID{}) == 0 {
		exit.ID = core.NewULID()
	}
	if exit.CreatedAt.IsZero() {
		exit.CreatedAt = time.Now()
	}

	// Use a transaction to ensure atomic creation of bidirectional exits
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return oops.With("operation", "begin transaction").Wrap(err)
	}
	defer func() {
		// Rollback is a no-op if tx was committed; error is safe to ignore
		_ = tx.Rollback(ctx) //nolint:errcheck // Rollback error after commit is meaningless
	}()

	if err := r.insertExitTx(ctx, tx, exit); err != nil {
		return err
	}

	// Create return exit if bidirectional
	if exit.Bidirectional && exit.ReturnName != "" {
		returnExit := exit.ReverseExit()
		if returnExit != nil {
			returnExit.ID = core.NewULID()
			returnExit.CreatedAt = exit.CreatedAt

			if err := r.insertExitTx(ctx, tx, returnExit); err != nil {
				return oops.With("operation", "create return exit").Wrap(err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.With("operation", "commit transaction").Wrap(err)
	}

	return nil
}

// insertExitTx inserts a single exit row within a transaction.
func (r *ExitRepository) insertExitTx(ctx context.Context, tx pgx.Tx, exit *world.Exit) error {
	lockDataJSON, err := marshalLockData(exit.LockData)
	if err != nil {
		return oops.With("operation", "marshal lock data").Wrap(err)
	}

	visibleToStrings := ulidsToStrings(exit.VisibleTo)

	_, err = tx.Exec(ctx, `
		INSERT INTO exits (id, from_location_id, to_location_id, name, aliases, bidirectional,
		                   return_name, visibility, visible_to, locked, lock_type, lock_data, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`,
		exit.ID.String(),
		exit.FromLocationID.String(),
		exit.ToLocationID.String(),
		exit.Name,
		exit.Aliases,
		exit.Bidirectional,
		nullableString(exit.ReturnName),
		string(exit.Visibility),
		visibleToStrings,
		exit.Locked,
		nullableLockType(exit.LockType),
		lockDataJSON,
		exit.CreatedAt,
	)
	if err != nil {
		return oops.With("operation", "create exit").With("id", exit.ID.String()).Wrap(err)
	}

	return nil
}

// Update modifies an existing exit.
func (r *ExitRepository) Update(ctx context.Context, exit *world.Exit) error {
	lockDataJSON, err := marshalLockData(exit.LockData)
	if err != nil {
		return oops.With("operation", "marshal lock data").Wrap(err)
	}

	visibleToStrings := ulidsToStrings(exit.VisibleTo)

	result, err := r.pool.Exec(ctx, `
		UPDATE exits SET from_location_id = $2, to_location_id = $3, name = $4, aliases = $5,
		       bidirectional = $6, return_name = $7, visibility = $8, visible_to = $9,
		       locked = $10, lock_type = $11, lock_data = $12
		WHERE id = $1
	`,
		exit.ID.String(),
		exit.FromLocationID.String(),
		exit.ToLocationID.String(),
		exit.Name,
		exit.Aliases,
		exit.Bidirectional,
		nullableString(exit.ReturnName),
		string(exit.Visibility),
		visibleToStrings,
		exit.Locked,
		nullableLockType(exit.LockType),
		lockDataJSON,
	)
	if err != nil {
		return oops.With("operation", "update exit").With("id", exit.ID.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.With("id", exit.ID.String()).Wrap(ErrNotFound)
	}
	return nil
}

// Delete removes an exit by ID.
// If bidirectional, also removes the return exit on a best-effort basis.
// Note: Return exit cleanup failures are logged at ERROR level but don't fail
// the operation, as the primary delete succeeded. This avoids transactional
// complexity while maintaining visibility of orphaned exits.
func (r *ExitRepository) Delete(ctx context.Context, id ulid.ULID) error {
	// First, get the exit to check if it's bidirectional
	exit, err := r.Get(ctx, id)
	if err != nil {
		return err
	}

	// Delete the exit
	result, err := r.pool.Exec(ctx, `DELETE FROM exits WHERE id = $1`, id.String())
	if err != nil {
		return oops.With("operation", "delete exit").With("id", id.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.With("id", id.String()).Wrap(ErrNotFound)
	}

	// If bidirectional, find and delete the return exit
	if exit.Bidirectional && exit.ReturnName != "" {
		returnExit, findErr := r.FindByName(ctx, exit.ToLocationID, exit.ReturnName)
		if findErr != nil {
			// Distinguish between "not found" (acceptable - may have been deleted) and actual errors
			if errors.Is(findErr, ErrNotFound) {
				slog.Debug("return exit not found during bidirectional cleanup (may have been already deleted)",
					"exit_id", id.String(),
					"to_location_id", exit.ToLocationID.String(),
					"return_name", exit.ReturnName)
			} else {
				// Actual database error - log at error level for visibility
				slog.Error("failed to find return exit during bidirectional cleanup",
					"exit_id", id.String(),
					"to_location_id", exit.ToLocationID.String(),
					"return_name", exit.ReturnName,
					"error", findErr)
			}
		} else if returnExit != nil {
			// Check that this is indeed the matching return exit
			if returnExit.ToLocationID == exit.FromLocationID {
				_, cleanupErr := r.pool.Exec(ctx, `DELETE FROM exits WHERE id = $1`, returnExit.ID.String())
				if cleanupErr != nil {
					// Delete failure leaves data inconsistent - log at error level
					slog.Error("failed to delete return exit - orphaned exit remains in database",
						"exit_id", id.String(),
						"return_exit_id", returnExit.ID.String(),
						"error", cleanupErr)
				}
			}
		}
	}

	return nil
}

// ListFromLocation returns all exits from a location.
func (r *ExitRepository) ListFromLocation(ctx context.Context, locationID ulid.ULID) ([]*world.Exit, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, from_location_id, to_location_id, name, aliases, bidirectional,
		       return_name, visibility, visible_to, locked, lock_type, lock_data, created_at
		FROM exits WHERE from_location_id = $1 ORDER BY name
	`, locationID.String())
	if err != nil {
		return nil, oops.With("operation", "list exits from location").With("location_id", locationID.String()).Wrap(err)
	}
	defer rows.Close()

	return r.scanExits(rows)
}

// FindByName finds an exit by name or alias from a location.
// Matching is case-insensitive for both name and aliases.
func (r *ExitRepository) FindByName(ctx context.Context, locationID ulid.ULID, name string) (*world.Exit, error) {
	// Use PostgreSQL LOWER() for case-insensitive matching
	// For aliases, unnest and compare with LOWER() for consistent behavior
	exit, err := r.scanExit(ctx, `
		SELECT id, from_location_id, to_location_id, name, aliases, bidirectional,
		       return_name, visibility, visible_to, locked, lock_type, lock_data, created_at
		FROM exits
		WHERE from_location_id = $1
		  AND (LOWER(name) = LOWER($2) OR EXISTS (
		    SELECT 1 FROM unnest(aliases) AS a WHERE LOWER(a) = LOWER($2)
		  ))
		LIMIT 1
	`, locationID.String(), name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.With("location_id", locationID.String()).With("name", name).Wrap(ErrNotFound)
	}
	if err != nil {
		return nil, oops.With("operation", "find exit by name").With("location_id", locationID.String()).With("name", name).Wrap(err)
	}
	return exit, nil
}

// FindByNameFuzzy finds an exit by name using fuzzy matching (pg_trgm).
// Returns the best match above the similarity threshold, or ErrNotFound.
// Threshold must be between 0.0 and 1.0 inclusive.
func (r *ExitRepository) FindByNameFuzzy(ctx context.Context, locationID ulid.ULID, name string, threshold float64) (*world.Exit, error) {
	if threshold < 0.0 || threshold > 1.0 {
		return nil, oops.
			With("threshold", threshold).
			Errorf("threshold must be between 0.0 and 1.0")
	}

	// Use pg_trgm similarity() to find best matching exit name
	// Also check aliases using array unnest
	exit, err := r.scanExit(ctx, `
		SELECT e.id, e.from_location_id, e.to_location_id, e.name, e.aliases, e.bidirectional,
		       e.return_name, e.visibility, e.visible_to, e.locked, e.lock_type, e.lock_data, e.created_at
		FROM exits e
		WHERE e.from_location_id = $1
		  AND (
		    similarity(e.name, $2) >= $3
		    OR EXISTS (
		      SELECT 1 FROM unnest(e.aliases) AS alias
		      WHERE similarity(alias, $2) >= $3
		    )
		  )
		ORDER BY GREATEST(
		  similarity(e.name, $2),
		  COALESCE((SELECT MAX(similarity(alias, $2)) FROM unnest(e.aliases) AS alias), 0)
		) DESC
		LIMIT 1
	`, locationID.String(), name, threshold)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.With("location_id", locationID.String()).With("name", name).With("threshold", threshold).Wrap(ErrNotFound)
	}
	if err != nil {
		return nil, oops.With("operation", "find exit by name fuzzy").With("location_id", locationID.String()).With("name", name).Wrap(err)
	}
	return exit, nil
}

// exitScanFields holds the intermediate scan values for an exit row.
type exitScanFields struct {
	idStr, fromLocStr, toLocStr string
	aliases                     []string
	returnName                  *string
	visibilityStr               string
	visibleToStrs               []string
	lockType                    *string
	lockDataJSON                []byte
}

// parseExitFromFields converts scanned fields into a world.Exit.
func parseExitFromFields(f *exitScanFields, exit *world.Exit) error {
	var err error
	exit.ID, err = ulid.Parse(f.idStr)
	if err != nil {
		return oops.With("operation", "parse exit id").With("id", f.idStr).Wrap(err)
	}
	exit.FromLocationID, err = ulid.Parse(f.fromLocStr)
	if err != nil {
		return oops.With("operation", "parse from_location_id").With("from_location_id", f.fromLocStr).Wrap(err)
	}
	exit.ToLocationID, err = ulid.Parse(f.toLocStr)
	if err != nil {
		return oops.With("operation", "parse to_location_id").With("to_location_id", f.toLocStr).Wrap(err)
	}
	exit.Aliases = f.aliases
	if f.returnName != nil {
		exit.ReturnName = *f.returnName
	}
	exit.Visibility = world.Visibility(f.visibilityStr)
	exit.VisibleTo, err = stringsToULIDs(f.visibleToStrs)
	if err != nil {
		return err
	}
	if f.lockType != nil {
		exit.LockType = world.LockType(*f.lockType)
	}
	exit.LockData, err = unmarshalLockData(f.lockDataJSON)
	if err != nil {
		return err
	}
	return nil
}

// scanExit scans a single exit from a query.
func (r *ExitRepository) scanExit(ctx context.Context, query string, args ...any) (*world.Exit, error) {
	row := r.pool.QueryRow(ctx, query, args...)

	var exit world.Exit
	var f exitScanFields

	err := row.Scan(
		&f.idStr, &f.fromLocStr, &f.toLocStr, &exit.Name, &f.aliases, &exit.Bidirectional,
		&f.returnName, &f.visibilityStr, &f.visibleToStrs, &exit.Locked, &f.lockType, &f.lockDataJSON, &exit.CreatedAt,
	)
	if err != nil {
		return nil, oops.With("operation", "scan exit").Wrap(err)
	}

	if err := parseExitFromFields(&f, &exit); err != nil {
		return nil, err
	}

	return &exit, nil
}

// scanExits scans multiple exits from rows.
func (r *ExitRepository) scanExits(rows pgx.Rows) ([]*world.Exit, error) {
	exits := make([]*world.Exit, 0)
	for rows.Next() {
		var exit world.Exit
		var f exitScanFields

		if err := rows.Scan(
			&f.idStr, &f.fromLocStr, &f.toLocStr, &exit.Name, &f.aliases, &exit.Bidirectional,
			&f.returnName, &f.visibilityStr, &f.visibleToStrs, &exit.Locked, &f.lockType, &f.lockDataJSON, &exit.CreatedAt,
		); err != nil {
			return nil, oops.With("operation", "scan exit").Wrap(err)
		}

		if err := parseExitFromFields(&f, &exit); err != nil {
			return nil, err
		}

		exits = append(exits, &exit)
	}

	if err := rows.Err(); err != nil {
		return nil, oops.With("operation", "iterate exits").Wrap(err)
	}

	return exits, nil
}

// Helper functions

func ulidsToStrings(ids []ulid.ULID) []string {
	if len(ids) == 0 {
		return nil
	}
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = id.String()
	}
	return strs
}

func stringsToULIDs(strs []string) ([]ulid.ULID, error) {
	if len(strs) == 0 {
		return nil, nil
	}
	ids := make([]ulid.ULID, 0, len(strs))
	for _, s := range strs {
		trimmed := strings.TrimSpace(s)
		id, err := ulid.Parse(trimmed)
		if err != nil {
			return nil, oops.With("operation", "parse visible_to ulid").
				With("value", trimmed).
				Wrap(err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullableLockType(lt world.LockType) *string {
	if lt == "" {
		return nil
	}
	s := string(lt)
	return &s
}

func marshalLockData(data map[string]any) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(data)
	if err != nil {
		return nil, oops.With("operation", "marshal lock data").Wrap(err)
	}
	return b, nil
}

func unmarshalLockData(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, oops.With("operation", "unmarshal lock data").Wrap(err)
	}
	return result, nil
}

// Compile-time interface check.
var _ world.ExitRepository = (*ExitRepository)(nil)
