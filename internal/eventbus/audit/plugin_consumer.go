// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// PluginAuditClient is the narrow surface the per-plugin consumer uses to
// dispatch delivered messages to a plugin's PluginAuditService.AuditEvent
// RPC. The production implementation wraps the plugin's gRPC client; tests
// substitute fakes that record calls.
type PluginAuditClient interface {
	AuditEvent(ctx context.Context, req *pluginv1.AuditEventRequest) (*pluginv1.AuditEventResponse, error)
}

// PluginConsumerConfig configures one per-plugin audit consumer.
type PluginConsumerConfig struct {
	// PluginName is used to derive a stable durable consumer name so the
	// consumer resumes from the last-acked seq on restart.
	PluginName string

	// Subjects are the NATS subject patterns the plugin claims via its
	// manifest's audit block. MUST be non-empty. Every subject is passed
	// to JetStream's FilterSubjects so the consumer only sees messages
	// owned by this plugin (no host-side ack-and-skip loop).
	Subjects []string

	// Client dispatches deliveries to the plugin's PluginAuditService.
	// The dispatcher converts each JetStream message to an AuditEventRequest
	// and forwards headers verbatim.
	Client PluginAuditClient

	// AckWait bounds how long the server waits for ack before redelivery.
	// Defaults to DefaultAckWait when zero.
	AckWait time.Duration

	// MaxAckPending bounds in-flight messages awaiting ack. Defaults to
	// DefaultMaxAckPending when zero.
	MaxAckPending int

	// MaxDeliver caps redelivery attempts. Defaults to DefaultMaxDeliver
	// when zero.
	MaxDeliver int
}

// pluginConsumer is one instance of a per-plugin audit worker. It wraps a
// durable JetStream consumer and the plugin gRPC dispatcher, and handles
// the same ack/error semantics as the host projection (§6): no-ack on
// error (let AckWait + MaxDeliver govern redelivery), explicit Ack on
// successful plugin RPC.
type pluginConsumer struct {
	cfg      PluginConsumerConfig
	consumer jetstream.Consumer
	cc       jetstream.ConsumeContext
	// workerCtx is the ctx passed to dispatched RPCs so plugin-side
	// AuditEvent calls cancel when the consumer is drained.
	workerCtx context.Context //nolint:containedctx // lifecycle ctx, matches projection.go pattern
}

// PluginConsumerManager tracks multiple per-plugin consumers and starts /
// stops them as a unit. Matches the lifecycle contract of the host
// projection so subsystem.go can own a single drain call.
type PluginConsumerManager struct {
	mu        sync.Mutex
	js        jetstream.JetStream
	consumers []*pluginConsumer
	started   bool

	// keySelector is wired on the manager per INV-CRYPTO-45 (substrate-symmetry
	// with the hot-tier reader at internal/eventbus/history/tier.go) so
	// production wiring (cmd/holomush/core.go:488) can thread the same
	// codec.KeySelector instance to both. Phase 7's per-consumer dispatch
	// path (pluginConsumer.dispatch) does NOT consume the selector — it
	// forwards ciphertext byte-equal without invoking any codec
	// (INV-CRYPTO-46). The field exists so future dispatcher-side validation
	// (e.g. "refuse if dek_ref doesn't resolve" before forwarding) can
	// thread the selector without re-wiring the constructor.
	keySelector codec.KeySelector
}

// PluginConsumerManagerOption configures NewPluginConsumerManager.
type PluginConsumerManagerOption func(*PluginConsumerManager)

// WithKeySelector wires a codec.KeySelector onto the manager. The selector
// is substrate per INV-CRYPTO-45 — see PluginConsumerManager.keySelector for
// the in-Phase-7 semantics.
func WithKeySelector(sel codec.KeySelector) PluginConsumerManagerOption {
	return func(m *PluginConsumerManager) { m.keySelector = sel }
}

// NewPluginConsumerManager constructs a manager bound to js. js MUST be
// the same JetStream context the host projection uses — consumers share
// the EVENTS stream.
func NewPluginConsumerManager(js jetstream.JetStream, opts ...PluginConsumerManagerOption) *PluginConsumerManager {
	m := &PluginConsumerManager{js: js}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	return m
}

