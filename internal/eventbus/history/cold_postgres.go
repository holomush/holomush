// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/history/source"
	"github.com/holomush/holomush/internal/pgnanos"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// postgresColdTier reads archived events from the events_audit table.
// Column shape comes from internal/store/migrations/000009_create_events_audit.
type postgresColdTier struct {
	pool *pgxpool.Pool

	// authGuard evaluates sensitive event delivery decisions on the
	// cold-tier history path. nil = pre-Phase 3b passthrough (mirrors
	// jetStreamHotTier semantics at hot_jetstream.go:60-65).
	authGuard eventbus.SessionAuthGuard
	// dekManager resolves DEK material for sensitive events. Legacy seam:
	// used when sourceResolver is nil. When sourceResolver IS set, dekManager
	// is bypassed in favor of the resolver-aware dispatcher path.
	dekManager eventbus.SessionDEKManager
	// auditEmitter logs plugin decrypt records.
	auditEmitter eventbus.SessionAuditEmitter
	// sourceResolver, when non-nil, routes sensitive cold-tier reads through
	// the resolver-aware dispatcher (DispatchFor). On the cold tier the
	// resolver is typically a source.SimpleResolver (no further fallback
	// past the cold tier itself) — sub-epic E T44 (holomush-jxo8.7.44).
	sourceResolver source.SourceResolver
}

// ColdTierOption tunes postgresColdTier construction. Mirrors HotTierOption
// from hot_jetstream.go for parity across tiers.
type ColdTierOption func(*postgresColdTier)

// WithColdHistoryAuthGuard injects the AuthGuard for sensitive event delivery
// decisions on the cold-tier history path. nil = pre-Phase 3b passthrough.
func WithColdHistoryAuthGuard(g eventbus.SessionAuthGuard) ColdTierOption {
	return func(c *postgresColdTier) { c.authGuard = g }
}

// WithColdHistoryDEKManager injects the DEK Manager used to resolve plaintext
// key material for sensitive codec events on the cold-tier history path.
// Required when WithColdHistoryAuthGuard is set.
func WithColdHistoryDEKManager(m eventbus.SessionDEKManager) ColdTierOption {
	return func(c *postgresColdTier) { c.dekManager = m }
}

// WithColdHistoryDecryptAuditEmitter injects the audit emitter for plugin
// decrypt records on the cold-tier history path.
func WithColdHistoryDecryptAuditEmitter(em eventbus.SessionAuditEmitter) ColdTierOption {
	return func(c *postgresColdTier) { c.auditEmitter = em }
}

// WithColdHistorySourceResolver injects the source.SourceResolver for the
// cold-tier history path. When set, the tier delegates to the resolver-aware
// dispatcher (DispatchFor) instead of decodeAuthorizeAndDispatch. Cold-tier
// production wiring typically uses a source.SimpleResolver: the cold tier is
// the FallbackResolver's target, so a fallback resolver wired on cold reads
// would recurse. Sub-epic E T44 (holomush-jxo8.7.44).
func WithColdHistorySourceResolver(r source.SourceResolver) ColdTierOption {
	return func(c *postgresColdTier) { c.sourceResolver = r }
}

func newPostgresColdTier(pool *pgxpool.Pool, opts ...ColdTierOption) *postgresColdTier {
	c := &postgresColdTier{pool: pool}
	for _, o := range opts {
		o(c)
	}
	return c
}

// NewColdTierLookup returns a source.ColdTierLookup backed by the
// events_audit PostgreSQL table. Used by cmd/holomush to construct a
// source.FallbackResolver for the production INV-CRYPTO-22 hot→cold fallback path
// without exposing the unexported postgresColdTier type.
func NewColdTierLookup(pool *pgxpool.Pool) source.ColdTierLookup {
	return newPostgresColdTier(pool)
}

