// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package readstream implements the AdminReadStream handler (sub-epic F).
// cold_reader.go owns the cold-tier SQL read directly against events_audit.
//
// ADR 0017: F bypasses HistoryReader/dispatcher entirely. This reader is
// purpose-built for break-glass operator reads and does NOT share any code
// path with the existing internal/eventbus/history/cold_postgres.go consumer.
// Per-row decrypt is out of scope here (R.11 bead).
package readstream

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/pgnanos"
)

// ColdQuery describes a single AdminReadStream batch query against events_audit.
// All subjects may be exact or NATS wildcard-pattern (trailing ".>").
type ColdQuery struct {
	// Subjects selects which audit rows to read. Each subject may be an exact
	// NATS subject or a NATS wildcard pattern (trailing ".>"). MUST be
	// non-empty; callers should populate via BuildSubjects.
	Subjects []eventbus.Subject
	// Since lower time bound (inclusive). Zero value = no lower bound.
	Since time.Time
	// Until upper time bound (inclusive). Zero value = no upper bound.
	Until time.Time
}

// ColdRow is the raw projected row from events_audit, not yet decrypted.
// R.11 will unmarshal Envelope and decrypt sensitive rows.
type ColdRow struct {
	// ID is the event ULID decoded from the 16-byte binary column.
	ID ulid.ULID
	// Subject is the NATS subject the event was published on.
	Subject eventbus.Subject
	// Type is the event type string.
	Type eventbus.Type
	// Timestamp is the event's logical timestamp (from the column, not the envelope).
	Timestamp time.Time
	// Actor is the host-stamped actor identity.
	Actor eventbus.Actor
	// Envelope is the proto-encoded eventbusv1.Event bytes (full envelope,
	// including ciphertext payload for sensitive events). R.11 unmarshals this.
	Envelope []byte
	// Codec identifies the encryption scheme. NameIdentity = cleartext.
	Codec codec.Name
	// KeyID is the DEK reference for sensitive events; zero for identity-codec rows.
	KeyID codec.KeyID
	// KeyVersion is the DEK key version; zero for identity-codec rows.
	KeyVersion uint32
	// JsSeq is the JetStream sequence number from the js_seq column.
	JsSeq uint64
}

// ColdReader executes break-glass cold-tier reads against events_audit for
// the AdminReadStream handler. It does NOT decrypt; that is R.11's job.
//
// Thread-safe (pgxpool is safe for concurrent use).
type ColdReader struct {
	pool *pgxpool.Pool
}

// NewColdReader constructs a ColdReader backed by the given connection pool.
func NewColdReader(pool *pgxpool.Pool) *ColdReader {
	return &ColdReader{pool: pool}
}

// Read executes a SQL query against events_audit and returns the matching
// rows in ascending timestamp, js_seq order.
//
// Only rows with dek_ref IS NOT NULL are returned (i.e. encrypted rows).
// Identity-codec rows are excluded — they are delivered via the hot/warm
// tiers without encryption overhead; break-glass is for sensitive events.
//
// An empty result set returns (nil, nil) — not an error.
func (c *ColdReader) Read(ctx context.Context, q ColdQuery) ([]ColdRow, error) {
	sqlStr, args, err := c.buildSQL(q)
	if err != nil {
		return nil, err
	}

	rows, err := c.pool.Query(ctx, sqlStr, args...)
	if err != nil {
		return nil, oops.Code("ADMIN_READSTREAM_COLD_QUERY_FAILED").
			With("subject_count", len(q.Subjects)).
			Wrap(err)
	}
	defer rows.Close()

	var out []ColdRow
	for rows.Next() {
		var (
			idBytes    []byte
			subjectStr string
			eventType  string
			// events_audit.timestamp is BIGINT-ns post-gfo6 (INV-STORE-1).
			ts           pgnanos.Time
			actorKindStr string
			actorIDBytes []byte
			envelope     []byte
			codecStr     string
			dekRef       sql.NullInt64
			dekVersion   sql.NullInt32
			jsSeq        int64
		)
		if scanErr := rows.Scan(
			&idBytes, &subjectStr, &eventType, &ts,
			&actorKindStr, &actorIDBytes,
			&envelope, &codecStr, &dekRef, &dekVersion, &jsSeq,
		); scanErr != nil {
			return nil, oops.Code("ADMIN_READSTREAM_COLD_SCAN_FAILED").Wrap(scanErr)
		}

		if len(idBytes) != 16 {
			return nil, oops.Code("ADMIN_READSTREAM_COLD_BAD_ID").
				With("len", len(idBytes)).
				Errorf("events_audit.id must be 16 bytes")
		}
		var id ulid.ULID
		copy(id[:], idBytes)

		// actor_id is BYTEA (16-byte ULID). NULL for system actors.
		var actorID ulid.ULID
		if len(actorIDBytes) == 16 {
			copy(actorID[:], actorIDBytes)
		}

		var keyID codec.KeyID
		var keyVersion uint32
		if dekRef.Valid {
			if dekRef.Int64 < 0 {
				return nil, oops.Code("ADMIN_READSTREAM_COLD_NEGATIVE_DEK_REF").
					With("dek_ref", dekRef.Int64).
					Errorf("events_audit.dek_ref must not be negative")
			}
			keyID = codec.KeyID(uint64(dekRef.Int64))
			// INV-49: a sensitive row MUST carry both dek_ref and dek_version.
			// If dek_ref is present but dek_version is NULL the row is malformed;
			// returning an explicit error lets classifyDecryptErr map it to
			// DEKBadColumns rather than silently producing keyVersion=0 which
			// would misroute to DEK_NOT_FOUND / STALE_DEK.
			if !dekVersion.Valid {
				return nil, oops.Code("ADMIN_READSTREAM_COLD_DEK_VERSION_NULL").
					With("row_id", id).
					Errorf("dek_version NULL with dek_ref present for row %s", id)
			}
		}
		if dekVersion.Valid {
			if dekVersion.Int32 < 0 {
				return nil, oops.Code("ADMIN_READSTREAM_COLD_NEGATIVE_DEK_VERSION").
					With("dek_version", dekVersion.Int32).
					Errorf("events_audit.dek_version must not be negative")
			}
			keyVersion = uint32(dekVersion.Int32)
		}

		out = append(out, ColdRow{
			ID:         id,
			Subject:    eventbus.Subject(subjectStr),
			Type:       eventbus.Type(eventType),
			Timestamp:  ts.Time(),
			Actor:      eventbus.Actor{Kind: actorKindFromString(actorKindStr), ID: actorID},
			Envelope:   envelope,
			Codec:      codec.Name(codecStr),
			KeyID:      keyID,
			KeyVersion: keyVersion,
			JsSeq:      uint64(jsSeq), //nolint:gosec // G115: js_seq is always a positive JetStream sequence; PG bigint stores only non-negative values here
		})
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("ADMIN_READSTREAM_COLD_ROWS_ERR").Wrap(err)
	}
	return out, nil
}

