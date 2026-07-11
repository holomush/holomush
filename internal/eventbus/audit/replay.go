// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/samber/oops"
)

// replayFetchMaxWait bounds a single Fetch on the DLQ replay consumer.
// The ordered consumer delivers already-persisted (durable) DLQ messages,
// so a short wait is enough; the outer loop keeps fetching until the
// stream's message count has been scanned or the caller's context expires.
const replayFetchMaxWait = 500 * time.Millisecond

// ReplayOptions bounds a ReplayDLQ pass.
type ReplayOptions struct {
	// MsgID, when non-empty, replays only the dead letter whose
	// Nats-Msg-Id header equals it; all other scanned messages are left
	// untouched. Empty replays every message subject to Limit.
	MsgID string

	// Limit caps how many DLQ messages are scanned in one pass. Zero means
	// scan the whole stream. A positive Limit is a safety fence for very
	// large dead-letter backlogs, letting an operator drain in batches.
	Limit int
}

// ReplayResult summarizes a ReplayDLQ pass.
type ReplayResult struct {
	// Scanned is the number of DLQ messages read from the stream.
	Scanned int
	// Replayed is the number of messages written through the audit persist
	// path (idempotent — a message already present in events_audit still
	// counts as replayed because the write succeeded as a no-op).
	Replayed int
	// Skipped is the number of scanned messages that did not match the
	// MsgID filter (only non-zero when opts.MsgID is set).
	Skipped int
	// Failed is the number of messages that could not be persisted (e.g.
	// a genuinely-poison message missing a required header). Failed
	// messages are reported and retained in the DLQ, never dropped.
	Failed int
}

