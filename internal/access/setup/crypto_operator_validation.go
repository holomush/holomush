// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	authpostgres "github.com/holomush/holomush/internal/auth/postgres"
)

// validateCryptoOperators cross-checks the configured operator list against
// the players table and emits structured warnings for IDs that don't
// correspond to any player. Returns the configured list as a deduplicated
// slice regardless of cross-check outcome — validation is observability,
// not gating, per Phase 5 sub-epic B's INV-B5 (lax+warn) and INV-B7
// (empty config is silent).
//
// Behaviour:
//   - Empty / nil configured list → empty slice, no DB query, no warning.
//   - All IDs known → silent (no warning), full slice returned.
//   - Some / all IDs unknown → one Warn per unknown ID, full configured
//     slice still returned. Server MUST NOT fail startup (INV-B5).
//   - Query failure (transient PG error, closed pool, etc.) → one Warn
//     ("crypto.operator validation skipped"), full configured slice still
//     returned. Server MUST NOT fail startup.
//
// The returned slice is the source of truth wired into
// PlayerAttributeProvider; it is intentionally permissive so that
// operators can be added before their player accounts are created
// without bouncing the server.
// The ([]string, error) return shape is preserved for forward-compatibility
// with sub-epic D, which may add a fail-closed mode. In Phase 5 sub-epic
// B (lax+warn), the error is always nil — see INV-B5.
//
// Relocated verbatim from cmd/holomush/crypto_operator_validation.go (07-09
// item 6) — ABAC already owns the pool + Database edge its Start needs to
// validate against; routing validation through the wiring builder
// instead would make ABAC a wiring consumer and close a second
// ABAC -> wiring -> ABAC cycle (THE RULE forces every wiring consumer
// to DependsOn SubsystemABAC).
//
//nolint:unparam // error result is always nil today; preserved for sub-epic D fail-closed mode.
func validateCryptoOperators(
	ctx context.Context,
	pool *pgxpool.Pool,
	configured []string,
	logger *slog.Logger,
) ([]string, error) {
	set := make(map[string]struct{}, len(configured))
	for _, id := range configured {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		set[id] = struct{}{}
	}
	if len(set) == 0 {
		return []string{}, nil
	}

	deduped := make([]string, 0, len(set))
	for id := range set {
		deduped = append(deduped, id)
	}
	// Deterministic order helps operator debugging (slog output, test
	// reproducibility) — set→slice iteration order is randomized in Go.
	sort.Strings(deduped)

	repo := authpostgres.NewPlayerRepository(pool)
	found, err := repo.ExistingIDs(ctx, deduped)
	if err != nil {
		// Lax+warn: a transient PG failure (e.g., readiness race during
		// startup, or a closed pool in tests) MUST NOT gate startup.
		// Surface the diagnostic but proceed with the full configured
		// slice — operators can fix the underlying issue without bouncing.
		logger.WarnContext(ctx, "crypto.operator validation skipped: database query failed; will re-check at next startup",
			"error", err,
			"configured_count", len(deduped))
		return deduped, nil
	}

	foundSet := make(map[string]struct{}, len(found))
	for _, id := range found {
		foundSet[id] = struct{}{}
	}
	for _, id := range deduped {
		if _, ok := foundSet[id]; !ok {
			logger.WarnContext(ctx, "crypto.operator references unknown player", "player_id", id)
		}
	}
	return deduped, nil
}
