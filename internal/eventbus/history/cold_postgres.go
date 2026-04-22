// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
)

// postgresColdTier reads archived events from the events_audit table.
// Column shape comes from internal/store/migrations/000009_create_events_audit.
type postgresColdTier struct {
	pool *pgxpool.Pool
}

func newPostgresColdTier(pool *pgxpool.Pool) *postgresColdTier {
	return &postgresColdTier{pool: pool}
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
func (c *postgresColdTier) Read(ctx context.Context, q eventbus.HistoryQuery, edge time.Time, pageSize int) ([]eventbus.Event, error) {
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
	sb.WriteString(`SELECT id, subject, type, timestamp, actor_kind, actor_id, payload, js_seq FROM events_audit WHERE `)
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
		args = append(args, q.NotBefore)
		fmt.Fprintf(&sb, " AND timestamp >= $%d", len(args))
	}
	if !q.NotAfter.IsZero() {
		args = append(args, q.NotAfter)
		fmt.Fprintf(&sb, " AND timestamp <= $%d", len(args))
	}

	// Crossover edge: cursor row passes regardless of edge (guarded by
	// BOTH js_seq and id — preventing a drift twin from bypassing the
	// edge filter). Non-cursor rows subject to edge filter.
	if !edge.IsZero() {
		args = append(args, edge)
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
			idBytes      []byte
			subjectStr   string
			eventType    string
			ts           time.Time
			actorKindStr string
			actorIDBytes []byte
			payload      []byte
			seq          int64
		)
		if scanErr := rows.Scan(&idBytes, &subjectStr, &eventType, &ts, &actorKindStr, &actorIDBytes, &payload, &seq); scanErr != nil {
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

		out = append(out, eventbus.Event{
			ID:        id,
			Seq:       seqU,
			Subject:   eventbus.Subject(subjectStr),
			Type:      eventbus.Type(eventType),
			Timestamp: ts.UTC(),
			Actor:     actorFromAuditRow(actorKindStr, actorIDBytes),
			Payload:   payload,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("EVENTBUS_COLD_ROWS_ERR").Wrap(err)
	}

	// Cursor supplied but zero rows returned — cursor seq is absent.
	// LAG vs STALE distinction comes in Task 8; for now all missing cursors
	// are STALE.
	if hasCursor && first {
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

// actorFromAuditRow rebuilds an eventbus.Actor from the string/bytes the
// audit projection stored. Mirrors the publisher's inverse in
// subscriber.go.
//
// TODO(holomush-u5bb): LegacyID is not persisted. The audit projection
// only stores actor_kind + actor_id (ULID bytes), so plugin-authored
// actors with non-ULID identifiers lose fidelity when read through the
// cold tier. Requires adding an actor_legacy_id column to events_audit
// (migration + projection update + reader update). Tracked separately.
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
