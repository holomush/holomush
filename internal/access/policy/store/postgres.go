// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/types"
)

// PostgresStore implements PolicyStore using PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore creates a PostgresStore backed by the given connection pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// policyColumns is the shared column list for SELECT queries.
const policyColumns = `id, name, description, effect, source, dsl_text, compiled_ast, enabled, seed_version, created_by, version, created_at, updated_at`

// scanPolicy scans a row into a StoredPolicy.
func scanPolicy(row pgx.Row) (*StoredPolicy, error) {
	var p StoredPolicy
	var effect string
	var ast []byte
	err := row.Scan(
		&p.ID, &p.Name, &p.Description, &effect, &p.Source,
		&p.DSLText, &ast, &p.Enabled, &p.SeedVersion,
		&p.CreatedBy, &p.Version, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scanning policy row: %w", err)
	}
	p.Effect = types.PolicyEffect(effect)
	p.CompiledAST = json.RawMessage(ast)
	return &p, nil
}

// scanPolicies scans multiple rows into a slice of StoredPolicy.
func scanPolicies(rows pgx.Rows) ([]*StoredPolicy, error) {
	defer rows.Close()
	var policies []*StoredPolicy
	for rows.Next() {
		var p StoredPolicy
		var effect string
		var ast []byte
		err := rows.Scan(
			&p.ID, &p.Name, &p.Description, &effect, &p.Source,
			&p.DSLText, &ast, &p.Enabled, &p.SeedVersion,
			&p.CreatedBy, &p.Version, &p.CreatedAt, &p.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning policy row: %w", err)
		}
		p.Effect = types.PolicyEffect(effect)
		p.CompiledAST = json.RawMessage(ast)
		policies = append(policies, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating policy rows: %w", err)
	}
	return policies, nil
}

// ValidateGrammarVersion checks that compiled_ast contains a grammar_version field
// as required by spec (02-policy-dsl.md). This ensures forward-compatible AST evolution.
func ValidateGrammarVersion(ast json.RawMessage) error {
	if len(ast) == 0 {
		return nil // empty AST is allowed (e.g., placeholder policies)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(ast, &parsed); err != nil {
		return oops.Code("POLICY_INVALID_AST").Errorf("compiled_ast is not a valid JSON object")
	}
	if _, ok := parsed["grammar_version"]; !ok {
		return oops.Code("POLICY_INVALID_AST").Errorf("compiled_ast missing required grammar_version field (spec: 02-policy-dsl.md)")
	}
	return nil
}

// Create inserts a new policy, generating a ULID for its ID.
// pg_notify('policy_changed', id) is sent in the same transaction.
func (s *PostgresStore) Create(ctx context.Context, p *StoredPolicy) error {
	if err := ValidateSourceNaming(p.Name, p.Source); err != nil {
		return err
	}
	if err := ValidateGrammarVersion(p.CompiledAST); err != nil {
		return err
	}

	id := ulid.Make().String()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oops.Code("POLICY_CREATE_FAILED").With("name", p.Name).Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	_, err = tx.Exec(ctx, `
		INSERT INTO access_policies (id, name, description, effect, source, dsl_text, compiled_ast, enabled, seed_version, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, id, p.Name, p.Description, string(p.Effect), p.Source,
		p.DSLText, []byte(p.CompiledAST), p.Enabled, p.SeedVersion, p.CreatedBy)
	if err != nil {
		return oops.Code("POLICY_CREATE_FAILED").With("name", p.Name).Wrap(err)
	}

	_, err = tx.Exec(ctx, `SELECT pg_notify('policy_changed', $1)`, id)
	if err != nil {
		return oops.Code("POLICY_CREATE_FAILED").With("name", p.Name).With("operation", "notify").Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.Code("POLICY_CREATE_FAILED").With("name", p.Name).With("operation", "commit").Wrap(err)
	}

	p.ID = id
	p.Version = 1
	return nil
}

// Get retrieves a policy by name.
func (s *PostgresStore) Get(ctx context.Context, name string) (*StoredPolicy, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM access_policies WHERE name = $1`, policyColumns), name)
	p, err := scanPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("POLICY_NOT_FOUND").With("name", name).Errorf("policy not found")
	}
	if err != nil {
		return nil, oops.With("operation", "get policy").With("name", name).Wrap(err)
	}
	return p, nil
}

