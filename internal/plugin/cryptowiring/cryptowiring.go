// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package cryptowiring holds the plugin-manifest-derived crypto/audit wiring
// shared by production boot (cmd/holomush) and the integration harness
// (internal/testsupport/integrationtest). Extracting these derivations keeps
// the harness faithful to prod's exact ownership/sensitivity routing.
package cryptowiring

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/history"
)

// ManifestSource is the narrow read surface the derivations need from a loaded
// plugin set. *plugin.Manager satisfies the richer original API; the prod call
// sites adapt it (see managerSource in cmd/holomush). Defined as an interface
// so cryptowiring unit tests use fakes instead of a fully-loaded Manager.
type ManifestSource interface {
	ListPlugins() []string
	// AlwaysSensitiveEmitTypes returns the crypto.emits[] event types declared
	// sensitivity:always for pluginName (qualified or unqualified).
	AlwaysSensitiveEmitTypes(pluginName string) []string
}

// AlwaysSensitiveSet produces the qualified `<plugin>:<event_type>` set the
// PluginDowngradeFence uses for INV-P7-7. Returns a non-nil empty map when src
// is nil. Each unqualified event type is prefixed with `<pluginName>:`.
func AlwaysSensitiveSet(src ManifestSource) map[string]struct{} {
	out := map[string]struct{}{}
	if src == nil {
		return out
	}
	for _, name := range src.ListPlugins() {
		prefix := name + ":"
		for _, et := range src.AlwaysSensitiveEmitTypes(name) {
			key := et
			if !strings.HasPrefix(key, prefix) {
				key = prefix + key
			}
			out[key] = struct{}{}
		}
	}
	return out
}

// KeySelector returns a new identity codec.KeySelector. Callers MUST call this
// once and thread the SAME instance into both audit.PluginConsumerManager
// (WithKeySelector) and history.NewReader (WithCodecSelector): INV-P7-9 requires
// pointer-identity across the two sinks, which is the caller's responsibility,
// not a guarantee of this constructor (it allocates a fresh value per call).
func KeySelector() codec.KeySelector { return &identityKeySelector{} }

type identityKeySelector struct{}

func (identityKeySelector) SelectForEncrypt(_ context.Context, _ string) (codec.Name, codec.KeyLabel, error) {
	return codec.NameIdentity, "", nil
}

func (identityKeySelector) SelectForDecrypt(_ context.Context, _ codec.Name, _ codec.KeyID) (codec.Key, error) {
	return codec.NoKey, nil
}

// CryptoKeysLookup wraps the pool with the Exists query satisfying
// history.CryptoKeysLookup. Filters destroyed_at IS NULL so destroyed DEKs read
// as Exists=false (INV-P7-15).
func CryptoKeysLookup(pool *pgxpool.Pool) history.CryptoKeysLookup {
	return &cryptoKeysLookup{pool: pool}
}

type cryptoKeysLookup struct {
	pool *pgxpool.Pool
}

func (l *cryptoKeysLookup) Exists(ctx context.Context, dekRef uint64) (bool, error) {
	if l.pool == nil {
		return false, oops.Code("CRYPTO_KEYS_LOOKUP_POOL_NIL").
			Errorf("crypto_keys lookup invoked with nil pool")
	}
	const q = `SELECT 1 FROM crypto_keys WHERE id = $1 AND destroyed_at IS NULL LIMIT 1`
	var one int
	err := l.pool.QueryRow(ctx, q, dekRef).Scan(&one)
	if err != nil {
		// pgx returns ErrNoRows when the row is absent (or destroyed) —
		// that's the legitimate Exists=false case, NOT an infrastructure
		// failure.
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, oops.Code("CRYPTO_KEYS_LOOKUP_QUERY_FAILED").
			With("dek_ref", dekRef).
			Wrap(err)
	}
	return true, nil
}
