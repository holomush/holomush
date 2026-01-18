package wasm

import (
	"context"
	"log/slog"
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
	host          *ExtismHost
	emitter       Emitter
	mu            sync.RWMutex
	subscriptions map[string][]string // plugin -> stream patterns
	wg            sync.WaitGroup
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewExtismSubscriber creates a subscriber for routing events to plugins.
// The provided context controls the subscriber's lifecycle; when cancelled,
// no new goroutines will be spawned for event handling.
func NewExtismSubscriber(ctx context.Context, host *ExtismHost, emitter Emitter) *ExtismSubscriber {
	ctx, cancel := context.WithCancel(ctx)
	return &ExtismSubscriber{
		host:          host,
		emitter:       emitter,
		subscriptions: make(map[string][]string),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Subscribe registers a plugin to receive events matching the stream pattern.
func (s *ExtismSubscriber) Subscribe(pluginName, streamPattern string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscriptions[pluginName] = append(s.subscriptions[pluginName], streamPattern)
}

// HandleEvent delivers an event to all subscribed plugins.
// If the subscriber has been stopped, no goroutines are spawned.
func (s *ExtismSubscriber) HandleEvent(ctx context.Context, event core.Event) {
	// Check if subscriber is stopped before acquiring lock
	if s.ctx.Err() != nil {
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for pluginName, patterns := range s.subscriptions {
		if !s.matchesAny(event.Stream, patterns) {
			continue
		}

		// Double-check context after pattern match
		if s.ctx.Err() != nil {
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

func (s *ExtismSubscriber) matchesAny(stream string, patterns []string) bool {
	for _, pattern := range patterns {
		if s.matchPattern(stream, pattern) {
			return true
		}
	}
	return false
}

func (s *ExtismSubscriber) matchPattern(stream, pattern string) bool {
	// Simple glob matching: "location:*" matches "location:anything"
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(stream, prefix)
	}
	return stream == pattern
}

// deliverWithTimeout delivers an event to a plugin with a 5-second timeout.
//
// Error handling strategy: This method runs in a goroutine spawned by HandleEvent,
// so there is no caller to return errors to. All errors are logged via slog.Error
// with appropriate context (plugin name, event type, error) and the method either
// returns early (for delivery failures) or continues processing (for emit failures).
// This is the standard Go pattern for error handling in fire-and-forget workers.
func (s *ExtismSubscriber) deliverWithTimeout(parentCtx context.Context, pluginName string, event core.Event) {
	// Use timeout context for plugin call only; emissions use parent context
	// to avoid starvation if plugin takes most of the 5 seconds
	ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
	defer cancel()

	emitted, err := s.host.DeliverEvent(ctx, pluginName, event)
	if err != nil {
		slog.Error("plugin event delivery failed",
			"plugin", pluginName,
			"event_type", event.Type,
			"error", err)
		return
	}

	// Emit any events the plugin generated using parent context
	for _, emit := range emitted {
		// Convert plugin.EventType to core.EventType (both are string types)
		eventType := core.EventType(emit.Type)

		if err := s.emitter.Emit(parentCtx, emit.Stream, eventType, []byte(emit.Payload)); err != nil {
			slog.Error("failed to emit plugin event",
				"plugin", pluginName,
				"error", err)
		}
	}
}
