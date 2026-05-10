// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
)

// Header names copied from the spec (§5). Keep in sync with the publisher
// side; mismatches cause the projection to reject messages.
const (
	headerMsgID         = "Nats-Msg-Id"
	headerCodec         = "App-Codec"
	headerEventType     = "App-Event-Type"
	headerSchemaVersion = "App-Schema-Version"
	headerActorKind     = "App-Actor-Kind"
	headerActorID       = "App-Actor-ID"
	headerRendering     = "App-Rendering"
)

// Phase A has no real actors publishing events; every event is emitted
// by the host process itself. When App-Actor-Kind is absent, default to
// this value.
const defaultActorKind = "system"

// persistTimeout bounds how long a single INSERT is allowed to take.
// On timeout the handler returns without acking; the server redelivers
// after AckWait. MUST stay ≤ DefaultDrainTimeout so in-flight INSERTs
// cannot outlive Subsystem.Stop.
const persistTimeout = DefaultDrainTimeout

// projection holds the durable pull consumer and the INSERT loop.
type projection struct {
	consumer jetstream.Consumer
	pool     *pgxpool.Pool
	cfg      Config
	owners   *OwnerMap // may be nil: nil ⇒ host owns every subject
	cc       jetstream.ConsumeContext
	// workerCtx is stored at start() time so persist() can derive its
	// Exec context from it. Subsystem.Stop cancels workerCtx; any
	// pending INSERT then cancels as well, so drain() cannot return
	// while writes are still in flight.
	workerCtx context.Context //nolint:containedctx // lifecycle ctx, not request ctx
}

// newProjection creates or updates the durable consumer on the EVENTS
// stream. Durable consumers resume from the last-acked seq on restart,
// which is what makes this crash-safe.
func newProjection(ctx context.Context, js jetstream.JetStream, pool *pgxpool.Pool, cfg Config) (*projection, error) {
	cons, err := js.CreateOrUpdateConsumer(ctx, eventbus.StreamName, jetstream.ConsumerConfig{
		Durable:       cfg.ConsumerName,
		Name:          cfg.ConsumerName,
		FilterSubject: eventbus.SubjectFilter,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       cfg.AckWait,
		MaxAckPending: cfg.MaxAckPending,
		// MaxDeliver caps redelivery attempts. Without it, a permanent
		// persist failure (AUDIT_MISSING_HEADER, AUDIT_BAD_SCHEMA_VERSION,
		// AUDIT_BAD_MSG_ID, BYTEA-incompatible DB row) would redeliver
		// forever and permanently consume a MaxAckPending slot — a
		// handful of poison messages could stall the projection.
		// Phase A TODO: wire a DLQ (e.g., stream 'EVENTS_AUDIT_DLQ') so
		// messages that exhaust MaxDeliver are preserved for operator
		// inspection rather than dropped on max-deliver expiry.
		MaxDeliver: cfg.MaxDeliver,
	})
	if err != nil {
		return nil, oops.Code("AUDIT_CONSUMER_CREATE_FAILED").
			With("stream", eventbus.StreamName).
			With("consumer", cfg.ConsumerName).
			Wrap(err)
	}
	return &projection{consumer: cons, pool: pool, cfg: cfg, owners: cfg.Owners}, nil
}

// start attaches the Consume callback synchronously so Subsystem.Start
// can surface any Consume failure to the orchestrator. On success it
// spawns a single goroutine that stops the consume context when the
// worker ctx is cancelled. The callback itself runs on the consumer's
// internal goroutine pool.
func (p *projection) start(ctx context.Context) error {
	cc, err := p.consumer.Consume(p.handle)
	if err != nil {
		return oops.Code("AUDIT_CONSUME_FAILED").
			With("consumer", p.cfg.ConsumerName).
			Wrap(err)
	}
	p.cc = cc
	p.workerCtx = ctx
	go func() {
		<-ctx.Done()
		cc.Stop()
	}()
	return nil
}