// wrapPluginConsumerCreateError applies the canonical
// AUDIT_PLUGIN_CONSUMER_CREATE_FAILED wrap. Mirrors
// wrapConsumerCreateError (host-projection variant in projection.go),
// surfacing the underlying NATS error as a structured `nats_err`
// field so oops Code() / field readers (Gomega's Succeed() matcher,
// Ginkgo failure summary, errutil.AssertErrorContext) see the root
// cause and not just the holomush error code.
//
// Includes the implicit `stream` field for symmetry with the host
// wrap — both surfaces target eventbus.StreamName ("EVENTS") on the
// same JetStream, so log-search and dashboard panels can filter by a
// single `stream` field across both audit-consumer-create call sites.
func wrapPluginConsumerCreateError(err error, pluginName, consumer string) error {
	return oops.Code("AUDIT_PLUGIN_CONSUMER_CREATE_FAILED").
		With("stream", eventbus.StreamName).
		With("plugin", pluginName).
		With("consumer", consumer).
		With("nats_err", err.Error()).
		Wrap(err)
}

// Add creates (or updates) a durable consumer for the given plugin block
// and registers its dispatcher. MUST be called before Start.
func (m *PluginConsumerManager) Add(ctx context.Context, cfg PluginConsumerConfig) error {
	if cfg.PluginName == "" {
		return oops.Code("AUDIT_PLUGIN_CONSUMER_INVALID_CONFIG").Errorf("PluginName required")
	}
	if len(cfg.Subjects) == 0 {
		return oops.Code("AUDIT_PLUGIN_CONSUMER_INVALID_CONFIG").
			With("plugin", cfg.PluginName).
			Errorf("at least one subject required")
	}
	if cfg.Client == nil {
		return oops.Code("AUDIT_PLUGIN_CONSUMER_INVALID_CONFIG").
			With("plugin", cfg.PluginName).
			Errorf("PluginAuditClient required")
	}

	// Check started before calling CreateOrUpdateConsumer so that late
	// Add() calls don't create orphaned durable consumers on the JetStream
	// server. TOCTOU window between this check and the append below is
	// acceptable: Start is called once after all Adds complete.
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return oops.Code("AUDIT_PLUGIN_CONSUMER_INVALID_STATE").
			With("plugin", cfg.PluginName).
			Errorf("cannot add plugin consumer after Start")
	}
	m.mu.Unlock()

	ackWait := cfg.AckWait
	if ackWait == 0 {
		ackWait = DefaultAckWait
	}
	maxAckPending := cfg.MaxAckPending
	if maxAckPending == 0 {
		maxAckPending = DefaultMaxAckPending
	}
	maxDeliver := cfg.MaxDeliver
	if maxDeliver == 0 {
		maxDeliver = DefaultMaxDeliver
	}

	name := pluginDurableName(cfg.PluginName)
	// Route through createConsumerWithRetry so plugin Add() shares the
	// JetStream-warmup retry behavior newProjection guards against —
	// same RPC, same js, same EVENTS stream. Prophylactic: no flake has
	// been observed on the plugin path (l015's empirical observation
	// was on the host projection), but the failure surface is
	// structurally identical, so the retry preserves symmetry rather
	// than papering over a tracked regression. Plugin Add() runs after
	// the host projection's consumer create in production wiring, so
	// warmup is typically already absorbed by the time we get here
	// (holomush-ghg1 follow-up to l015).
	cons, err := createConsumerWithRetry(ctx, func(ctx context.Context) (jetstream.Consumer, error) {
		return m.js.CreateOrUpdateConsumer(ctx, eventbus.StreamName, jetstream.ConsumerConfig{
			Durable:        name,
			Name:           name,
			FilterSubjects: cfg.Subjects,
			AckPolicy:      jetstream.AckExplicitPolicy,
			AckWait:        ackWait,
			MaxAckPending:  maxAckPending,
			MaxDeliver:     maxDeliver,
		})
	})
	if err != nil {
		return wrapPluginConsumerCreateError(err, cfg.PluginName, name)
	}

	pc := &pluginConsumer{cfg: cfg, consumer: cons}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		// Second-layer check closes the TOCTOU window: Start may have run
		// concurrently while CreateOrUpdateConsumer was in flight. Surface
		// the misuse instead of stranding a registration that would never
		// be attached to a Consume loop.
		return oops.Code("AUDIT_PLUGIN_CONSUMER_INVALID_STATE").
			With("plugin", cfg.PluginName).
			Errorf("cannot add plugin consumer after Start")
	}
	// Idempotent by plugin name: re-adding the same plugin replaces the
	// existing entry rather than creating a duplicate durable binding.
	for i, existing := range m.consumers {
		if existing.cfg.PluginName == cfg.PluginName {
			m.consumers[i] = pc
			return nil
		}
	}
	m.consumers = append(m.consumers, pc)
	return nil
}