// Read satisfies ColdTier per §6.1 of the suos spec. Builds a parameterized
// SELECT against events_audit, ordering by js_seq (NOT ULID — see spec §1
// problem statement for why). Honors the subject filter, seq cursor(s),
// time bounds, direction, and page size. An optional `edge` time
// constraint is applied when crossing tiers (the cursor row is OR'd into
// the WHERE so it always passes regardless of edge — see §6.2).
//
// PIGGYBACK VALIDATION (§6.2 — load-bearing): When a cursor is supplied
// (AfterSeq > 0 forward, BeforeSeq > 0 backward), the SQL uses `>=` (forward)
// or `<=` (backward), NOT a strict inequality. The first row returned is
// the cursor echo: it MUST have js_seq == cursor.Seq AND id == cursor.ID.
// We validate, discard, and return the rest. A future maintainer who
// changes >= back to > silently disables the tripwire and reintroduces
// the bug class this design exists to prevent. DO NOT.
func (c *postgresColdTier) Read(ctx context.Context, q eventbus.HistoryQuery, edge time.Time, pageSize int, snap *StreamStateSnapshot) ([]eventbus.Event, error) {
	if pageSize <= 0 {
		return nil, nil
	}
	subjectExact, subjectPattern := classifySubject(string(q.Subject))

	dir := q.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	var cursorSeq uint64
	var cursorID ulid.ULID
	if dir == eventbus.DirectionForward {
		cursorSeq = q.AfterSeq
		cursorID = q.AfterID
	} else {
		cursorSeq = q.BeforeSeq
		cursorID = q.BeforeID
	}
	hasCursor := cursorSeq > 0

	var (
		sb   strings.Builder
		args []any
	)
	sb.WriteString(`SELECT id, subject, type, timestamp, actor_kind, actor_id, envelope, js_seq, rendering, codec, dek_ref, dek_version FROM events_audit WHERE `)
	if subjectPattern != "" {
		args = append(args, subjectPattern)
		fmt.Fprintf(&sb, "subject LIKE $%d", len(args))
	} else {
		args = append(args, subjectExact)
		fmt.Fprintf(&sb, "subject = $%d", len(args))
	}

	// Cursor bound — INCLUSIVE so the cursor echo is the first row.
	if hasCursor {
		args = append(args, int64(cursorSeq)) //nolint:gosec // G115: js_seq is always a positive JetStream sequence number; fits safely in int64
		if dir == eventbus.DirectionForward {
			fmt.Fprintf(&sb, " AND js_seq >= $%d", len(args))
		} else {
			fmt.Fprintf(&sb, " AND js_seq <= $%d", len(args))
		}
	}

	if !q.NotBefore.IsZero() {
		args = append(args, pgnanos.From(q.NotBefore))
		fmt.Fprintf(&sb, " AND timestamp >= $%d", len(args))
	}
	if !q.NotAfter.IsZero() {
		args = append(args, pgnanos.From(q.NotAfter))
		fmt.Fprintf(&sb, " AND timestamp <= $%d", len(args))
	}

	// Crossover edge: cursor row passes regardless of edge (guarded by
	// BOTH js_seq and id — preventing a drift twin from bypassing the
	// edge filter). Non-cursor rows subject to edge filter.
	if !edge.IsZero() {
		args = append(args, pgnanos.From(edge))
		edgeIdx := len(args)
		if hasCursor {
			args = append(args, int64(cursorSeq)) //nolint:gosec // G115: js_seq is always a positive JetStream sequence number; fits safely in int64
			seqIdx := len(args)
			args = append(args, cursorID[:])
			idIdx := len(args)
			fmt.Fprintf(&sb, " AND (timestamp < $%d OR (js_seq = $%d AND id = $%d))",
				edgeIdx, seqIdx, idIdx)
		} else {
			fmt.Fprintf(&sb, " AND timestamp < $%d", edgeIdx)
		}
	}

	if dir == eventbus.DirectionForward {
		sb.WriteString(" ORDER BY js_seq ASC")
	} else {
		sb.WriteString(" ORDER BY js_seq DESC")
	}

	limit := pageSize
	if hasCursor {
		// One extra row for the cursor echo we'll discard.
		limit = pageSize + 1
	}
	args = append(args, limit)
	fmt.Fprintf(&sb, " LIMIT $%d", len(args))

	rows, err := c.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, oops.Code("EVENTBUS_COLD_QUERY_FAILED").
			With("subject", string(q.Subject)).
			Wrap(err)
	}
	defer rows.Close()

	out := make([]eventbus.Event, 0, pageSize)
	first := true
	for rows.Next() {
		var (
			idBytes    []byte
			subjectStr string
			eventType  string
			// ts is scanned for column-position alignment with the SELECT list
			// only; Event.Timestamp is recovered from envelopeBytes via
			// decodeColdRow's proto.Unmarshal (INV-STORE-5 AAD byte-equality).
			ts             pgnanos.Time
			actorKindStr   string
			actorIDBytes   []byte
			envelopeBytes  []byte // was: payload (post-rename)
			seq            int64
			renderingBytes []byte
			codecStr       string
			dekRef         sql.NullInt64
			dekVersion     sql.NullInt32
		)
		if scanErr := rows.Scan(
			&idBytes, &subjectStr, &eventType, &ts,
			&actorKindStr, &actorIDBytes, &envelopeBytes, &seq, &renderingBytes,
			&codecStr, &dekRef, &dekVersion,
		); scanErr != nil {
			return nil, oops.Code("EVENTBUS_COLD_SCAN_FAILED").Wrap(scanErr)
		}
		if len(idBytes) != 16 {
			return nil, oops.Code("EVENTBUS_COLD_BAD_ID").
				With("len", len(idBytes)).
				Errorf("events_audit.id must be 16 bytes")
		}
		var id ulid.ULID
		copy(id[:], idBytes)
		seqU := uint64(seq) //nolint:gosec // G115: js_seq is always a positive JetStream sequence number; PG bigint stores only non-negative values here

		// Cursor-echo guard — MUST run before envelope-unmarshal so a stale
		// cursor row produces EVENTBUS_CURSOR_STALE rather than an unrelated
		// proto.Unmarshal error.
		if hasCursor && first {
			first = false
			if seqU != cursorSeq || id != cursorID {
				return nil, oops.Code("EVENTBUS_CURSOR_STALE").
					With("subject", string(q.Subject)).
					With("cursor_seq", cursorSeq).
					With("cursor_id", cursorID.String()).
					With("got_seq", seqU).
					With("got_id", id.String()).
					Wrap(eventbus.ErrCursorStale)
			}
			continue // discard cursor echo
		}
		first = false

		// Envelope-unmarshal + dispatcher call (Task 4 shared function).
		// Auth inputs come from c (the cold tier's authGuard/dekManager/
		// auditEmitter fields populated by ColdTierOption options) and
		// from q.Identity (the per-call principal from HistoryQuery,
		// mirroring hot_jetstream.go:165's q.Identity flow).
		row := coldRow{
			ID:         idBytes,
			Envelope:   envelopeBytes,
			Codec:      codecStr,
			DEKRef:     dekRef,
			DEKVersion: dekVersion,
		}
		ev, metaOnly, dispatchErr := decodeColdRow(ctx, row, q.Identity, c.authGuard, c.dekManager, c.auditEmitter, c.sourceResolver)
		if dispatchErr != nil {
			return nil, dispatchErr
		}

		// Overlay column-derived fields the dispatcher doesn't own:
		// - ID restated from idBytes (envelope id is already stamped from
		//   the same source by decodeColdRow, but we keep this explicit
		//   to anchor the cold-tier ID provenance to the column)
		// - Seq comes from js_seq column (not in envelope)
		// - Rendering preserves the column-based JSONB unmarshal for
		//   rolling-upgrade compatibility (DiscardUnknown semantics).
		ev.ID = id
		ev.Seq = seqU
		ev.MetadataOnly = metaOnly

		if len(renderingBytes) > 0 {
			var protoMD corev1.RenderingMetadata
			// DiscardUnknown: tolerate forward schema additions on persisted
			// JSONB rendering payloads. Strict decode would fail rolling
			// upgrades where new writers stamp newer fields while older
			// readers are still reading archived data.
			if unmarshalErr := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(renderingBytes, &protoMD); unmarshalErr != nil {
				return nil, oops.Code("EVENTBUS_COLD_BAD_RENDERING").
					With("subject", string(q.Subject)).
					Wrap(unmarshalErr)
			}
			ev.Rendering = eventbus.RenderingFromProto(&protoMD)
		}

		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("EVENTBUS_COLD_ROWS_ERR").Wrap(err)
	}

	// Cursor supplied but zero rows returned — cursor seq is absent from cold.
	// Classify LAG vs STALE by consulting the snapshot's full live-window
	// range [firstSeq, lastSeq]:
	//   - cursor.Seq inside [firstSeq, lastSeq] → LAG (audit projection has
	//     not caught up yet to a seq that JS still holds).
	//   - cursor.Seq below firstSeq → STALE (retention aged it out of JS;
	//     it should be in cold but isn't — drift or deletion).
	//   - cursor.Seq above lastSeq  → STALE (cursor refers to a seq that
	//     never existed; rebuild reassigned seqs or client fabricated one).
	//   - snap.Get error            → propagate; don't mask infra failures
	//     as STALE because that would tell clients to drop a still-valid
	//     cursor on a transient JS lookup blip.
	if hasCursor && first {
		if snap != nil {
			firstSeq, lastSeq, snapErr := snap.Get(ctx)
			if snapErr != nil {
				return nil, snapErr
			}
			if firstSeq > 0 && cursorSeq >= firstSeq && cursorSeq <= lastSeq {
				return nil, oops.Code("EVENTBUS_CURSOR_LAG").
					With("subject", string(q.Subject)).
					With("cursor_seq", cursorSeq).
					With("cursor_id", cursorID.String()).
					With("js_first_seq", firstSeq).
					With("js_last_seq", lastSeq).
					Wrap(eventbus.ErrCursorLag)
			}
		}
		return nil, oops.Code("EVENTBUS_CURSOR_STALE").
			With("subject", string(q.Subject)).
			With("cursor_seq", cursorSeq).
			With("cursor_id", cursorID.String()).
			Wrap(eventbus.ErrCursorStale)
	}

	return out, nil
}

