// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus"
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
	m.consumers = append(m.consumers, pc)
	m.mu.Unlock()
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
			// Best-effort rollback of already-started consumers.
			for _, started := range m.consumers {
				if started.cc != nil {
					started.cc.Stop()
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
func (pc *pluginConsumer) dispatch(msg jetstream.Msg) error {
	h := msg.Headers()

	msgID := h.Get(headerMsgID)
	if msgID == "" {
		return oops.Code("AUDIT_PLUGIN_MISSING_HEADER").With("header", headerMsgID).Errorf("missing header")
	}
	idBytes, err := decodeULIDString(msgID)
	if err != nil {
		return oops.Code("AUDIT_PLUGIN_BAD_MSG_ID").With("msg_id", msgID).Wrap(err)
	}

	meta, err := msg.Metadata()
	if err != nil {
		return oops.Code("AUDIT_PLUGIN_METADATA_FAILED").Wrap(err)
	}

	headers := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	actor, err := actorFromHeaders(h)
	if err != nil {
		return err
	}

	parent := pc.workerCtx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, persistTimeout)
	defer cancel()

	event := &eventbusv1.Event{
		Id:        idBytes,
		Subject:   msg.Subject(),
		Type:      h.Get(headerEventType),
		Timestamp: timestamppb.New(meta.Timestamp),
		Actor:     actor,
		Payload:   msg.Data(),
	}

	_, err = pc.cfg.Client.AuditEvent(ctx, &pluginv1.AuditEventRequest{
		Event:   event,
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

// actorFromHeaders reconstructs an Actor proto from the wire headers. Missing
// App-Actor-Kind defaults to system (matches projection.persist). A set-but-
// malformed App-Actor-ID is a contract violation and is rejected here rather
// than silently attributed to system.
func actorFromHeaders(h nats.Header) (*eventbusv1.Actor, error) {
	kind := h.Get(headerActorKind)
	if kind == "" {
		kind = defaultActorKind
	}
	var id []byte
	if v := h.Get(headerActorID); v != "" {
		parsed, err := ulid.Parse(v)
		if err != nil {
			return nil, oops.Code("AUDIT_PLUGIN_BAD_ACTOR_ID").With("value", v).Wrap(err)
		}
		b := parsed.Bytes()
		id = b
	}
	return &eventbusv1.Actor{
		Kind: actorKindProto(kind),
		Id:   id,
	}, nil
}

// actorKindProto maps the header string to the ActorKind enum. Unknown
// values yield ACTOR_KIND_UNSPECIFIED (the zero value), matching the
// tolerance-at-read policy used throughout the audit path.
func actorKindProto(s string) eventbusv1.ActorKind {
	switch s {
	case "ACTOR_KIND_CHARACTER", "character":
		return eventbusv1.ActorKind_ACTOR_KIND_CHARACTER
	case "ACTOR_KIND_SYSTEM", "system":
		return eventbusv1.ActorKind_ACTOR_KIND_SYSTEM
	case "ACTOR_KIND_PLUGIN", "plugin":
		return eventbusv1.ActorKind_ACTOR_KIND_PLUGIN
	default:
		return eventbusv1.ActorKind_ACTOR_KIND_UNSPECIFIED
	}
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

// itoaAckWait converts a duration to a short string for logs without
// pulling in strconv.FormatFloat. Used only by debug-level helpers.
func itoaAckWait(d time.Duration) string { //nolint:unused // reserved for future debug logging
	return strconv.FormatInt(int64(d/time.Millisecond), 10) + "ms"
}
