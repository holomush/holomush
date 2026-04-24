// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
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
}

// NewPluginConsumerManager constructs a manager bound to js. js MUST be
// the same JetStream context the host projection uses — consumers share
// the EVENTS stream.
func NewPluginConsumerManager(js jetstream.JetStream) *PluginConsumerManager {
	return &PluginConsumerManager{js: js}
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
	cons, err := m.js.CreateOrUpdateConsumer(ctx, eventbus.StreamName, jetstream.ConsumerConfig{
		Durable:        name,
		Name:           name,
		FilterSubjects: cfg.Subjects,
		AckPolicy:      jetstream.AckExplicitPolicy,
		AckWait:        ackWait,
		MaxAckPending:  maxAckPending,
		MaxDeliver:     maxDeliver,
	})
	if err != nil {
		return oops.Code("AUDIT_PLUGIN_CONSUMER_CREATE_FAILED").
			With("plugin", cfg.PluginName).
			With("consumer", name).
			Wrap(err)
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
// The raw msg.Data() is the codec-encoded proto envelope (potentially
// encrypted). Plugin audit storage MUST receive the original payload, not
// the envelope/ciphertext, so dispatch decodes the envelope first and
// populates the AuditEventRequest.Event from the decoded fields (including
// the publisher-stamped Timestamp, not the JetStream metadata timestamp).
func (pc *pluginConsumer) dispatch(msg jetstream.Msg) error {
	h := msg.Headers()

	msgID := h.Get(headerMsgID)
	if msgID == "" {
		return oops.Code("AUDIT_PLUGIN_MISSING_HEADER").With("header", headerMsgID).Errorf("missing header")
	}
	if _, err := decodeULIDString(msgID); err != nil {
		return oops.Code("AUDIT_PLUGIN_BAD_MSG_ID").With("msg_id", msgID).Wrap(err)
	}

	headers := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	envelope, err := decodeEnvelope(h, msg.Data())
	if err != nil {
		return oops.Code("AUDIT_PLUGIN_DECODE_FAILED").
			With("plugin", pc.cfg.PluginName).
			With("subject", msg.Subject()).
			Wrap(err)
	}

	parent := pc.workerCtx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, persistTimeout)
	defer cancel()

	_, err = pc.cfg.Client.AuditEvent(ctx, &pluginv1.AuditEventRequest{
		Event:   envelope,
		Headers: headers,
	})
	if err != nil {
		return oops.Code("AUDIT_PLUGIN_DISPATCH_FAILED").
			With("plugin", pc.cfg.PluginName).
			With("subject", msg.Subject()).
			Wrap(err)
	}
	return nil
}

// decodeEnvelope decodes the JetStream message data into an eventbusv1.Event.
// The data is codec-encoded proto envelope bytes; this resolves the codec by
// the App-Codec header, calls Decode, then proto.Unmarshal. Only the identity
// codec is supported here because PluginConsumerManager has no KeySelector
// wired yet — a non-identity codec returns an error so misconfigurations
// surface at dispatch time rather than forwarding ciphertext to the plugin.
func decodeEnvelope(h nats.Header, data []byte) (*eventbusv1.Event, error) {
	codecNameStr := h.Get(headerCodec)
	if codecNameStr == "" {
		return nil, oops.Code("AUDIT_PLUGIN_MISSING_HEADER").
			With("header", headerCodec).
			Errorf("missing header")
	}
	if codec.Name(codecNameStr) != codec.NameIdentity {
		return nil, oops.Code("AUDIT_PLUGIN_CODEC_UNSUPPORTED").
			With("codec", codecNameStr).
			Errorf("plugin audit dispatcher does not have a KeySelector; only identity codec is supported")
	}
	c, err := codec.Resolve(codec.Name(codecNameStr))
	if err != nil {
		return nil, oops.Code("AUDIT_PLUGIN_CODEC_UNKNOWN").
			With("codec", codecNameStr).
			Wrap(err)
	}
	plain, err := c.Decode(context.Background(), data, codec.Key{})
	if err != nil {
		return nil, oops.Code("AUDIT_PLUGIN_CODEC_DECODE_FAILED").
			With("codec", codecNameStr).
			Wrap(err)
	}
	var envelope eventbusv1.Event
	if unmarshalErr := proto.Unmarshal(plain, &envelope); unmarshalErr != nil {
		return nil, oops.Code("AUDIT_PLUGIN_ENVELOPE_UNMARSHAL_FAILED").Wrap(unmarshalErr)
	}
	return &envelope, nil
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