// buildSQL constructs the SQL query string and positional args for a ColdQuery.
// Exported for testing; not part of the public API.
func (c *ColdReader) buildSQL(q ColdQuery) (string, []any, error) { //nolint:gocritic // unnamedResult: three-return helper; naming adds no clarity
	if len(q.Subjects) == 0 {
		return "", nil, oops.Code("ADMIN_READSTREAM_COLD_NO_SUBJECTS").
			Errorf("ColdQuery.Subjects must not be empty")
	}

	var sb strings.Builder
	var args []any

	sb.WriteString(`SELECT id, subject, type, timestamp, actor_kind, actor_id, ` +
		`envelope, codec, dek_ref, dek_version, js_seq ` +
		`FROM events_audit WHERE (`)

	// Build one LIKE clause per subject (Option C from the brief).
	// All subjects are converted to LIKE patterns uniformly:
	//   - Trailing ".>" → trailing ".%"  (NATS sub-tree wildcard)
	//   - Exact subject → used verbatim (no SQL metacharacters in NATS subjects)
	for i, s := range q.Subjects {
		pat := subjectToLikePattern(s)
		args = append(args, pat)
		if i > 0 {
			sb.WriteString(" OR ")
		}
		fmt.Fprintf(&sb, "subject LIKE $%d", len(args))
	}
	sb.WriteString(")")

	// dek_ref IS NOT NULL: only return encrypted rows. Identity-codec rows
	// (cleartext) are excluded from break-glass reads — they are delivered
	// via the hot/warm tiers without needing operator intervention.
	sb.WriteString(" AND dek_ref IS NOT NULL")

	if !q.Since.IsZero() {
		args = append(args, pgnanos.From(q.Since))
		fmt.Fprintf(&sb, " AND timestamp >= $%d", len(args))
	}
	if !q.Until.IsZero() {
		args = append(args, pgnanos.From(q.Until))
		fmt.Fprintf(&sb, " AND timestamp <= $%d", len(args))
	}

	sb.WriteString(" ORDER BY timestamp ASC, js_seq ASC")

	return sb.String(), args, nil
}

// subjectToLikePattern converts a NATS subject to a SQL LIKE pattern.
//
// NATS wildcard `.>` (matches any sub-tree) becomes `.%` for SQL LIKE.
// Exact subjects are returned unchanged — NATS subjects contain only
// alphanumeric characters and `.`, neither of which is a SQL LIKE
// metacharacter, so no escaping is needed.
func subjectToLikePattern(s eventbus.Subject) string {
	str := string(s)
	if strings.HasSuffix(str, ".>") {
		// strip ".>" and replace with ".%"
		return str[:len(str)-2] + ".%"
	}
	return str
}

// actorKindFromString maps the actor_kind text column to eventbus.ActorKind.
// Column values are lowercase strings matching the proto enum names
// (e.g., "system", "character", "player", "plugin").
func actorKindFromString(s string) eventbus.ActorKind {
	switch s {
	case "character":
		return eventbus.ActorKindCharacter
	case "player":
		return eventbus.ActorKindPlayer
	case "system":
		return eventbus.ActorKindSystem
	case "plugin":
		return eventbus.ActorKindPlugin
	default:
		return eventbus.ActorKindUnknown
	}
}
