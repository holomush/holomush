// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package audit implements the DecryptAuditEmitter (Decision 3) for Phase 3b.
// Emitter satisfies both DecryptAuditEmitter and
// authguard.BackpressureChecker: per-plugin bounded queues drain to
// audit.<gameID>.plugin_decrypt.<pluginName> via eventbus.Publisher.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/idgen"
)

const (
	defaultCapacity  = 10_000
	defaultThreshold = 0.5
	defaultGameID    = "holomush"
	// auditEventType is the event type emitted for plugin decrypt audit records.
	auditEventType = "audit:plugin_decrypt"
)

// PluginDecryptRecord carries the fields logged when a plugin successfully
// receives decrypted event payload (Decision 3, master spec §7.6).
type PluginDecryptRecord struct {
	PluginName       string
	PluginInstanceID string
	EventID          ulid.ULID
	EventSubject     eventbus.Subject
	EventType        eventbus.Type
	DEKRef           codec.KeyID
	DEKVersion       uint32
	GrantID          ulid.ULID
}

// DecryptAuditEmitter records a decrypt-delivery audit entry for a plugin.
type DecryptAuditEmitter interface {
	EmitPluginDecrypt(ctx context.Context, rec PluginDecryptRecord) error
}

// Option configures an Emitter.
type Option func(*Emitter)

// WithCapacity sets the per-plugin queue buffer depth (default 10_000).
func WithCapacity(n int) Option {
	return func(q *Emitter) {
		if n > 0 {
			q.capacity = n
		}
	}
}

// WithThreshold sets the drain-below fraction at which ShouldThrottle clears
// (default 0.5, i.e. 50% of capacity).
func WithThreshold(r float64) Option {
	return func(q *Emitter) {
		if r > 0 && r < 1 {
			q.threshold = r
		}
	}
}

// WithGameID overrides the game-id segment in the published subject
// (default "holomush").
func WithGameID(gameID string) Option {
	return func(q *Emitter) {
		if gameID != "" {
			q.gameID = gameID
		}
	}
}

// pluginQueue is a per-plugin bounded channel with an atomic throttle flag.
type pluginQueue struct {
	ch        chan PluginDecryptRecord
	throttled atomic.Bool
}

// Emitter satisfies both DecryptAuditEmitter and authguard.BackpressureChecker.
// Per-plugin queues are created lazily on the first EmitPluginDecrypt call for
// that plugin. Obtain via NewQueuedEmitter.
type Emitter struct {
	pub       eventbus.Publisher
	gameID    string
	capacity  int
	threshold float64

	mu     sync.RWMutex
	queues map[string]*pluginQueue

	stopCh chan struct{}
	wg     sync.WaitGroup

	// drainCtx and drainCancel bound in-flight Publish calls to the emitter
	// lifetime. Shutdown calls drainCancel so a blocked Publish unblocks
	// (propagates cancellation without changing the fire-and-forget steady-state
	// behavior per master spec §12 Q2 — Publish ctx is only cancelled on Shutdown).
	drainCtx    context.Context //nolint:containedctx // intentional: bounds in-flight drains
	drainCancel context.CancelFunc
}

// NewQueuedEmitter constructs an Emitter with the given Publisher and
// options. Returns AUDIT_EMITTER_DEPENDENCY_NIL if pub is nil.
func NewQueuedEmitter(pub eventbus.Publisher, opts ...Option) (*Emitter, error) {
	if pub == nil {
		return nil, oops.Code("AUDIT_EMITTER_DEPENDENCY_NIL").
			With("dependency", "Publisher").
			Errorf("nil Publisher")
	}
	drainCtx, drainCancel := context.WithCancel(context.Background()) //nolint:gosec // G118: drainCancel is called in Shutdown; storing it in the struct is intentional lifecycle management.
	q := &Emitter{
		pub:         pub,
		gameID:      defaultGameID,
		capacity:    defaultCapacity,
		threshold:   defaultThreshold,
		queues:      make(map[string]*pluginQueue),
		stopCh:      make(chan struct{}),
		drainCtx:    drainCtx,
		drainCancel: drainCancel,
	}
	for _, opt := range opts {
		opt(q)
	}
	return q, nil
}

// ShouldThrottle implements authguard.BackpressureChecker. Returns true when
// the plugin's queue is at capacity; remains true until drain falls below
// capacity*threshold (default 50%).
func (q *Emitter) ShouldThrottle(pluginName string) bool {
	q.mu.RLock()
	pq, ok := q.queues[pluginName]
	q.mu.RUnlock()
	if !ok {
		return false
	}
	return pq.throttled.Load()
}

