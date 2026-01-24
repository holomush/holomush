// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package postgres provides PostgreSQL implementations of world repositories.
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

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
		exit.ID = ulid.Make()
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
			returnExit.ID = ulid.Make()
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
// If bidirectional, also removes the return exit atomically in a single transaction.
// Returns *world.BidirectionalCleanupResult if cleanup of the return exit encountered
// issues (e.g., return exit not found). Callers can check IsSevere() to determine
// if the issue warrants logging/alerting.
func (r *ExitRepository) Delete(ctx context.Context, id ulid.ULID) error {
	// First, get the exit to check if it's bidirectional
	exit, err := r.Get(ctx, id)
	if err != nil {
		return err
	}

	// Use a transaction to ensure atomic deletion of bidirectional exits
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return oops.With("operation", "begin transaction").Wrap(err)
	}
	defer func() {
		// Rollback is a no-op if tx was committed; error is safe to ignore
		_ = tx.Rollback(ctx) //nolint:errcheck // Rollback error after commit is meaningless
	}()

	// Delete the primary exit
	result, err := tx.Exec(ctx, `DELETE FROM exits WHERE id = $1`, id.String())
	if err != nil {
		return oops.With("operation", "delete exit").With("id", id.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.With("id", id.String()).Wrap(ErrNotFound)
	}

	// If bidirectional, find and delete the return exit within the same transaction
	var cleanupResult *world.BidirectionalCleanupResult
	if exit.Bidirectional && exit.ReturnName != "" {
		cleanupResult = &world.BidirectionalCleanupResult{
			ExitID:       id,
			ToLocationID: exit.ToLocationID,
			ReturnName:   exit.ReturnName,
		}

		returnExit, findErr := r.findByNameTx(ctx, tx, exit.ToLocationID, exit.ReturnName)
		if findErr != nil {
			// Distinguish between "not found" (acceptable) and actual errors
			if errors.Is(findErr, ErrNotFound) {
				// Return exit not found is not severe - may have been deleted already
				cleanupResult.Issue = &world.CleanupIssue{
					Type: world.CleanupReturnNotFound,
				}
				// Continue with commit - primary delete should still succeed
			} else {
				// Actual error - rollback
				cleanupResult.Issue = &world.CleanupIssue{
					Type: world.CleanupFindError,
					Err:  findErr,
				}
				return cleanupResult
			}
		} else if returnExit != nil && returnExit.ToLocationID == exit.FromLocationID {
			_, cleanupErr := tx.Exec(ctx, `DELETE FROM exits WHERE id = $1`, returnExit.ID.String())
			if cleanupErr != nil {
				cleanupResult.Issue = &world.CleanupIssue{
					Type:         world.CleanupDeleteError,
					ReturnExitID: returnExit.ID,
					Err:          cleanupErr,
				}
				return cleanupResult
			}
			// Clear cleanup result since return exit was successfully deleted
			cleanupResult = nil
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.With("operation", "commit transaction").Wrap(err)
	}

	// Return informational cleanup result if return exit was not found
	if cleanupResult != nil && cleanupResult.Issue != nil {
		return cleanupResult
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

// ListVisibleExits returns exits from a location that are visible to a character.
// The visibility check is atomic - joins with locations to get owner in a single query.
// Visibility rules:
//   - 'all': visible to everyone
//   - 'owner': visible only to location owner
//   - 'list': visible only to characters in visible_to array
//   - unknown values: not visible (fail-closed for security)
func (r *ExitRepository) ListVisibleExits(ctx context.Context, locationID, characterID ulid.ULID) ([]*world.Exit, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT e.id, e.from_location_id, e.to_location_id, e.name, e.aliases, e.bidirectional,
		       e.return_name, e.visibility, e.visible_to, e.locked, e.lock_type, e.lock_data, e.created_at
		FROM exits e
		JOIN locations l ON e.from_location_id = l.id
		WHERE e.from_location_id = $1
		  AND (
		    e.visibility = 'all'
		    OR (e.visibility = 'owner' AND l.owner_id = $2)
		    OR (e.visibility = 'list' AND $2 = ANY(e.visible_to))
		  )
		ORDER BY e.name
	`, locationID.String(), characterID.String())
	if err != nil {
		return nil, oops.With("operation", "list visible exits").
			With("location_id", locationID.String()).
			With("character_id", characterID.String()).
			Wrap(err)
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

// findByNameTx finds an exit by name within a transaction.
func (r *ExitRepository) findByNameTx(ctx context.Context, tx pgx.Tx, locationID ulid.ULID, name string) (*world.Exit, error) {
	exit, err := r.scanExitTx(ctx, tx, `
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

// FindBySimilarity finds an exit by name using fuzzy matching (pg_trgm).
// Returns the best match above the similarity threshold, or ErrNotFound.
// Threshold must be between 0.0 and 1.0 inclusive.
func (r *ExitRepository) FindBySimilarity(ctx context.Context, locationID ulid.ULID, name string, threshold float64) (*world.Exit, error) {
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
	exit.VisibleTo, err = stringsToULIDs(f.visibleToStrs, "visible_to")
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
	return scanExitRow(row)
}

// scanExitTx scans a single exit from a query within a transaction.
func (r *ExitRepository) scanExitTx(ctx context.Context, tx pgx.Tx, query string, args ...any) (*world.Exit, error) {
	row := tx.QueryRow(ctx, query, args...)
	return scanExitRow(row)
}

// scanExitRow scans a single exit from a row.
func scanExitRow(row pgx.Row) (*world.Exit, error) {
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

func nullableLockType(lt world.LockType) *string {
	return nullableString(string(lt))
}

// Compile-time interface check.
var _ world.ExitRepository = (*ExitRepository)(nil)
