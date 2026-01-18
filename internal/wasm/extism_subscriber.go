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
}

// NewExtismSubscriber creates a subscriber for routing events to plugins.
func NewExtismSubscriber(host *ExtismHost, emitter Emitter) *ExtismSubscriber {
	return &ExtismSubscriber{
		host:          host,
		emitter:       emitter,
		subscriptions: make(map[string][]string),
	}
}

// Subscribe registers a plugin to receive events matching the stream pattern.
func (s *ExtismSubscriber) Subscribe(pluginName, streamPattern string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscriptions[pluginName] = append(s.subscriptions[pluginName], streamPattern)
}

// HandleEvent delivers an event to all subscribed plugins.
func (s *ExtismSubscriber) HandleEvent(ctx context.Context, event core.Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for pluginName, patterns := range s.subscriptions {
		if !s.matchesAny(event.Stream, patterns) {
			continue
		}

		go s.deliverWithTimeout(ctx, pluginName, event)
	}
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
