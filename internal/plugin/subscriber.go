// Package plugin provides plugin management and lifecycle control.
package plugin

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	pluginpkg "github.com/holomush/holomush/pkg/plugin"
)

// EventEmitter publishes events from plugins.
type EventEmitter interface {
	EmitPluginEvent(ctx context.Context, pluginName string, event pluginpkg.EmitEvent) error
}

// subscription tracks which events a plugin wants.
type subscription struct {
	pluginName string
	stream     string
	eventTypes map[string]bool // empty = all events
}

// Subscriber dispatches events to plugins.
type Subscriber struct {
	host          Host
	emitter       EventEmitter
	subscriptions []subscription
	mu            sync.RWMutex
	wg            sync.WaitGroup
}

// NewSubscriber creates an event subscriber.
func NewSubscriber(host Host, emitter EventEmitter) *Subscriber {
	return &Subscriber{
		host:    host,
		emitter: emitter,
	}
}

// Subscribe registers a plugin to receive events.
func (s *Subscriber) Subscribe(pluginName, stream string, eventTypes []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	typeSet := make(map[string]bool)
	for _, t := range eventTypes {
		typeSet[t] = true
	}

	s.subscriptions = append(s.subscriptions, subscription{
		pluginName: pluginName,
		stream:     stream,
		eventTypes: typeSet,
	})
}

// Start begins processing events from the channel.
func (s *Subscriber) Start(ctx context.Context, events <-chan pluginpkg.Event) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				s.dispatch(ctx, event)
			}
		}
	}()
}

// Stop waits for the subscriber to finish.
func (s *Subscriber) Stop() {
	s.wg.Wait()
}

func (s *Subscriber) dispatch(ctx context.Context, event pluginpkg.Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, sub := range s.subscriptions {
		if sub.stream != event.Stream {
			continue
		}
		if len(sub.eventTypes) > 0 && !sub.eventTypes[string(event.Type)] {
			continue
		}

		s.deliverAsync(ctx, sub.pluginName, event)
	}
}

func (s *Subscriber) deliverAsync(ctx context.Context, pluginName string, event pluginpkg.Event) {
	// Use timeout for plugin execution
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()

		emits, err := s.host.DeliverEvent(ctx, pluginName, event)
		if err != nil {
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				slog.Warn("plugin event delivery timed out",
					"plugin", pluginName,
					"event_id", event.ID,
					"stream", event.Stream,
					"event_type", string(event.Type),
					"timeout", "5s")
			case errors.Is(err, context.Canceled):
				slog.Debug("plugin event delivery canceled",
					"plugin", pluginName,
					"event_id", event.ID)
			default:
				slog.Error("failed to deliver event to plugin",
					"plugin", pluginName,
					"event_id", event.ID,
					"stream", event.Stream,
					"event_type", string(event.Type),
					"error", err)
			}
			return
		}

		// Emit response events
		for _, emit := range emits {
			if err := s.emitter.EmitPluginEvent(ctx, pluginName, emit); err != nil {
				slog.Error("failed to emit plugin event",
					"plugin", pluginName,
					"stream", emit.Stream,
					"error", err)
			}
		}
	}()
}
