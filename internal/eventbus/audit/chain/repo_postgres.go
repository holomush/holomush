// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package chain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
)

// postgresRepo implements [Repo] backed by a *pgxpool.Pool.
//
// Events are read from events_audit; chain-init signals from bootstrap_metadata
// (schema: chain_name text, scope_key text — see migration 000030).
//
// Subject convention: the full subject for a (subjectPrefix, scope) pair is
// `subjectPrefix + "." + scope`. DiscoverScopes returns the raw suffix after
// `subjectPrefix + "."` (e.g., given subjectPrefix "events.g.system.rekey" and
// a row with subject "events.g.system.rekey.scene.01ABC", scope is "scene.01ABC").
// Callers that need a domain-typed scope (e.g., "scene:01ABC") apply their own
// Handler.ScopeFromSubject.
type postgresRepo struct {
	pool *pgxpool.Pool
}

// NewPostgresRepo wraps pool in a Repo implementation.
func NewPostgresRepo(pool *pgxpool.Pool) Repo {
	return &postgresRepo{pool: pool}
}

// LoadEntriesByScope returns all events_audit rows whose subject equals
// subjectPrefix + "." + scope, ordered by js_seq ASC.
//
// The envelope column is returned as the Entry.Payload bytes. An empty result
// is not an error — an empty chain is valid at first boot (genesis not yet
// emitted). The caller (Verifier) cross-checks the empty-chain case against
// ChainInitialized.
func (r *postgresRepo) LoadEntriesByScope(ctx context.Context, subjectPrefix, scope string) ([]Entry, error) {
	subject := subjectPrefix + "." + scope
	rows, err := r.pool.Query(ctx, `
		SELECT js_seq, subject, envelope
		  FROM events_audit
		 WHERE subject = $1
		 ORDER BY js_seq ASC
	`, subject)
	if err != nil {
		return nil, oops.Code("AUDIT_CHAIN_LOAD_FAILED").
			With("subject_prefix", subjectPrefix).
			With("scope", scope).
			Wrap(err)
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.JSSeq, &e.Subject, &e.Payload); err != nil {
			return nil, oops.Code("AUDIT_CHAIN_SCAN_FAILED").
				With("subject_prefix", subjectPrefix).
				With("scope", scope).
				Wrap(err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("AUDIT_CHAIN_ROWS_ERR").
			With("subject_prefix", subjectPrefix).
			With("scope", scope).
			Wrap(err)
	}
	return out, nil
}

// DiscoverScopes returns the distinct scope suffixes for all events_audit rows
// whose subject starts with subjectPrefix + ".".
//
// The suffix is the raw string after the prefix (e.g., for prefix
// "events.g.system.rekey" and subject "events.g.system.rekey.scene.01ABC",
// the scope suffix is "scene.01ABC"). An empty result means no chain events
// have been emitted yet; this is not an error.
func (r *postgresRepo) DiscoverScopes(ctx context.Context, subjectPrefix string) ([]string, error) {
	// LIKE pattern: subjectPrefix + ".%" matches all subjects under this prefix.
	// Escape SQL LIKE metacharacters (_, %, \) in subjectPrefix so a chain whose
	// game-id or namespace contains them cannot pull rows from sibling chains.
	escapedPrefix := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(subjectPrefix)
	pattern := escapedPrefix + ".%"
	prefixWithDot := subjectPrefix + "."

	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT subject
		  FROM events_audit
		 WHERE subject LIKE $1 ESCAPE '\'
	`, pattern)
	if err != nil {
		return nil, oops.Code("AUDIT_CHAIN_DISCOVER_FAILED").
			With("subject_prefix", subjectPrefix).
			Wrap(err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var subject string
		if err := rows.Scan(&subject); err != nil {
			return nil, oops.Code("AUDIT_CHAIN_SCAN_FAILED").
				With("subject_prefix", subjectPrefix).
				Wrap(err)
		}
		if !strings.HasPrefix(subject, prefixWithDot) {
			// Defensive: LIKE matched but prefix check failed; skip.
			continue
		}
		scope := subject[len(prefixWithDot):]
		if scope == "" {
			continue
		}
		out = append(out, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("AUDIT_CHAIN_ROWS_ERR").
			With("subject_prefix", subjectPrefix).
			Wrap(err)
	}
	return out, nil
}

// ChainInitialized returns true if the (chainName, scope) pair exists in
// bootstrap_metadata, signaling that the chain genesis has been emitted at
// least once. A missing row is not an error — it means the chain has never
// been initialized (first-boot path).
func (r *postgresRepo) ChainInitialized(ctx context.Context, chainName, scope string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM bootstrap_metadata
			 WHERE chain_name = $1 AND scope_key = $2
		)
	`, chainName, scope).Scan(&exists)
	if err != nil {
		return false, oops.Code("AUDIT_CHAIN_INIT_READ_FAILED").
			With("chain_name", chainName).
			With("scope", scope).
			Wrap(err)
	}
	return exists, nil
}

// MarkChainInitialized records the (chainName, scope) pair in bootstrap_metadata,
// signaling that the chain genesis has been emitted. Idempotent: ON CONFLICT
// DO NOTHING ensures that re-marking an already-initialized chain is a no-op.
func (r *postgresRepo) MarkChainInitialized(ctx context.Context, chainName, scope string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO bootstrap_metadata (chain_name, scope_key)
		VALUES ($1, $2)
		ON CONFLICT (chain_name, scope_key) DO NOTHING
	`, chainName, scope)
	if err != nil {
		// Guard against the pgx no-rows sentinel (should not occur with INSERT,
		// but be defensive).
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.Code("AUDIT_CHAIN_INIT_WRITE_FAILED").
			With("chain_name", chainName).
			With("scope", scope).
			Wrap(fmt.Errorf("bootstrap_metadata insert: %w", err))
	}
	return nil
}