// classifySubject normalizes a HistoryQuery.Subject into either an exact
// match OR a LIKE pattern. NATS wildcards translate to SQL LIKE:
//
//   - `*` → matches one token → `[^.]+` in spirit, but SQL LIKE can't
//     express "anything-but-dot" without a regex. We approximate with `%`
//     accepting that a pattern with `*` may match more than the NATS rules
//     strictly allow. Callers that depend on strict NATS wildcard
//     semantics against archived rows SHOULD switch to an exact subject.
//   - `>` (terminal) → matches rest → `%` at that position.
//
// F4 only uses exact subjects from the gRPC handler; wildcard support here
// is a TODO(F5) stub to be fleshed out when plugin-owned audit schemas
// need it.
func classifySubject(subject string) (exact, pattern string) {
	if subject == "" {
		return "", ""
	}
	if !strings.ContainsAny(subject, "*>") {
		return subject, ""
	}
	// Translate: "*" and ">" both become "%" for LIKE. This is coarser
	// than NATS wildcard semantics; noted above.
	escaped := strings.ReplaceAll(subject, "%", `\%`)
	escaped = strings.ReplaceAll(escaped, "_", `\_`)
	escaped = strings.ReplaceAll(escaped, "*", "%")
	escaped = strings.ReplaceAll(escaped, ">", "%")
	return "", escaped
}

