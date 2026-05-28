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

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/history"
	plugins "github.com/holomush/holomush/internal/plugin"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

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

// newViolationEmitter constructs a ViolationEmitter that publishes
// `events.<game>.system.plugin_integrity_violation` events on every
// PluginDowngradeFence INV-P7-7 refusal. The events.> prefix is required
// by INV-E26 (Phase 5 sub-epic E §3.6) — only the EVENTS JetStream
// SubjectFilter feeds events_audit. The emitter MUST NOT block —
// the fence already enforces a 100ms ceiling around EmitViolation, but
// this implementation also serializes the payload into a tiny Event so
// it never allocates beyond the violation message itself.
//
// Takes the RAW EventBus publisher and the verb registry separately,
// then wraps internally with a fresh RenderingPublisher. Encapsulating
// the wrap here makes it structurally impossible for callers to pass a
// pre-wrapped publisher chain — which would otherwise fail with
// EMIT_RESERVED_HEADER inside RenderingPublisher.Publish (the inner RP
// sees App-Rendering already stamped by the outer one). Pass nil
// rawPub for the degraded "no audit publisher configured" deployment —
// EmitViolation becomes a no-op, the fence still refuses the row.
//
// registry is required when rawPub is non-nil; passing nil registry
// with non-nil rawPub panics, mirroring eventbus.NewRenderingPublisher's
// own nil-registry contract.
func newViolationEmitter(rawPub eventbus.Publisher, registry *core.VerbRegistry, gameID string) history.ViolationEmitter {
	if rawPub == nil {
		return &violationEmitter{publisher: nil, gameID: gameID}
	}
	return &violationEmitter{
		publisher: eventbus.NewRenderingPublisher(rawPub, registry),
		gameID:    gameID,
	}
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
