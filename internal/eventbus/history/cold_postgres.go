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

// Read satisfies ColdTier. Builds a parameterized SELECT against
// events_audit, honoring the subject filter, ULID cursor(s), time bounds,
// direction, and page size. An optional `edge` constraint is applied when
// the caller wants to restrict the cold tier to Timestamp < edge (the
// crossover case); pass the zero time to skip the constraint.
//
// The query uses positional placeholders with a dynamic parameter list so
// callers are never at risk of a SQL-injection surface — all user data
// flows through args, never into the SQL text.
func (c *postgresColdTier) Read(ctx context.Context, q eventbus.HistoryQuery, edge time.Time, pageSize int) ([]eventbus.Event, error) {
	if pageSize <= 0 {
		return nil, nil
	}
	subjectExact, subjectPattern := classifySubject(string(q.Subject))
	var (
		sb   strings.Builder
		args []any
	)
	sb.WriteString(`
		SELECT id, subject, type, timestamp, actor_kind, actor_id, payload
		FROM events_audit
		WHERE `)
	if subjectPattern != "" {
		args = append(args, subjectPattern)
		fmt.Fprintf(&sb, "subject LIKE $%d", len(args))
	} else {
		args = append(args, subjectExact)
		fmt.Fprintf(&sb, "subject = $%d", len(args))
	}

	// After: exclusive lower bound by ULID.
	//
	// TODO(holomush-suos): cold-tier pagination should key on
	// events_audit.js_seq, not ULID. Post-F8 cutover removed the global
	// writer, so id (a ULID) is no longer a safe proxy for JetStream
	// publish order across concurrent subjects. Using id >/< for cursors
	// and ORDER BY id can diverge from the hot JetStream tier ordering and
	// produce unstable crossover pagination. The js_seq column already
	// exists in migration 000009; the HistoryQuery cursor semantics and the
	// crossover tier-boundary logic both need to switch together.
	if !q.After.IsZero() {
		args = append(args, q.After[:])
		fmt.Fprintf(&sb, " AND id > $%d", len(args))
	}
	// Before: exclusive upper bound by ULID. See TODO(holomush-suos) above.
	if !q.Before.IsZero() {
		args = append(args, q.Before[:])
		fmt.Fprintf(&sb, " AND id < $%d", len(args))
	}
	// Inclusive time bounds.
	if !q.NotBefore.IsZero() {
		args = append(args, q.NotBefore)
		fmt.Fprintf(&sb, " AND timestamp >= $%d", len(args))
	}
	if !q.NotAfter.IsZero() {
		args = append(args, q.NotAfter)
		fmt.Fprintf(&sb, " AND timestamp <= $%d", len(args))
	}
	// Tier-boundary constraint: when the Reader is crossing from hot to
	// cold, this caps cold to events that are actually archived-only.
	// Passing the zero time disables the constraint (whole-archive query).
	if !edge.IsZero() {
		args = append(args, edge)
		fmt.Fprintf(&sb, " AND timestamp < $%d", len(args))
	}

	dir := q.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	if dir == eventbus.DirectionBackward {
		sb.WriteString(" ORDER BY id DESC")
	} else {
		sb.WriteString(" ORDER BY id ASC")
	}
	args = append(args, pageSize)
	fmt.Fprintf(&sb, " LIMIT $%d", len(args))

	rows, err := c.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, oops.Code("EVENTBUS_COLD_QUERY_FAILED").
			With("subject", string(q.Subject)).
			Wrap(err)
	}
	defer rows.Close()

	out := make([]eventbus.Event, 0, pageSize)
	for rows.Next() {
		var (
			idBytes      []byte
			subject      string
			eventType    string
			ts           time.Time
			actorKindStr string
			actorIDBytes []byte
			payload      []byte
		)
		if scanErr := rows.Scan(&idBytes, &subject, &eventType, &ts, &actorKindStr, &actorIDBytes, &payload); scanErr != nil {
			return nil, oops.Code("EVENTBUS_COLD_SCAN_FAILED").Wrap(scanErr)
		}
		if len(idBytes) != 16 {
			return nil, oops.Code("EVENTBUS_COLD_BAD_ID").
				With("len", len(idBytes)).
				Errorf("events_audit.id must be 16 bytes")
		}
		var id ulid.ULID
		copy(id[:], idBytes)
		out = append(out, eventbus.Event{
			ID:        id,
			Subject:   eventbus.Subject(subject),
			Type:      eventbus.Type(eventType),
			Timestamp: ts.UTC(),
			Actor:     actorFromAuditRow(actorKindStr, actorIDBytes),
			Payload:   payload,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("EVENTBUS_COLD_ROWS_ERR").Wrap(err)
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