// coldRow holds the fields decodeColdRow needs to call the shared
// dispatcher: ID for error messages, the marshaled Envelope bytes, and
// the codec/DEK columns that supply AAD inputs. Subject/Type/Timestamp/
// Actor are recovered from the unmarshaled envelope (no need to mirror
// them as columns); js_seq + rendering are overlaid on the dispatcher
// output by the production Read method (they don't enter the dispatcher).
type coldRow struct {
	ID         []byte
	Envelope   []byte
	Codec      string
	DEKRef     sql.NullInt64
	DEKVersion sql.NullInt32
}

// decodeColdRow is the cold-path equivalent of decodeJetStreamMessage's
// auth-aware branch in hot_jetstream.go: it unmarshals the envelope and
// calls the shared dispatcher (decodeAuthorizeAndDispatch) with column-
// derived inputs. Returns (event, metadataOnly, error).
//
// On unmarshal failure returns AUDIT_ENVELOPE_UNMARSHAL_FAILED — surfaces
// audit-table corruption distinctly from downstream auth/decode errors.
func decodeColdRow(
	ctx context.Context,
	row coldRow,
	identity eventbus.SessionIdentity,
	guard eventbus.SessionAuthGuard,
	dekMgr eventbus.SessionDEKManager,
	auditEm eventbus.SessionAuditEmitter,
	resolver source.SourceResolver,
) (eventbus.Event, bool, error) {
	var pbEnvelope eventbusv1.Event
	if err := proto.Unmarshal(row.Envelope, &pbEnvelope); err != nil {
		return eventbus.Event{}, false, oops.Code("AUDIT_ENVELOPE_UNMARSHAL_FAILED").
			With("event_id", hex.EncodeToString(row.ID)).
			Wrap(err)
	}
	codecName := codec.Name(row.Codec)
	var keyID codec.KeyID
	var keyVersion uint32
	if codecName != codec.NameIdentity {
		// Sensitive (non-identity) rows MUST carry both DEK columns —
		// the audit projection stamps them from the App-Dek-Ref /
		// App-Dek-Version headers (per INV-CRYPTO-25). NULL columns on a
		// sensitive row mean a corrupted or partially-projected row;
		// fail-closed rather than feeding (0, 0) to the dispatcher
		// which would surface as a confusing Resolve(0, 0) miss or an
		// auth-guard mismatch. Mirrors the hot-tier contract violation
		// at hot_jetstream.go:502-507 (EVENTBUS_HISTORY_DEK_HEADER_MISSING).
		if !row.DEKRef.Valid || !row.DEKVersion.Valid {
			return eventbus.Event{}, false, oops.Code("EVENTBUS_COLD_DEK_COLUMNS_MISSING").
				With("codec", row.Codec).
				With("has_dek_ref", row.DEKRef.Valid).
				With("has_dek_version", row.DEKVersion.Valid).
				With("event_id", hex.EncodeToString(row.ID)).
				Errorf("sensitive audit row missing required DEK columns")
		}
		if row.DEKRef.Int64 < 0 || row.DEKVersion.Int32 < 0 {
			return eventbus.Event{}, false, oops.Code("EVENTBUS_COLD_BAD_DEK_COLUMNS").
				With("codec", row.Codec).
				With("dek_ref", row.DEKRef.Int64).
				With("dek_version", row.DEKVersion.Int32).
				With("event_id", hex.EncodeToString(row.ID)).
				Errorf("sensitive audit row has invalid (negative) DEK column values")
		}
		keyID = codec.KeyID(row.DEKRef.Int64)
		keyVersion = uint32(row.DEKVersion.Int32)
	}
	if resolver != nil {
		// Resolver-aware dispatch: routes through newDispatcher.DispatchFor
		// which uses resolver.Resolve in place of the inline dekMgr.Resolve
		// call. Sub-epic E T44 (holomush-jxo8.7.44).
		d := newDispatcher(WithSourceResolver(resolver))
		return d.DispatchFor(ctx, &pbEnvelope, codecName, keyID, keyVersion, identity, guard, auditEm)
	}
	return decodeAuthorizeAndDispatch(
		ctx, &pbEnvelope, codecName, keyID, keyVersion,
		identity, guard, dekMgr, auditEm, false,
	)
}

