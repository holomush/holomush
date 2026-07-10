// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/samber/oops"
)

// Dead-letter stream defaults (CLUSTER-04, D-09/D-12).
const (
	// DefaultDLQStreamName is the JetStream stream that holds audit
	// messages which exhausted MaxDeliver. Its failure domain is
	// deliberately independent of Postgres (whose outage is the most
	// likely cause of audit dead letters, D-09).
	DefaultDLQStreamName = "EVENTS_AUDIT_DLQ"

	// defaultDLQGameID is the game-id token used when the subject prefix
	// is not overridden. core.go supplies the real game id.
	defaultDLQGameID = "main"

	// defaultDLQMaxAge bounds dead-letter retention so a poison flood
	// cannot exhaust storage (D-12). The Prometheus counter alerts
	// operators long before anything ages out (D-11).
	defaultDLQMaxAge = 30 * 24 * time.Hour
)

// defaultDLQSubject is the subject prefix dead letters are published
// under. It nests inside the CLUSTER-02 `internal.>` granted prefix so it
// stays within the holomush-server account's permissions.
const defaultDLQSubject = "internal." + defaultDLQGameID + ".audit.dlq"

// DLQConfig bounds and locates the audit dead-letter stream (D-12).
type DLQConfig struct {
	// StreamName is the JetStream stream name. Zero resolves to
	// DefaultDLQStreamName via Defaults().
	StreamName string

	// Subject is the dead-letter subject prefix (e.g.
	// "internal.<game_id>.audit.dlq"). Captured messages publish to
	// "<Subject>.<original-subject>" so the original event subject is
	// recoverable for replay. Zero resolves to defaultDLQSubject.
	Subject string

	// MaxAge caps how long dead letters are retained. Zero resolves to
	// defaultDLQMaxAge via Defaults().
	MaxAge time.Duration

	// MaxBytes caps the DLQ stream size in bytes. Zero means unbounded by
	// size (age-capped only) — mapped to the JetStream -1 sentinel at
	// stream-declare time.
	MaxBytes int64

	// Storage selects the DLQ stream's storage tier. The zero value is
	// jetstream.FileStorage (durable — the production default). Tests
	// override to jetstream.MemoryStorage for speed.
	Storage jetstream.StorageType
}

// Defaults fills any zero-valued DLQ field with its default. Storage is
// intentionally left untouched: its zero value is jetstream.FileStorage
// (the durable production default).
func (c DLQConfig) Defaults() DLQConfig {
	if c.StreamName == "" {
		c.StreamName = DefaultDLQStreamName
	}
	if c.Subject == "" {
		c.Subject = defaultDLQSubject
	}
	if c.MaxAge == 0 {
		c.MaxAge = defaultDLQMaxAge
	}
	return c
}

// dlqJetStream is the narrow JetStream surface the DLQ publisher needs.
// jetstream.JetStream satisfies it; unit tests fake it to exercise
// EnsureStream/Capture without a broker.
type dlqJetStream interface {
	CreateOrUpdateStream(ctx context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error)
	PublishMsg(ctx context.Context, msg *nats.Msg, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// dlqCapturer captures a single message to the dead-letter stream. The
// projection depends on this interface (not the concrete publisher) so
// the Term/Nak decision is unit-testable with a fake.
type dlqCapturer interface {
	Capture(ctx context.Context, msg jetstream.Msg) error
}

// dlqPublisher is the reusable DLQ capture helper (D-10). It idempotently
// provisions the bounded dead-letter stream and publishes captured
// messages header-preserving so Nats-Msg-Id survives for replay dedup.
// The plugin audit consumer reuses this helper in a documented follow-up.
type dlqPublisher struct {
	js      dlqJetStream
	cfg     DLQConfig
	counter prometheus.Counter
}

// newDLQPublisher builds a DLQ publisher over js with the given config.
// The counter is the package-global DLQMessagesTotal so every capture is
// observable regardless of construction site.
func newDLQPublisher(js dlqJetStream, cfg DLQConfig) *dlqPublisher {
	return &dlqPublisher{js: js, cfg: cfg.Defaults(), counter: DLQMessagesTotal}
}

// EnsureStream idempotently creates or updates the bounded dead-letter
// stream (D-12). Re-running with an unchanged config is a no-op success.
func (d *dlqPublisher) EnsureStream(ctx context.Context) error {
	maxBytes := d.cfg.MaxBytes
	if maxBytes == 0 {
		// JetStream uses -1 for "unbounded"; a literal 0 would reject
		// every publish.
		maxBytes = -1
	}
	_, err := d.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      d.cfg.StreamName,
		Subjects:  []string{d.cfg.Subject + ".>"},
		Retention: jetstream.LimitsPolicy,
		Storage:   d.cfg.Storage,
		Replicas:  1,
		MaxAge:    d.cfg.MaxAge,
		MaxBytes:  maxBytes,
	})
	if err != nil {
		return oops.Code("AUDIT_DLQ_STREAM_DECLARE_FAILED").
			With("stream", d.cfg.StreamName).
			Wrap(err)
	}
	return nil
}

// Capture publishes the full message (original subject encoded in the DLQ
// subject suffix, headers, and data) to the dead-letter stream, then
// increments DLQMessagesTotal. On publish failure it returns the error
// WITHOUT incrementing the counter so the caller can Nak (never drop, D-09).
//
// Headers are forwarded unchanged — Nats-Msg-Id and every audit header
// survive so a replay can reconstruct the events_audit row and JetStream
// dedup still applies.
func (d *dlqPublisher) Capture(ctx context.Context, msg jetstream.Msg) error {
	subject := d.cfg.Subject + "." + msg.Subject()
	_, err := d.js.PublishMsg(ctx, &nats.Msg{
		Subject: subject,
		Header:  msg.Headers(),
		Data:    msg.Data(),
	})
	if err != nil {
		return oops.Code("AUDIT_DLQ_PUBLISH_FAILED").
			With("dlq_subject", subject).
			Wrap(err)
	}
	d.counter.Inc()
	return nil
}
