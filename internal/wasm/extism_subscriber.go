package wasm

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/holomush/holomush/internal/core"
)

// Emitter is the interface for emitting events back to the system.
type Emitter interface {
	Emit(ctx context.Context, stream string, eventType core.EventType, payload []byte) error
}

// ExtismSubscriber routes events to Extism plugins.
type ExtismSubscriber struct {
	host            *ExtismHost
	emitter         Emitter
	deliveryTimeout time.Duration
	mu              sync.RWMutex
	subscriptions   map[string][]string // plugin -> stream patterns
	wg              sync.WaitGroup
	ctx             context.Context
	cancel          context.CancelFunc
}

// Option configures an ExtismSubscriber.
type Option func(*ExtismSubscriber)

// WithDeliveryTimeout sets the timeout for plugin event delivery.
// Default is 5 seconds.
func WithDeliveryTimeout(d time.Duration) Option {
	return func(s *ExtismSubscriber) {
		s.deliveryTimeout = d
	}
}

// NewExtismSubscriber creates a subscriber for routing events to plugins.
// The provided context controls the subscriber's lifecycle; when cancelled,
// no new goroutines will be spawned for event handling.
//
// Panics if host or emitter is nil since these are programming errors.
func NewExtismSubscriber(ctx context.Context, host *ExtismHost, emitter Emitter, opts ...Option) *ExtismSubscriber {
	if host == nil {
		panic("wasm: NewExtismSubscriber requires non-nil host")
	}
	if emitter == nil {
		panic("wasm: NewExtismSubscriber requires non-nil emitter")
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &ExtismSubscriber{
		host:            host,
		emitter:         emitter,
		deliveryTimeout: 5 * time.Second,
		subscriptions:   make(map[string][]string),
		ctx:             ctx,
		cancel:          cancel,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Subscribe registers a plugin to receive events matching the stream pattern.
// Empty plugin names or patterns are rejected with a warning log.
// Non-existent plugins are allowed (for lazy loading) but logged at debug level.
func (s *ExtismSubscriber) Subscribe(pluginName, streamPattern string) {
	if pluginName == "" {
		slog.Warn("ignoring subscription with empty plugin name")
		return
	}
	if streamPattern == "" {
		slog.Warn("ignoring subscription with empty pattern", "plugin", pluginName)
		return
	}
	if !s.host.HasPlugin(pluginName) {
		slog.Debug("subscribing plugin not yet loaded", "plugin", pluginName)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscriptions[pluginName] = append(s.subscriptions[pluginName], streamPattern)
}

// HandleEvent delivers an event to all subscribed plugins.
// If the subscriber has been stopped, no goroutines are spawned.
func (s *ExtismSubscriber) HandleEvent(ctx context.Context, event core.Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for pluginName, patterns := range s.subscriptions {
		if !slices.ContainsFunc(patterns, func(p string) bool {
			return s.matchPattern(event.Stream, p)
		}) {
			continue
		}

		// Check if subscriber is stopped before spawning goroutine
		if s.ctx.Err() != nil {
			slog.Warn("dropping event due to shutdown",
				"event_id", event.ID.String(),
				"event_stream", event.Stream,
				"event_type", event.Type,
			)
			return
		}

		s.wg.Add(1)
		go func(plugin string) {
			defer s.wg.Done()
			s.deliverWithTimeout(ctx, plugin, event)
		}(pluginName)
	}
}

// Stop cancels the subscriber context and waits for all in-flight
// event deliveries to complete.
func (s *ExtismSubscriber) Stop() {
	s.cancel()
	s.wg.Wait()
}

func (s *ExtismSubscriber) matchPattern(stream, pattern string) bool {
	// Simple glob matching: "location:*" matches "location:anything"
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(stream, prefix)
	}
	return stream == pattern
}

// validateEmitStream checks if a plugin-emitted stream name is valid.
// Returns true if the stream is valid, false otherwise.
// Currently validates:
// - Stream name must not be empty
func (s *ExtismSubscriber) validateEmitStream(stream string) bool {
	return stream != ""
}

// deliverWithTimeout delivers an event to a plugin with the configured timeout.
//
// Error handling strategy: This method runs in a goroutine spawned by HandleEvent,
// so there is no caller to return errors to. All errors are logged via slog.Error
// with appropriate context (plugin name, event type, error) and the method either
// returns early (for delivery failures) or continues processing (for emit failures).
// This is the standard Go pattern for error handling in fire-and-forget workers.
func (s *ExtismSubscriber) deliverWithTimeout(parentCtx context.Context, pluginName string, event core.Event) {
	// Use timeout context for plugin call only; emissions use parent context
	// to avoid starvation if plugin takes most of the timeout
	ctx, cancel := context.WithTimeout(parentCtx, s.deliveryTimeout)
	defer cancel()

	emitted, err := s.host.DeliverEvent(ctx, pluginName, event)
	if err != nil {
		slog.Error("plugin event delivery failed",
			"plugin", pluginName,
			"event_id", event.ID.String(),
			"event_stream", event.Stream,
			"event_timestamp", event.Timestamp,
			"event_type", event.Type,
			"error", err)
		return
	}

	// Check if context was cancelled before processing emits
	if parentCtx.Err() != nil {
		slog.Warn("skipping plugin emits due to context cancellation",
			"plugin", pluginName,
			"pending_emits", len(emitted))
		return
	}

	// Emit any events the plugin generated using parent context
	var emitFailures int
	for i, emit := range emitted {
		// Validate stream name before emitting
		if !s.validateEmitStream(emit.Stream) {
			slog.Warn("rejected plugin emit: empty stream name",
				"plugin", pluginName,
				"emit_index", i,
				"emit_count", len(emitted),
				"emitted_type", emit.Type)
			emitFailures++
			continue
		}

		// Convert plugin.EventType to core.EventType (both are string types)
		eventType := core.EventType(emit.Type)

		if err := s.emitter.Emit(parentCtx, emit.Stream, eventType, []byte(emit.Payload)); err != nil {
			slog.Error("failed to emit plugin event",
				"plugin", pluginName,
				"emit_index", i,
				"emit_count", len(emitted),
				"emitted_stream", emit.Stream,
				"emitted_type", emit.Type,
				"error", err)
			emitFailures++
		}
	}

	if emitFailures > 0 {
		slog.Warn("plugin emit batch had failures",
			"plugin", pluginName,
			"failed", emitFailures,
			"total", len(emitted))
	}
}