// actorFromAuditRow rebuilds an eventbus.Actor from the string/bytes the
// audit projection stored. Mirrors the publisher's inverse in
// subscriber.go.
func actorFromAuditRow(kindStr string, idBytes []byte) eventbus.Actor {
	a := eventbus.Actor{}
	switch kindStr {
	case "character":
		a.Kind = eventbus.ActorKindCharacter
	case "player":
		a.Kind = eventbus.ActorKindPlayer
	case "system":
		a.Kind = eventbus.ActorKindSystem
	case "plugin":
		a.Kind = eventbus.ActorKindPlugin
	default:
		a.Kind = eventbus.ActorKindUnknown
	}
	if len(idBytes) == 16 {
		var u ulid.ULID
		copy(u[:], idBytes)
		a.ID = u
	}
	return a
}

// LookupByID implements source.ColdTierLookup for the events_audit-backed
// cold tier. Used by the INV-CRYPTO-22 fallback path: dispatcher's hot-tier DEK
// lookup failed, ask the cold tier whether a re-encrypted copy exists.
// Returns (envelope, false, nil) when no row exists.
func (c *postgresColdTier) LookupByID(ctx context.Context, id eventbus.EventID) (eventbus.Envelope, bool, error) {
	var (
		idBytes    []byte
		subject    string
		evType     string
		envelopeB  []byte
		codecName  string
		dekRef     *int64
		dekVersion *uint32
		ts         pgnanos.Time
	)
	err := c.pool.QueryRow(ctx, `
		SELECT id, subject, type, envelope, codec, dek_ref, dek_version, timestamp
		  FROM events_audit
		 WHERE id = $1
	`, id[:]).Scan(&idBytes, &subject, &evType, &envelopeB, &codecName, &dekRef, &dekVersion, &ts)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return eventbus.Envelope{}, false, nil
		}
		return eventbus.Envelope{}, false, oops.Code("COLD_LOOKUP_QUERY_FAILED").
			With("event_id", id.String()).Wrap(err)
	}
	env := eventbus.NewEnvelopeFromColdRow(eventbus.ColdRow{
		EventID:    id,
		Subject:    subject,
		Type:       evType,
		Payload:    envelopeB,
		Codec:      codecName,
		KeyID:      derefKeyID(dekRef),
		KeyVersion: derefUint32(dekVersion),
		Timestamp:  ts.Time(),
	})
	return env, true, nil
}

// derefKeyID dereferences a nullable int64 pointer to a codec.KeyID.
// Returns 0 for nil (identity-codec rows have no dek_ref).
func derefKeyID(p *int64) codec.KeyID {
	if p == nil {
		return 0
	}
	return codec.KeyID(*p) //nolint:gosec // G115: dek_ref is a BIGSERIAL PK reference; always non-negative
}

// derefUint32 dereferences a nullable uint32 pointer.
// Returns 0 for nil (identity-codec rows have no dek_version).
func derefUint32(p *uint32) uint32 {
	if p == nil {
		return 0
	}
	return *p
}
