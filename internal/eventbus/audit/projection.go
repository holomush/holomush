// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/pgnanos"
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
	// js is retained solely so AwaitDrained (a test helper) can resolve
	// the underlying stream to read its LastSeq. Avoids a cold-start race
	// where the consumer's pending count momentarily reports 0 before it
	// has synced with a just-published message.
	js     jetstream.JetStream
	pool   *pgxpool.Pool
	cfg    Config
	owners *OwnerMap // may be nil: nil ⇒ host owns every subject
	// dlq captures poison messages on the final delivery attempt so a
	// message exhausting MaxDeliver is never silently dropped (D-09).
	dlq dlqCapturer
	cc  jetstream.ConsumeContext
	// workerCtx is stored at start() time so persist() can derive its
	// Exec context from it. Subsystem.Stop cancels workerCtx; any
	// pending INSERT then cancels as well, so drain() cannot return
	// while writes are still in flight.
	workerCtx context.Context //nolint:containedctx // lifecycle ctx, not request ctx
}

// consumerCreateBackoffs is the retry schedule for CreateOrUpdateConsumer.
// Sized to absorb JetStream's brief warmup window where the meta-leader
// is elected and the events stream is queryable but the consumer-create
// RPC can still return "no responders" or context-deadline errors under
// load (observed flake holomush-l015 — admin/policy_chain BeforeEach
// surfaced AUDIT_CONSUMER_CREATE_FAILED once across ~3 task test:int
// runs while the host was at 1.5x normal wall time). Total worst-case
// wait before giving up: ~350ms — long enough to outlast typical warmup
// jitter, short enough that a real permanent failure (config mismatch,
// stream missing) still fails fast within the surrounding test timeout.
//
// Shared by newProjection (host audit) and PluginConsumerManager.Add
// (per-plugin consumers) — both invoke the same RPC against the same
// stream and so face the same warmup race.
//
// Declared `var` (not `const`) so projection_unit_test.go's
// withShortBackoffs(t) can swap it to a microsecond schedule for the
// retry tests. Tests in this package MUST NOT call t.Parallel() while
// the retry tests are present — concurrent t.Cleanup restores would
// race on the shared slice. The race detector would catch a violation,
// but the failure mode is non-obvious; prefer the comment as a guard.
var consumerCreateBackoffs = []time.Duration{
	100 * time.Millisecond,
	250 * time.Millisecond,
}

// newProjection creates or updates the durable consumer on the EVENTS
// stream. Durable consumers resume from the last-acked seq on restart,
// which is what makes this crash-safe.
//
// The CreateOrUpdateConsumer call is wrapped in a bounded retry to absorb
// transient JetStream warmup races (see consumerCreateBackoffs). On
// terminal failure the wrap surfaces the underlying NATS error as a
// structured `nats_err` field so callers (oops.Code() consumers,
// Gomega's Succeed() matcher) see the root cause and not just the
// AUDIT_CONSUMER_CREATE_FAILED code.
func newProjection(ctx context.Context, js jetstream.JetStream, pool *pgxpool.Pool, cfg Config) (*projection, error) {
	cons, err := createConsumerWithRetry(ctx, func(ctx context.Context) (jetstream.Consumer, error) {
		return js.CreateOrUpdateConsumer(ctx, eventbus.StreamName, jetstream.ConsumerConfig{
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
			// handful of poison messages could stall the projection. On the
			// final attempt handle() captures the message to EVENTS_AUDIT_DLQ
			// (see dlq.go) so it is preserved for operator inspection/replay
			// rather than dropped on max-deliver expiry (CLUSTER-04, D-09).
			MaxDeliver: cfg.MaxDeliver,
		})
	})
	if err != nil {
		return nil, wrapConsumerCreateError(err, eventbus.StreamName, cfg.ConsumerName)
	}
	// Provision the bounded dead-letter stream once at construction so the
	// final-attempt capture path (handle) can publish without a per-message
	// ensure. EnsureStream is idempotent (D-12).
	dlq := newDLQPublisher(js, cfg.DLQ)
	if err := dlq.EnsureStream(ctx); err != nil {
		return nil, oops.Code("AUDIT_DLQ_STREAM_INIT_FAILED").Wrap(err)
	}
	return &projection{consumer: cons, js: js, pool: pool, cfg: cfg, owners: cfg.Owners, dlq: dlq}, nil
}

// wrapConsumerCreateError applies the canonical AUDIT_CONSUMER_CREATE_FAILED
// wrap. Surfaces the underlying NATS error as a structured `nats_err`
// field so callers that read oops Code() / fields (Gomega's Succeed()
// matcher, the Ginkgo failure summary, errutil.AssertErrorContext) see
// the root cause and not just the holomush error code.
func wrapConsumerCreateError(err error, stream, consumer string) error {
	return oops.Code("AUDIT_CONSUMER_CREATE_FAILED").
		With("stream", stream).
		With("consumer", consumer).
		With("nats_err", err.Error()).
		Wrap(err)
}