// handle is the Consume callback.
//
// Plugin-owned subjects (per the OwnerMap) are ack-and-skipped: the host
// MUST advance its consumer cursor past them so retention can age them
// out at the stream level, but MUST NOT persist them — the per-plugin
// consumer registered in F5 projects those messages to plugin-owned
// audit schemas independently.
//
// JetStream FilterSubjects does not support exclusion natively, so the
// host consumer stays subscribed to `events.>` and exclusion happens
// here in the handler.
//
// Host-owned subjects flow to persist(); the error path deliberately
// omits an Ack so JetStream redelivers after AckWait (see persist
// comment for the rationale — no Nak avoids instant-redeliver storms).
func (p *projection) handle(msg jetstream.Msg) {
	// Make the nil-OwnerMap contract explicit: a nil owners map means the
	// host owns every subject, so skip the plugin-ownership lookup entirely.
	// OwnerMap.Resolve is nil-safe today, but relying on that invariant
	// remotely would leave a nil-receiver trap if the implementation
	// changes.
	if p.owners != nil {
		if owner := p.owners.Resolve(msg.Subject()); owner.PluginName != "" {
			// Low-signal per-message event; debug-only to keep log volume
			// bounded. Plugin-owned audit coverage is observable via the
			// SkippedPluginOwnedTotal counter instead.
			slog.Default().Debug(
				"audit projection skipping plugin-owned subject",
				"subject", msg.Subject(),
				"plugin", owner.PluginName,
			)
			SkippedPluginOwnedTotal.WithLabelValues(owner.PluginName).Inc()
			_ = msg.Ack() //nolint:errcheck // ack failures are absorbed by redelivery
			return
		}
	}
	if err := p.persist(msg); err != nil {
		// Deliberate no-ack: JetStream will redeliver after AckWait.
		// We do not Nak() because Nak triggers INSTANT redelivery,
		// which would storm the database on persistent errors (e.g.
		// DB down). AckWait-based redelivery gives natural backoff.
		return
	}
	// Ack errors here are transient network/protocol errors; the server
	// will redeliver if the ack never arrives, and the idempotent
	// INSERT will absorb the retry. Nothing useful to do on error
	// besides retry, which the redelivery gives us for free.
	_ = msg.Ack() //nolint:errcheck // ack failures are absorbed by redelivery + idempotent INSERT
}

// persist writes one audit row. Uses ON CONFLICT DO NOTHING for
// idempotency — if the same Nats-Msg-Id is delivered twice (e.g. on
// restart before the previous ack reached the server), the second
// INSERT becomes a no-op and we still ack.
//
// Phase A note: only system-actor events flow through here. Phase B
// will emit a real ULID in App-Actor-ID for user-initiated events; the
// code below decodes that header if present but tolerates its absence.
func (p *projection) persist(msg jetstream.Msg) error {
	h := msg.Headers()

	msgID := h.Get(headerMsgID)
	if msgID == "" {
		return oops.Code("AUDIT_MISSING_HEADER").With("header", headerMsgID).Errorf("missing header")
	}
	codec := h.Get(headerCodec)
	if codec == "" {
		return oops.Code("AUDIT_MISSING_HEADER").With("header", headerCodec).Errorf("missing header")
	}
	eventType := h.Get(headerEventType)
	if eventType == "" {
		return oops.Code("AUDIT_MISSING_HEADER").With("header", headerEventType).Errorf("missing header")
	}
	schemaVer := h.Get(headerSchemaVersion)
	if schemaVer == "" {
		return oops.Code("AUDIT_MISSING_HEADER").With("header", headerSchemaVersion).Errorf("missing header")
	}
	// ParseInt with bitSize=16 returns int64 that fits in int16; the
	// events_audit.schema_ver column is SMALLINT. Using strconv.ParseInt
	// directly (rather than Atoi + cast) avoids a downcast warning while
	// still rejecting out-of-range values at the boundary.
	ver, err := strconv.ParseInt(schemaVer, 10, 16)
	if err != nil {
		return oops.Code("AUDIT_BAD_SCHEMA_VERSION").With("value", schemaVer).Wrap(err)
	}
	renderingJSON := h.Get(headerRendering)
	if renderingJSON == "" {
		return oops.Code("AUDIT_MISSING_HEADER").
			With("header", headerRendering).
			Errorf("missing header")
	}

	meta, err := msg.Metadata()
	if err != nil {
		return oops.Code("AUDIT_METADATA_FAILED").Wrap(err)
	}

	actorKind := h.Get(headerActorKind)
	if actorKind == "" {
		actorKind = defaultActorKind
	}

	// Phase A: only system-actor events flow, so actor_id is usually nil.
	// Phase B will emit a real ULID in App-Actor-ID. Tolerate its absence;
	// if present, it MUST parse — a malformed ULID is a publisher contract
	// violation and MUST be rejected rather than silently attributed to
	// system (which would corrupt the audit trail).
	var actorID []byte
	if v := h.Get(headerActorID); v != "" {
		parsed, parseErr := ulid.Parse(v)
		if parseErr != nil {
			return oops.Code("AUDIT_BAD_ACTOR_ID").With("value", v).Wrap(parseErr)
		}
		b := parsed.Bytes()
		actorID = b
	}

	idBytes, err := decodeULIDString(msgID)
	if err != nil {
		return oops.Code("AUDIT_BAD_MSG_ID").With("msg_id", msgID).Wrap(err)
	}

	// Phase 3a: parse optional App-Dek-Ref and App-Dek-Version headers.
	// Both are absent for codec=identity rows; nil pointers below write
	// SQL NULL via pgx nullable handling.
	var dekRef *int64
	if v := h.Get(eventbus.HeaderDekRef); v != "" {
		parsed, parseErr := strconv.ParseInt(v, 10, 64)
		if parseErr != nil {
			return oops.Code("AUDIT_DEK_REF_PARSE_FAILED").
				With("header", eventbus.HeaderDekRef).
				With("value", v).
				Wrap(parseErr)
		}
		dekRef = &parsed
	}
	var dekVer *int32
	if v := h.Get(eventbus.HeaderDekVersion); v != "" {
		parsed, parseErr := strconv.ParseInt(v, 10, 32)
		if parseErr != nil {
			return oops.Code("AUDIT_DEK_VERSION_PARSE_FAILED").
				With("header", eventbus.HeaderDekVersion).
				With("value", v).
				Wrap(parseErr)
		}
		v32 := int32(parsed)
		dekVer = &v32
	}

	// Derive persist ctx from workerCtx so Subsystem.Stop can cancel
	// in-flight INSERTs. Falls back to Background if persist runs before
	// start (defensive — shouldn't happen in normal lifecycle).
	parent := p.workerCtx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, persistTimeout)
	defer cancel()

	_, err = p.pool.Exec(
		ctx, `
		INSERT INTO events_audit (
			id, subject, type, timestamp, actor_kind, actor_id,
			envelope, schema_ver, codec, js_seq, rendering,
			dek_ref, dek_version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (id) DO NOTHING`,
		idBytes,
		msg.Subject(),
		eventType,
		meta.Timestamp,
		actorKind,
		actorID,
		msg.Data(),
		ver,
		codec,
		meta.Sequence.Stream,
		renderingJSON,
		dekRef,
		dekVer,
	)
	if err != nil {
		return oops.Code("AUDIT_INSERT_FAILED").Wrap(err)
	}
	return nil
}