// GetByID retrieves a policy by its ID.
func (s *PostgresStore) GetByID(ctx context.Context, id string) (*StoredPolicy, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM access_policies WHERE id = $1`, policyColumns), id)
	p, err := scanPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("POLICY_NOT_FOUND").With("id", id).Errorf("policy not found")
	}
	if err != nil {
		return nil, oops.With("operation", "get policy by id").With("id", id).Wrap(err)
	}
	return p, nil
}

// Update modifies an existing policy, increments its version, records the
// old version in access_policy_versions, and sends pg_notify.
func (s *PostgresStore) Update(ctx context.Context, p *StoredPolicy) error {
	if err := ValidateSourceNaming(p.Name, p.Source); err != nil {
		return err
	}
	if err := ValidateGrammarVersion(p.CompiledAST); err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oops.Code("POLICY_UPDATE_FAILED").With("name", p.Name).Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	// Get current version for optimistic concurrency and version history.
	var currentVersion int
	var currentDSL string
	var policyID string
	err = tx.QueryRow(ctx,
		`SELECT id, version, dsl_text FROM access_policies WHERE name = $1 FOR UPDATE`, p.Name,
	).Scan(&policyID, &currentVersion, &currentDSL)
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.Code("POLICY_NOT_FOUND").With("name", p.Name).Errorf("policy not found")
	}
	if err != nil {
		return oops.Code("POLICY_UPDATE_FAILED").With("name", p.Name).Wrap(err)
	}

	// Only bump version and record history when dsl_text changes (spec: 05-storage-audit.md §225).
	// Non-DSL edits (description, enabled, etc.) update the row directly.
	dslChanged := currentDSL != p.DSLText
	newVersion := currentVersion
	if dslChanged {
		newVersion = currentVersion + 1
		_, err = tx.Exec(ctx, `
			INSERT INTO access_policy_versions (id, policy_id, version, dsl_text, changed_by, change_note)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, ulid.Make().String(), policyID, currentVersion, currentDSL, p.CreatedBy, p.ChangeNote)
		if err != nil {
			return oops.Code("POLICY_UPDATE_FAILED").With("name", p.Name).With("operation", "version_history").Wrap(err)
		}
	}

	result, err := tx.Exec(ctx, `
		UPDATE access_policies
		SET description = $2, effect = $3, source = $4, dsl_text = $5,
		    compiled_ast = $6, enabled = $7, seed_version = $8, version = $9, updated_at = now()
		WHERE name = $1
	`, p.Name, p.Description, string(p.Effect), p.Source,
		p.DSLText, []byte(p.CompiledAST), p.Enabled, p.SeedVersion, newVersion)
	if err != nil {
		return oops.Code("POLICY_UPDATE_FAILED").With("name", p.Name).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("POLICY_NOT_FOUND").With("name", p.Name).Errorf("policy not found")
	}

	_, err = tx.Exec(ctx, `SELECT pg_notify('policy_changed', $1)`, policyID)
	if err != nil {
		return oops.Code("POLICY_UPDATE_FAILED").With("name", p.Name).With("operation", "notify").Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.Code("POLICY_UPDATE_FAILED").With("name", p.Name).With("operation", "commit").Wrap(err)
	}

	p.ID = policyID
	p.Version = newVersion
	return nil
}

// Delete removes a policy by name. CASCADE removes version history.
// pg_notify is sent in the same transaction.
func (s *PostgresStore) Delete(ctx context.Context, name string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return oops.Code("POLICY_DELETE_FAILED").With("name", name).Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	var policyID string
	err = tx.QueryRow(ctx, `SELECT id FROM access_policies WHERE name = $1`, name).Scan(&policyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.Code("POLICY_NOT_FOUND").With("name", name).Errorf("policy not found")
	}
	if err != nil {
		return oops.Code("POLICY_DELETE_FAILED").With("name", name).Wrap(err)
	}

	_, err = tx.Exec(ctx, `DELETE FROM access_policies WHERE name = $1`, name)
	if err != nil {
		return oops.Code("POLICY_DELETE_FAILED").With("name", name).Wrap(err)
	}

	_, err = tx.Exec(ctx, `SELECT pg_notify('policy_changed', $1)`, policyID)
	if err != nil {
		return oops.Code("POLICY_DELETE_FAILED").With("name", name).With("operation", "notify").Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.Code("POLICY_DELETE_FAILED").With("name", name).With("operation", "commit").Wrap(err)
	}
	return nil
}

// ListEnabled returns all policies where enabled = true, ordered by name.
func (s *PostgresStore) ListEnabled(ctx context.Context) ([]*StoredPolicy, error) {
	rows, err := s.pool.Query(ctx,
		fmt.Sprintf(`SELECT %s FROM access_policies WHERE enabled = true ORDER BY name`, policyColumns))
	if err != nil {
		return nil, oops.With("operation", "list enabled policies").Wrap(err)
	}
	return scanPolicies(rows)
}

// List returns policies matching the given options, ordered by name.
func (s *PostgresStore) List(ctx context.Context, opts ListOptions) ([]*StoredPolicy, error) {
	var where []string
	var args []any
	argIdx := 1

	if opts.Source != "" {
		where = append(where, fmt.Sprintf("source = $%d", argIdx))
		args = append(args, opts.Source)
		argIdx++
	}
	if opts.Enabled != nil {
		where = append(where, fmt.Sprintf("enabled = $%d", argIdx))
		args = append(args, *opts.Enabled)
		argIdx++
	}
	if opts.Effect != nil {
		where = append(where, fmt.Sprintf("effect = $%d", argIdx))
		args = append(args, string(*opts.Effect))
		argIdx++ //nolint:ineffassign // keeps consistent pattern for future filter additions
	}

	query := fmt.Sprintf("SELECT %s FROM access_policies", policyColumns)
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY name"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, oops.With("operation", "list policies").Wrap(err)
	}
	return scanPolicies(rows)
}
