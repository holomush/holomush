// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/history"
	plugins "github.com/holomush/holomush/internal/plugin"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// buildKeySelector returns the single codec.KeySelector instance threaded
// into both audit.PluginConsumerManager (via audit.WithKeySelector) and
// history.NewReader (via history.WithCodecSelector). INV-P7-9 requires the
// SAME pointer-identity selector in both places — see the parked test at
// test/integration/eventbus_e2e/dispatcher_selector_identity_test.go.
//
// Phase 7 keeps the production deployment on the identity selector
// (plugin-owned audit subjects are not yet encrypted at the bus level).
// When a real KEK-backed selector ships, this constructor takes the
// configured codec.KeySelector and returns it unchanged. The single
// pointer is what matters for INV-P7-9.
func buildKeySelector() codec.KeySelector {
	return &identityProductionKeySelector{}
}

// identityProductionKeySelector is the placeholder selector used while
// no plugin-owned audit subject is encrypted at the bus boundary. It
// always returns (codec.NameIdentity, "", nil) for encrypt and the
// no-op codec.NoKey for decrypt — equivalent to the package-private
// identityKeySelector in internal/eventbus/publisher.go but lifted to
// the boot wiring layer so the SAME instance can be threaded into both
// the dispatcher and the reader.
type identityProductionKeySelector struct{}

func (identityProductionKeySelector) SelectForEncrypt(_ context.Context, _ string) (codec.Name, codec.KeyLabel, error) {
	return codec.NameIdentity, "", nil
}

func (identityProductionKeySelector) SelectForDecrypt(_ context.Context, _ codec.Name, _ codec.KeyID) (codec.Key, error) {
	return codec.NoKey, nil
}

// buildAlwaysSensitiveSet walks every loaded manifest and produces the
// qualified `<plugin>:<event_type>` set the PluginDowngradeFence uses for
// INV-P7-7 (manifest-set heuristic). Built ONCE at boot per INV-P7-8 — the
// fence copies the input map so callers may not mutate it after
// construction.
//
// Returns an empty (non-nil) map when mgr is nil or no plugin declares
// `crypto.emits[].sensitivity: always`, mirroring the behaviour of
// alwaysSensitiveFromManifest in test/integration/eventbus_e2e/.
func buildAlwaysSensitiveSet(mgr *plugins.Manager) map[string]struct{} {
	out := map[string]struct{}{}
	if mgr == nil {
		return out
	}
	for _, name := range mgr.ListPlugins() {
		dp, ok := mgr.GetLoadedPlugin(name)
		if !ok || dp.Manifest == nil || dp.Manifest.Crypto == nil {
			continue
		}
		for _, emit := range dp.Manifest.Crypto.Emits {
			if emit.Sensitivity != plugins.SensitivityAlways {
				continue
			}
			key := emit.EventType
			prefix := dp.Manifest.Name + ":"
			if !startsWith(key, prefix) {
				key = prefix + key
			}
			out[key] = struct{}{}
		}
	}
	return out
}

// startsWith is a tiny stdlib-free helper so this file does not pull
// the strings package just to check a single prefix.
func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// newCryptoKeysLookup wraps the *pgxpool.Pool with a thin Exists query
// that satisfies history.CryptoKeysLookup. The query filters
// `destroyed_at IS NULL` so destroyed DEKs read as Exists=false (the
// fence then surfaces the row as metadata_only=true per INV-P7-15).
func newCryptoKeysLookup(pool *pgxpool.Pool) history.CryptoKeysLookup {
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

// newViolationEmitter wraps the host's audit Publisher to publish
// `events.<game>.system.plugin_integrity_violation` events on every
// PluginDowngradeFence INV-P7-7 refusal. The events.> prefix is required
// by INV-E26 (Phase 5 sub-epic E §3.6) — only the EVENTS JetStream
// SubjectFilter feeds events_audit. The emitter MUST NOT block —
// the fence already enforces a 100ms ceiling around EmitViolation, but
// this implementation also serializes the payload into a tiny Event so
// it never allocates beyond the violation message itself.
//
// Publisher contract: pub MUST be a publisher chain that does NOT yet
// stamp App-Rendering — typically a freshly-wrapped RenderingPublisher
// over the raw EventBus publisher. Passing a chain that already stamped
// App-Rendering fails with EMIT_RESERVED_HEADER inside
// RenderingPublisher.Publish. In particular, the gRPC subsystem's
// primary `publisher` (the one returned by grpcSubsystem.wrapPublisher)
// is already wrapped and MUST NOT be passed here; the production wiring
// in grpcSubsystem.Start constructs a dedicated wrapper for this emitter
// instead. Pass nil for the degraded "no audit publisher configured"
// deployment — EmitViolation becomes a no-op, the fence still refuses
// the row.
func newViolationEmitter(pub eventbus.Publisher, gameID string) history.ViolationEmitter {
	return &violationEmitter{publisher: pub, gameID: gameID}
}

type violationEmitter struct {
	publisher eventbus.Publisher
	gameID    string
}

func (e *violationEmitter) EmitViolation(
	ctx context.Context,
	pluginName string,
	row *pluginauditpb.AuditRow,
	expectedSensitivity string,
	refusalCode string,
) error {
	if e.publisher == nil {
		// Degraded deployment — the fence still refuses the row; we just
		// can't emit the audit signal. Return nil so the fence does not
		// log an emit-error on every refusal.
		return nil
	}
	// Subject prefix MUST be `events.<game>.` per INV-E26 (Phase 5 sub-epic E
	// §3.6 supersession of master spec §4.6 line 830): the EVENTS JetStream
	// SubjectFilter at internal/eventbus/subsystem.go:24,27 is the only path
	// by which audit projection writes to events_audit, so audit-bearing
	// events MUST live under that filter. The `audit.<game>.` prefix is
	// forbidden — it bypasses the filter and silently drops the event.
	subjectStr := fmt.Sprintf("events.%s.system.plugin_integrity_violation", e.gameID)
	subj, err := eventbus.NewSubject(subjectStr)
	if err != nil {
		return oops.Code("PLUGIN_INTEGRITY_VIOLATION_INVALID_SUBJECT").
			With("subject", subjectStr).
			Wrap(err)
	}
	evType, err := eventbus.NewType("system:plugin_integrity_violation")
	if err != nil {
		return oops.Code("PLUGIN_INTEGRITY_VIOLATION_INVALID_TYPE").Wrap(err)
	}
	rowID := ulid.ULID{}
	if len(row.GetId()) == 16 {
		copy(rowID[:], row.GetId())
	}
	payload, err := json.Marshal(map[string]string{
		"plugin_name":          pluginName,
		"event_id":             rowID.String(),
		"event_type":           row.GetType(),
		"claimed_codec":        row.GetCodec(),
		"expected_sensitivity": expectedSensitivity,
		"refusal_code":         refusalCode,
	})
	if err != nil {
		return oops.Code("PLUGIN_INTEGRITY_VIOLATION_PAYLOAD_MARSHAL").Wrap(err)
	}
	ev := eventbus.NewEvent(subj, evType, eventbus.Actor{Kind: eventbus.ActorKindSystem}, payload)
	if perr := e.publisher.Publish(ctx, ev); perr != nil {
		return oops.Code("PLUGIN_INTEGRITY_VIOLATION_EMIT_FAILED").
			With("plugin_name", pluginName).
			With("subject", subjectStr).
			Wrap(perr)
	}
	return nil
}