// createConsumerWithRetry invokes create with bounded retries from
// consumerCreateBackoffs. Returns the first success, the last error
// after the budget is exhausted, or the last error if ctx is cancelled
// mid-backoff. Retries on any non-nil error — the cost of retrying a
// truly permanent error (config mismatch, missing stream) is bounded by
// the total backoff (~350ms) and the diagnostic cost of differentiating
// transient vs permanent error classes exceeds the savings.
//
// Shared by newProjection and PluginConsumerManager.Add. Both callers
// wrap the returned error with their own oops Code (the host wraps with
// AUDIT_CONSUMER_CREATE_FAILED via wrapConsumerCreateError; the plugin
// path wraps with AUDIT_PLUGIN_CONSUMER_CREATE_FAILED via
// wrapPluginConsumerCreateError in plugin_consumer.go).
func createConsumerWithRetry(ctx context.Context, create func(context.Context) (jetstream.Consumer, error)) (jetstream.Consumer, error) {
	var lastErr error
	for attempt := 0; attempt <= len(consumerCreateBackoffs); attempt++ {
		cons, err := create(ctx)
		if err == nil {
			return cons, nil
		}
		lastErr = err
		if attempt == len(consumerCreateBackoffs) {
			break
		}
		if ctx.Err() != nil {
			return nil, lastErr
		}
		select {
		case <-time.After(consumerCreateBackoffs[attempt]):
		case <-ctx.Done():
			return nil, lastErr
		}
	}
	return nil, lastErr
}

// start attaches the Consume callback synchronously so Subsystem.Start
// can surface any Consume failure to the orchestrator. On success it
// spawns a single goroutine that stops the consume context when the
// worker ctx is cancelled. The callback itself runs on the consumer's
// internal goroutine pool.
//
// p.workerCtx MUST be assigned before Consume registers the callback.
// JetStream may invoke p.handle (which calls persist, which reads
// p.workerCtx) on its own goroutine the moment Consume returns; setting
// the field after registration is a data race surfaced by sub-epic D's
// full-boot E2E under -race.
func (p *projection) start(ctx context.Context) error {
	p.workerCtx = ctx
	cc, err := p.consumer.Consume(p.handle)
	if err != nil {
		return oops.Code("AUDIT_CONSUME_FAILED").
			With("consumer", p.cfg.ConsumerName).
			Wrap(err)
	}
	p.cc = cc
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
		// Final-attempt DLQ capture (CLUSTER-04, D-09): once a message has
		// been delivered MaxDeliver times, JetStream will stop redelivering
		// it. Rather than let it drop on max-deliver expiry, capture it to
		// the bounded EVENTS_AUDIT_DLQ stream and Term the original. If the
		// DLQ publish itself fails, Nak instead so redelivery continues —
		// nothing is ever silently dropped. The DLQ's failure domain is
		// independent of Postgres (the most likely cause of dead letters).
		meta, mErr := msg.Metadata()
		if mErr == nil && p.cfg.MaxDeliver > 0 && meta.NumDelivered >= uint64(p.cfg.MaxDeliver) {
			if dlqErr := p.dlq.Capture(p.workerCtx, msg); dlqErr != nil {
				slog.ErrorContext(
					p.workerCtx,
					"audit DLQ capture failed; keeping message for redelivery",
					"subject", msg.Subject(),
					"num_delivered", meta.NumDelivered,
					"error", dlqErr.Error(),
				)
				_ = msg.Nak() //nolint:errcheck // Nak failures are absorbed by continued redelivery
				return
			}
			slog.WarnContext(
				p.workerCtx,
				"audit message exhausted MaxDeliver; captured to dead-letter stream",
				"subject", msg.Subject(),
				"num_delivered", meta.NumDelivered,
			)
			_ = msg.Term() //nolint:errcheck // Term failures are absorbed by max-deliver expiry
			return
		}
		// Below the cap (or metadata unavailable): deliberate no-ack.
		// JetStream will redeliver after AckWait. We do not Nak() because
		// Nak triggers INSTANT redelivery, which would storm the database
		// on persistent errors (e.g. DB down). AckWait-based redelivery
		// gives natural backoff.
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
//
// The header-parse + INSERT body lives in writeAuditRow so the DLQ
// replay path (replay.go) drives the SAME idempotent write; persist owns
// only the workerCtx-derived timeout so Subsystem.Stop can cancel
// in-flight INSERTs.
func (p *projection) persist(msg jetstream.Msg) error {
	// Derive persist ctx from workerCtx so Subsystem.Stop can cancel
	// in-flight INSERTs. Falls back to Background if persist runs before
	// start (defensive — shouldn't happen in normal lifecycle).
	parent := p.workerCtx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, persistTimeout)
	defer cancel()
	// On the live path msg.Subject() IS the original event subject.
	return writeAuditRow(ctx, p.pool, msg.Subject(), msg)
}

