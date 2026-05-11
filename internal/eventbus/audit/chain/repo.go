// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package chain

import (
	"context"
)

// Entry is one decoded events_audit row returned by Repo queries.
// Payload holds the raw envelope bytes stored in the events_audit.envelope column.
type Entry struct {
	JSSeq   int64
	Subject string
	Payload []byte
}

// Repo abstracts the SQL surface for the audit-chain primitive.
// Backed by [NewPostgresRepo] in production.
//
// References chains by subjectPrefix (the unique-per-chain metadata field from
// [Chain.SubjectPrefix]) rather than by the [Chain] struct or [Handler] bundle,
// so the Repo does not carry per-chain behavior.
//
// Subject convention: for a given subjectPrefix and scope, the full events_audit
// subject is `subjectPrefix + "." + scope`. DiscoverScopes returns the raw suffix
// (scope = subject[len(subjectPrefix)+1:]); callers with a [Handler] apply
// Handler.ScopeFromSubject for domain-typed scope parsing.
type Repo interface {
	// LoadEntriesByScope returns all events_audit rows with
	// subject = subjectPrefix + "." + scope, ordered by js_seq ASC.
	// An empty slice is not an error (first-boot, genesis not yet emitted).
	LoadEntriesByScope(ctx context.Context, subjectPrefix, scope string) ([]Entry, error)

	// DiscoverScopes returns distinct scope suffixes for all events_audit rows
	// whose subject starts with subjectPrefix + ".".
	// The returned strings are raw suffixes (e.g. "scene.01ABC" for rekey),
	// NOT domain-parsed scopes.
	DiscoverScopes(ctx context.Context, subjectPrefix string) ([]string, error)

	// ChainInitialized returns true if a bootstrap_metadata row exists for
	// (chainName, scope), meaning the chain genesis was emitted at least once.
	// A missing row is not an error — it indicates first-boot.
	ChainInitialized(ctx context.Context, chainName, scope string) (bool, error)

	// MarkChainInitialized records (chainName, scope) in bootstrap_metadata.
	// Idempotent: re-marking an already-initialized chain is a no-op.
	MarkChainInitialized(ctx context.Context, chainName, scope string) error
}
