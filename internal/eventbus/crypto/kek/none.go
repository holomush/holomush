// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/samber/oops"
)

// NoneProvider is a dev-and-test sentinel that refuses all crypto
// operations. It exists so deployments running with
// crypto.provider.name=none can boot without a master key, while
// guaranteeing they cannot accidentally publish sensitive events.
//
// Two invariants the provider enforces:
//   - INV-CRYPTO-18: at construction, refuse if any crypto_keys row exists.
//     A row implies prior encryption with a real provider; with
//     NoneProvider the historical DEKs are unreachable.
//   - INV-CRYPTO-20: at runtime, refuse Wrap/Unwrap.
type NoneProvider struct{}

// PGQuerier is the pgx surface used by NewNoneProvider (QueryRow for
// the INV-CRYPTO-18 row-count check) and by LocalAEADProvider's
// startupIntegrityCheck (Query for the INV-CRYPTO-19 wrap_key_id enumeration).
// Bundling both methods means any value that satisfies the interface
// can drive both providers; a mock that omits Query will fail to
// compile at the call site rather than at runtime.
//
// Real *pgx.Conn and *pgxpool.Pool both satisfy this interface in
// production.
type PGQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// NewNoneProvider constructs a NoneProvider after verifying INV-CRYPTO-18 (no
// crypto_keys rows exist). The DB SELECT runs synchronously; a non-empty
// table returns CRYPTO_KEYS_NONEMPTY_WITH_NONE_PROVIDER and the
// constructor caller (server boot) refuses to start.
func NewNoneProvider(ctx context.Context, db PGQuerier) (*NoneProvider, error) {
	var count int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM crypto_keys").Scan(&count); err != nil {
		return nil, oops.Code("CRYPTO_KEYS_COUNT_QUERY_FAILED").Wrap(err)
	}
	if count > 0 {
		return nil, oops.Code("CRYPTO_KEYS_NONEMPTY_WITH_NONE_PROVIDER").
			With("crypto_keys_row_count", count).
			Errorf("none provider refuses to start: %d crypto_keys row(s) exist; "+
				"use the same provider that wrote those rows or migrate via "+
				"`holomush crypto provider-migrate` (Phase 6)", count)
	}
	return &NoneProvider{}, nil
}

// NewNoneProviderForUnitTest constructs a NoneProvider without the DB
// check. Tests of Wrap/Unwrap/HealthCheck/Name use this path; the DB
// integrity check is exercised separately in
// none_integration_test.go.
func NewNoneProviderForUnitTest() *NoneProvider { return &NoneProvider{} }

// Name returns "none".
func (p *NoneProvider) Name() string { return "none" }

// Wrap refuses (INV-CRYPTO-20). Surfaces at emit-time when Phase 3 calls
// DEKManager.GetOrCreate for a sensitive event.
func (p *NoneProvider) Wrap(_ context.Context, _ []byte) (wrapped []byte, kekKeyID string, err error) {
	return nil, "", oops.Code("CRYPTO_NONE_PROVIDER_WRAP_REFUSED").
		Errorf("none provider cannot wrap; configure a real provider to publish sensitive events")
}

// Unwrap refuses. There are no rows for it to unwrap (INV-CRYPTO-18 guarantees
// the table was empty at construction); a call here implies a logic bug.
func (p *NoneProvider) Unwrap(_ context.Context, _ []byte, kekKeyID string) ([]byte, error) {
	return nil, oops.Code("CRYPTO_NONE_PROVIDER_UNWRAP_REFUSED").
		With("kek_key_id", kekKeyID).
		Errorf("none provider cannot unwrap; this should be unreachable when INV-CRYPTO-18 holds")
}

// RotateKEK refuses.
func (p *NoneProvider) RotateKEK(_ context.Context) (string, error) {
	return "", oops.Code("CRYPTO_NONE_PROVIDER_ROTATE_REFUSED").
		Errorf("none provider has no KEK to rotate")
}

// HealthCheck succeeds — NoneProvider is "healthy" in the sense it
// reliably refuses operations.
func (p *NoneProvider) HealthCheck(_ context.Context) error { return nil }