// EmitPluginDecrypt implements DecryptAuditEmitter. Non-blocking: enqueues the
// record or returns AUDIT_QUEUE_FULL if the plugin's queue is at capacity.
func (q *Emitter) EmitPluginDecrypt(_ context.Context, rec PluginDecryptRecord) error {
	pq := q.queueFor(rec.PluginName)

	select {
	case pq.ch <- rec:
		// Enqueued. Mark throttled if we've reached capacity.
		if len(pq.ch) >= q.capacity {
			pq.throttled.Store(true)
		}
		return nil
	default:
		// Queue full: mark throttled and return error.
		pq.throttled.Store(true)
		return oops.Code("AUDIT_QUEUE_FULL").
			With("plugin", rec.PluginName).
			With("capacity", q.capacity).
			Errorf("audit queue full for plugin %q", rec.PluginName)
	}
}

// queueFor returns the existing pluginQueue for pluginName or creates one and
// starts its drain goroutine. Double-checked locking ensures a single queue per
// plugin even under concurrent first calls.
func (q *Emitter) queueFor(pluginName string) *pluginQueue {
	q.mu.RLock()
	pq, ok := q.queues[pluginName]
	q.mu.RUnlock()
	if ok {
		return pq
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if existing, ok := q.queues[pluginName]; ok {
		return existing
	}
	pq = &pluginQueue{ch: make(chan PluginDecryptRecord, q.capacity)}
	q.queues[pluginName] = pq
	q.wg.Add(1)
	go q.drain(pluginName, pq)
	return pq
}

// drain reads records from the plugin queue and publishes them. On stop signal
// it returns immediately; it does NOT flush remaining entries (bounded flush
// deadline is the caller's responsibility via Shutdown). Drain failures surface
// as log noise and do not retry-block the subscriber path (master spec §12 Q2).
func (q *Emitter) drain(pluginName string, pq *pluginQueue) {
	defer q.wg.Done()
	threshold := int(float64(q.capacity) * q.threshold)
	if threshold < 1 {
		threshold = 1
	}
	for {
		select {
		case <-q.stopCh:
			return
		case rec := <-pq.ch:
			event := q.buildEvent(pluginName, rec)
			// Best-effort: drain failures are logged/metered but never
			// retry-block the subscriber path. drainCtx is cancelled by
			// Shutdown so an in-flight Publish unblocks when the emitter stops.
			if err := q.pub.Publish(q.drainCtx, event); err != nil {
				// TODO(metrics): increment authguard_audit_drain_failed_total
				_ = err
			}
			// Lift throttle once queue drains below threshold.
			if pq.throttled.Load() && len(pq.ch) < threshold {
				pq.throttled.Store(false)
			}
		}
	}
}

// buildEvent constructs the audit eventbus.Event for a PluginDecryptRecord.
// Sensitive=false, ActorKindSystem, IdentityCodec (no encryption).
func (q *Emitter) buildEvent(pluginName string, rec PluginDecryptRecord) eventbus.Event {
	now := time.Now().UTC()
	// json.Marshal cannot fail for this map (all fields are JSON-serializable
	// primitives/strings). If it somehow does, we emit with an empty payload
	// rather than dropping the audit record entirely.
	payload, err := json.Marshal(map[string]any{
		"plugin_name":        rec.PluginName,
		"plugin_instance_id": rec.PluginInstanceID,
		"event_id":           rec.EventID.String(),
		"event_subject":      string(rec.EventSubject),
		"event_type":         string(rec.EventType),
		"dek_ref":            uint64(rec.DEKRef),
		"dek_version":        rec.DEKVersion,
		"grant_id":           rec.GrantID.String(),
		"emitted_at":         now.Format(time.RFC3339Nano),
	})
	if err != nil {
		// TODO(metrics): increment authguard_audit_marshal_failed_total
		payload = []byte("{}")
	}
	// Audit-namespace subjects per master spec §7.6 / Phase 3b grounding doc Decision 3.
	// Intentionally NOT under events.> — the audit.> namespace is separate so
	// §7.7's two-layer isolation (Phase 3b ABAC default-deny + Phase 3d NATS
	// account-level deny_subscribe) can lock plugin/character principals out
	// of audit reads while still allowing them events.> reads.
	return eventbus.Event{
		ID:        idgen.New(),
		Subject:   eventbus.Subject(fmt.Sprintf("audit.%s.plugin_decrypt.%s", q.gameID, pluginName)),
		Type:      eventbus.Type(auditEventType),
		Timestamp: now,
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   payload,
		Sensitive: false,
	}
}

// Shutdown signals drain goroutines to stop and waits for them with the given
// deadline. Returns AUDIT_EMITTER_SHUTDOWN_TIMEOUT if the context expires.
// drainCancel is called first so any in-flight Publish call unblocks
// immediately, preventing Shutdown from hanging on a slow Publisher.
func (q *Emitter) Shutdown(ctx context.Context) error {
	q.drainCancel() // unblock any in-flight pub.Publish calls
	close(q.stopCh)
	done := make(chan struct{})
	go func() { q.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return oops.Code("AUDIT_EMITTER_SHUTDOWN_TIMEOUT").Wrap(ctx.Err())
	}
}