// drain stops the Consume context and waits for in-flight handlers to
// finish. Honors ctx cancellation so orchestrator shutdown deadlines
// propagate. Returns a coded error on timeout so the caller can
// distinguish "drained cleanly" from "gave up waiting" — a silent
// success here would hide production incidents where the Consume loop
// is blocked on a slow DB.
func (p *projection) drain(ctx context.Context) error {
	if p.cc == nil {
		return nil
	}
	p.cc.Stop()
	select {
	case <-p.cc.Closed():
		return nil
	case <-time.After(DefaultDrainTimeout):
		return oops.Code("AUDIT_DRAIN_TIMEOUT").
			With("timeout", DefaultDrainTimeout.String()).
			Errorf("audit projection drain exceeded %s", DefaultDrainTimeout)
	case <-ctx.Done():
		if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
			return oops.Code("AUDIT_DRAIN_CTX").Wrap(err)
		}
		return nil
	}
}

// awaitDrained polls ConsumerInfo until NumPending and NumAckPending are
// both zero, or until timeout. Uses time.After (not time.Sleep) because
// forbidigo bans time.Sleep across the eventbus package tree.
func (p *projection) awaitDrained(t AwaitT, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		info, err := p.consumer.Info(context.Background())
		if err == nil && info.NumPending == 0 && info.NumAckPending == 0 {
			return
		}
		<-time.After(AwaitPollInterval)
	}
	t.Fatalf("audit projection did not drain within %s", timeout)
}

// decodeULIDString parses a canonical 26-char ULID string into its 16
// raw bytes, suitable for storing in a BYTEA column.
func decodeULIDString(s string) ([]byte, error) {
	u, err := ulid.Parse(s)
	if err != nil {
		return nil, oops.Code("AUDIT_BAD_ULID").Wrap(err)
	}
	b := u.Bytes()
	return b, nil
}