// writeAuditRow parses a JetStream message's audit headers and writes the
// corresponding events_audit row with ON CONFLICT (id) DO NOTHING. It is
// the single durable-write body shared by the live projection (persist)
// and the DLQ replay path (ReplayDLQ) so header parsing and idempotency
// stay byte-identical across both — a replayed dead letter reconstructs
// the exact row the live path would have written (CLUSTER-04, D-11).
//
// subject is the ORIGINAL event subject to store. On the live path it is
// msg.Subject() verbatim; on replay it is the original subject recovered
// from the DLQ subject suffix (DLQ capture wraps the original subject as
// <dlq-prefix>.<orig-subject>), so the recovered row's subject column
// matches what the live path would have written.
//
// Note: timestamp and js_seq derive from msg.Metadata(). On the live path
// that is the EVENTS stream's metadata; on replay it is the
// EVENTS_AUDIT_DLQ message's metadata (the original event's stream
// sequence/timestamp are not preserved by DLQ capture — only headers,
// data, and the subject-suffix survive). The dedup key is the
// header-carried Nats-Msg-Id (id column), which IS preserved, so
// idempotency holds regardless of which stream the message is read from.
func writeAuditRow(ctx context.Context, pool *pgxpool.Pool, subject string, msg jetstream.Msg) error {
	h := msg.Headers()

	msgID := h.Get(headerMsgID)
	if msgID == "" {
		return oops.Code("AUDIT_MISSING_HEADER").With("header", headerMsgID).Errorf("missing header")
	}
	// Shared header parser produces typed Codec / SchemaVer / DEKRef /
	// DEKVersion. The per-plugin dispatcher uses the same parser; INV-CRYPTO-39
	// byte-equality across the host-projection branch and the per-plugin
	// dispatcher branch is structural (single implementation feeds both).
	parsed, err := ParseAuditHeaders(h)
	if err != nil {
		return err
	}
	codec := parsed.Codec
	ver := parsed.SchemaVer
	dekRef := parsed.DEKRef
	dekVer := parsed.DEKVersion

	eventType := h.Get(headerEventType)
	if eventType == "" {
		return oops.Code("AUDIT_MISSING_HEADER").With("header", headerEventType).Errorf("missing header")
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

	_, err = pool.Exec(
		ctx, `
		INSERT INTO events_audit (
			id, subject, type, timestamp, actor_kind, actor_id,
			envelope, schema_ver, codec, js_seq, rendering,
			dek_ref, dek_version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (id) DO NOTHING`,
		idBytes,
		subject,
		eventType,
		pgnanos.From(meta.Timestamp),
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

// awaitDrained polls ConsumerInfo until the consumer's AckFloor has
// caught up to the stream's LastSeq AND there are no acks in flight, or
// until timeout. Anchoring to the stream's LastSeq (rather than only the
// consumer's NumPending) eliminates a cold-start race:
//
// When AwaitDrained is called immediately after a publish, the consumer's
// view can briefly report NumPending==0 and NumAckPending==0 before it
// has synced with the stream and observed the new message. A check that
// only looks at consumer pending counts returns "drained" in that window,
// causing tests to query the audit table before the INSERT has run
// (observed flake: TestProjectionDrainsPublishedMessageToAuditTable
// returning sql.ErrNoRows on projection_test.go:159, bd holomush-1nl7).
//
// Stream.Info() is authoritative for "is there a message?" — if a publish
// has been ack'd by the server, LastSeq reflects it. We wait until the
// consumer's AckFloor.Stream has advanced through that LastSeq, which
// requires the consumer to have observed AND acknowledged the message.
//
// Uses time.After (not time.Sleep) because the forbidigo linter bans
// time.Sleep across the eventbus package tree.
func (p *projection) awaitDrained(t AwaitT, timeout time.Duration) {
	t.Helper()
	stream, err := p.js.Stream(context.Background(), eventbus.StreamName)
	if err != nil {
		t.Fatalf("audit projection awaitDrained: stream lookup failed: %v", err)
		return
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cInfo, cErr := p.consumer.Info(context.Background())
		sInfo, sErr := stream.Info(context.Background())
		if cErr == nil && sErr == nil &&
			cInfo.NumAckPending == 0 &&
			cInfo.AckFloor.Stream >= sInfo.State.LastSeq {
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