// Start attaches the Consume callback for every registered consumer. Any
// failure rolls back started consumers so Start is all-or-nothing.
func (m *PluginConsumerManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return nil
	}
	for _, pc := range m.consumers {
		pc.workerCtx = ctx
		cc, err := pc.consumer.Consume(pc.handle)
		if err != nil {
			// Rollback: stop previously-started consumers AND wait for
			// each to drain so in-flight callbacks don't outlive the
			// failed Start. Mirrors the drain pattern used in Stop.
			for _, started := range m.consumers {
				if started.cc != nil {
					started.cc.Stop()
					select {
					case <-started.cc.Closed():
					case <-time.After(DefaultDrainTimeout):
					case <-ctx.Done():
					}
					started.cc = nil
				}
			}
			return oops.Code("AUDIT_PLUGIN_CONSUME_FAILED").
				With("plugin", pc.cfg.PluginName).
				Wrap(err)
		}
		pc.cc = cc
	}
	m.started = true
	return nil
}

// Stop drains every consumer's in-flight handlers. Bounded by
// DefaultDrainTimeout per consumer so a single slow plugin cannot block
// shutdown indefinitely.
func (m *PluginConsumerManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return nil
	}
	var firstErr error
	for _, pc := range m.consumers {
		if pc.cc == nil {
			continue
		}
		pc.cc.Stop()
		select {
		case <-pc.cc.Closed():
		case <-time.After(DefaultDrainTimeout):
			if firstErr == nil {
				firstErr = oops.Code("AUDIT_PLUGIN_DRAIN_TIMEOUT").
					With("plugin", pc.cfg.PluginName).
					With("timeout", DefaultDrainTimeout.String()).
					Errorf("plugin audit consumer drain exceeded %s", DefaultDrainTimeout)
			}
		case <-ctx.Done():
			if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) && firstErr == nil {
				firstErr = oops.Code("AUDIT_PLUGIN_DRAIN_CTX").
					With("plugin", pc.cfg.PluginName).
					Wrap(err)
			}
			return firstErr
		}
		pc.cc = nil
	}
	m.started = false
	return firstErr
}

// Consumers returns the count of registered consumers. Test-helper only.
func (m *PluginConsumerManager) Consumers() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.consumers)
}

// handle is the Consume callback. Mirrors projection.handle exactly for
// the error / ack contract: on error we deliberately do NOT Nak — AckWait
// handles redelivery with natural backoff. On success we Ack.
func (pc *pluginConsumer) handle(msg jetstream.Msg) {
	if err := pc.dispatch(msg); err != nil {
		slog.Default().Error(
			"plugin audit dispatch failed; relying on JetStream redelivery",
			"plugin", pc.cfg.PluginName,
			"subject", msg.Subject(),
			"error", err,
		)
		return
	}
	_ = msg.Ack() //nolint:errcheck // ack failures absorbed by redelivery + idempotent plugin INSERT
}

// dispatch converts a JetStream delivery into a PluginAuditService RPC
// and forwards it. The plugin is expected to INSERT idempotently; a
// retried delivery on the host side therefore produces zero duplicate
// plugin rows.
//
// Per Phase 7 spec §3 + §5.1 (INV-CRYPTO-38, INV-CRYPTO-46): the dispatcher
// forwards ciphertext byte-equal — it does NOT decrypt the envelope's
// payload before forwarding. The envelope projection fields (id,
// subject, type, timestamp, actor) are read from a no-decryption
// proto.Unmarshal — those fields are cleartext regardless of codec
// (the codec encrypts only the Payload field in place; verified at
// publisher.go:266-292 + hot_jetstream.go:441-444).
func (pc *pluginConsumer) dispatch(msg jetstream.Msg) error {
	h := msg.Headers()

	msgID := h.Get(headerMsgID)
	if msgID == "" {
		return oops.Code("AUDIT_PLUGIN_MISSING_HEADER").With("header", headerMsgID).Errorf("missing header")
	}
	if _, err := decodeULIDString(msgID); err != nil {
		return oops.Code("AUDIT_PLUGIN_BAD_MSG_ID").With("msg_id", msgID).Wrap(err)
	}

	row, err := buildAuditRow(msg)
	if err != nil {
		return err
	}

	parent := pc.workerCtx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, persistTimeout)
	defer cancel()

	if _, err := pc.cfg.Client.AuditEvent(ctx, &pluginv1.AuditEventRequest{Row: row}); err != nil {
		return oops.Code("AUDIT_PLUGIN_DISPATCH_FAILED").
			With("plugin", pc.cfg.PluginName).
			With("subject", msg.Subject()).
			Wrap(err)
	}
	return nil
}