// ReplayDLQ re-drives dead letters from the EVENTS_AUDIT_DLQ stream back
// into events_audit through the SAME idempotent write the live projection
// uses (writeAuditRow). It is the recovery half of CLUSTER-04: once the
// underlying outage (usually Postgres) is fixed, replay restores captured
// messages so a dead letter is recoverable, not "nicer-looking data loss"
// (D-11).
//
// Idempotency: writeAuditRow uses ON CONFLICT (id) DO NOTHING keyed on the
// header-carried Nats-Msg-Id, which DLQ capture preserves byte-for-byte.
// Replaying the same message twice therefore yields exactly one row.
//
// Never-drop: the DLQ stream uses LimitsPolicy retention, so reading it
// (even acking) does not delete messages — they age out only via
// MaxAge/MaxBytes. A message that still fails to persist (genuinely
// poison) is counted in Failed and left in place for operator inspection,
// never consumed away.
//
// It dials neither the admin UDS nor any host service: replay needs only
// a JetStream handle (read the DLQ) and a Postgres pool (write
// events_audit).
func ReplayDLQ(
	ctx context.Context,
	js jetstream.JetStream,
	pool *pgxpool.Pool,
	cfg DLQConfig,
	opts ReplayOptions,
) (ReplayResult, error) {
	cfg = cfg.Defaults()
	var result ReplayResult

	stream, err := js.Stream(ctx, cfg.StreamName)
	if err != nil {
		return result, oops.Code("AUDIT_DLQ_REPLAY_STREAM_LOOKUP_FAILED").
			With("stream", cfg.StreamName).
			Wrap(err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return result, oops.Code("AUDIT_DLQ_REPLAY_STREAM_INFO_FAILED").
			With("stream", cfg.StreamName).
			Wrap(err)
	}

	budget := int(info.State.Msgs) //nolint:gosec // stream msg count is bounded well within int
	if opts.Limit > 0 && opts.Limit < budget {
		budget = opts.Limit
	}
	if budget == 0 {
		return result, nil
	}

	// Ephemeral ordered consumer over the DLQ, delivered from the start.
	// Ordered consumers are read-only: they never delete from a
	// LimitsPolicy stream, and each ReplayDLQ pass starts a fresh one from
	// the beginning — so a re-run re-reads every dead letter (idempotency
	// absorbs the duplicate writes).
	cons, err := stream.OrderedConsumer(ctx, jetstream.OrderedConsumerConfig{
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return result, oops.Code("AUDIT_DLQ_REPLAY_CONSUMER_FAILED").
			With("stream", cfg.StreamName).
			Wrap(err)
	}

	for result.Scanned < budget {
		if ctx.Err() != nil {
			return result, oops.Code("AUDIT_DLQ_REPLAY_CANCELLED").Wrap(ctx.Err())
		}
		wait := replayFetchMaxWait
		if d, ok := ctx.Deadline(); ok {
			remaining := time.Until(d)
			if remaining <= 0 {
				return result, oops.Code("AUDIT_DLQ_REPLAY_CANCELLED").Wrap(context.DeadlineExceeded)
			}
			if remaining < wait {
				wait = remaining
			}
		}

		batch, fetchErr := cons.Fetch(budget-result.Scanned, jetstream.FetchMaxWait(wait))
		if fetchErr != nil && !errors.Is(fetchErr, nats.ErrTimeout) {
			return result, oops.Code("AUDIT_DLQ_REPLAY_FETCH_FAILED").
				With("stream", cfg.StreamName).
				Wrap(fetchErr)
		}
		// Defensive: a timeout error is expected to come with a non-nil batch
		// (jetstream's current behavior), but guard against a nil batch so a
		// future client change cannot turn this into a nil-deref panic.
		if batch == nil {
			break
		}

		got := 0
		for msg := range batch.Messages() {
			got++
			result.Scanned++
			replayOne(ctx, pool, msg, opts.MsgID, cfg.Subject, &result)
		}
		if err := batch.Error(); err != nil && !errors.Is(err, nats.ErrTimeout) {
			return result, oops.Code("AUDIT_DLQ_REPLAY_FETCH_FAILED").
				With("stream", cfg.StreamName).
				Wrap(err)
		}
		if got == 0 {
			// No more messages available within the fetch window. The
			// stream reported budget messages up front; an empty fetch
			// means we have drained what is currently readable.
			break
		}
	}

	slog.InfoContext(
		ctx, "audit DLQ replay complete",
		"stream", cfg.StreamName,
		"scanned", result.Scanned,
		"replayed", result.Replayed,
		"skipped", result.Skipped,
		"failed", result.Failed,
	)
	return result, nil
}

// replayOne applies one DLQ message: skip on MsgID mismatch, else write it
// through the shared audit persist path and record the outcome. The
// message is acked to advance the ephemeral consumer; acking a
// LimitsPolicy stream does not delete the message, so a failed persist
// still leaves the dead letter in the DLQ.
//
// dlqSubjectPrefix is the DLQ subject prefix (cfg.Subject) used to recover
// the original event subject from the DLQ subject suffix, so the restored
// events_audit row carries the same subject the live path would have.
func replayOne(ctx context.Context, pool *pgxpool.Pool, msg jetstream.Msg, wantMsgID, dlqSubjectPrefix string, result *ReplayResult) {
	if wantMsgID != "" && msg.Headers().Get(headerMsgID) != wantMsgID {
		result.Skipped++
		_ = msg.Ack() //nolint:errcheck // ack advances the ephemeral cursor; LimitsPolicy retains the message
		return
	}
	subject, ok := originalSubject(msg.Subject(), dlqSubjectPrefix)
	if !ok {
		// The DLQ subject does not carry the expected prefix — replaying it
		// would persist a corrupted events_audit.subject (the DLQ prefix still
		// prepended). This happens when the replay config's game_id differs
		// from (or falls back to "main" instead of) the capture-time game_id.
		// Fail loud: count Failed and retain the dead letter, never write a
		// corrupted subject. (subject metadata is NOT an AAD input, so this is
		// crypto-neutral.)
		result.Failed++
		slog.WarnContext(
			ctx, "audit DLQ replay: DLQ subject does not carry the expected prefix; not persisted (replay game_id likely differs from capture-time game_id)",
			"subject", msg.Subject(),
			"expected_prefix", dlqSubjectPrefix,
			"msg_id", msg.Headers().Get(headerMsgID),
		)
		_ = msg.Ack() //nolint:errcheck // ack advances the cursor; the message stays in the LimitsPolicy DLQ for a corrected re-run
		return
	}
	if err := writeAuditRow(ctx, pool, subject, msg); err != nil {
		result.Failed++
		slog.WarnContext(
			ctx, "audit DLQ replay: message could not be persisted; retained in DLQ",
			"subject", msg.Subject(),
			"msg_id", msg.Headers().Get(headerMsgID),
			"error", err.Error(),
		)
		_ = msg.Ack() //nolint:errcheck // ack advances the cursor; the poison message stays in the LimitsPolicy DLQ
		return
	}
	result.Replayed++
	_ = msg.Ack() //nolint:errcheck // ack advances the cursor; ON CONFLICT DO NOTHING already made the write idempotent
}

// originalSubject recovers the original event subject from a DLQ message's
// subject. DLQ capture publishes to "<prefix>.<orig-subject>" (dlq.go
// Capture), so stripping "<prefix>." yields the original. It returns
// (stripped, true) on a clean prefix match. When the prefix does NOT match,
// it returns (dlqSubject, false) so the caller can fail loud rather than
// persist a subject with the DLQ prefix still prepended (silent corruption of
// the restored events_audit.subject column). An empty prefix cannot happen in
// replay (DLQConfig.Defaults resolves it), so it is treated as a mismatch.
func originalSubject(dlqSubject, prefix string) (string, bool) {
	if prefix == "" {
		return dlqSubject, false
	}
	if stripped, ok := strings.CutPrefix(dlqSubject, prefix+"."); ok {
		return stripped, true
	}
	return dlqSubject, false
}