// buildAuditRow constructs the AuditRow forwarded to the plugin's
// AuditEvent RPC. NEVER decrypts (INV-CRYPTO-46). Payload bytes are
// preserved byte-equal from the bus envelope (INV-CRYPTO-38).
//
// Wire-format invariant (verified at publisher.go:266-292 + hot_jetstream.go:441-444):
// the codec encrypts the event.Payload field in place; cleartext
// envelope metadata fields (id, subject, type, timestamp, actor) are
// NOT encrypted; msg.Data() is the marshaled envelope proto. Therefore
// `Payload: envelope.GetPayload()` correctly captures ciphertext for
// encrypted events and plaintext for identity-codec events — both
// byte-equal to what's stored in events_audit for host-owned subjects.
func buildAuditRow(msg jetstream.Msg) (*pluginv1.AuditRow, error) {
	hdrMeta, err := ParseAuditHeaders(msg.Headers())
	if err != nil {
		return nil, oops.Code("AUDIT_PLUGIN_HEADER_PARSE_FAILED").Wrap(err)
	}

	// Unmarshal the envelope ONLY to read projection fields. We do NOT
	// invoke the codec's Decode here — the projection fields are
	// cleartext regardless of codec, and decrypting would violate
	// INV-CRYPTO-46 (no decrypt before forward).
	envelope, err := unmarshalProjectionOnly(msg.Data())
	if err != nil {
		return nil, oops.Code("AUDIT_PLUGIN_ENVELOPE_UNMARSHAL_FAILED").Wrap(err)
	}

	row := &pluginv1.AuditRow{
		Id:        envelope.GetId(),
		Subject:   envelope.GetSubject(),
		Type:      envelope.GetType(),
		Timestamp: envelope.GetTimestamp(),
		Actor:     envelope.GetActor(),
		Codec:     hdrMeta.Codec,
		Payload:   envelope.GetPayload(), // ciphertext when codec != identity
		SchemaVer: hdrMeta.SchemaVer,
	}
	if hdrMeta.DEKRef != nil {
		v := uint64(*hdrMeta.DEKRef) //nolint:gosec // dek_ref originates as crypto_keys.id (BIGSERIAL, always >= 0); int64→uint64 widening is safe
		row.DekRef = &v
	}
	if hdrMeta.DEKVersion != nil {
		v := uint32(*hdrMeta.DEKVersion) //nolint:gosec // dek_version originates as a 1-based counter (always >= 0); int32→uint32 is safe
		row.DekVersion = &v
	}
	return row, nil
}

// unmarshalProjectionOnly proto-unmarshals msg.Data() into eventbusv1.Event.
// Per the codec contract (internal/eventbus/codec/xchacha20poly1305_v1.go),
// the codec encrypts the Event.payload field in-place — projection
// fields are always cleartext in the envelope, regardless of codec.
// We re-use proto.Unmarshal directly here rather than invoking the
// codec's Decode (which would decrypt).
func unmarshalProjectionOnly(data []byte) (*eventbusv1.Event, error) {
	var ev eventbusv1.Event
	if err := proto.Unmarshal(data, &ev); err != nil {
		return nil, err //nolint:wrapcheck // wrapped by caller with AUDIT_PLUGIN_ENVELOPE_UNMARSHAL_FAILED
	}
	return &ev, nil
}

// pluginDurableName derives the JetStream durable consumer name. Matches
// the spec §6b naming convention: plugin_audit_<name>. The plugin name is
// already constrained by the manifest pattern (^[a-z](-?[a-z0-9])*$) to a
// safe identifier, so no further sanitisation is required.
func pluginDurableName(pluginName string) string {
	return "plugin_audit_" + pluginName
}

// ---------------------------------------------------------------------------
// PluginHistoryRouter implementation.
// ---------------------------------------------------------------------------

// PluginHistoryQueryClient is the subset of PluginAuditService used by the
// history router — just the server-streaming QueryHistory RPC. Kept narrow
// so tests can substitute fakes without implementing the full service.
type PluginHistoryQueryClient interface {
	QueryHistory(ctx context.Context, req *pluginv1.QueryHistoryRequest) (PluginHistoryStream, error)
}

// PluginHistoryStream is the per-call streaming interface the router
// consumes. Mirrors the PluginAuditService_QueryHistoryClient surface
// needed by the history package.
type PluginHistoryStream interface {
	Recv() (*pluginv1.QueryHistoryResponse, error)
	Close() error
}
