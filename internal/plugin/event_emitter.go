// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package plugins provides plugin management and lifecycle control.
package plugins

import (
	"context"
	"strings"
	"time"

	"github.com/holomush/holomush/internal/core"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/samber/oops"
)

// ManifestLookup returns the manifest for a loaded plugin.
type ManifestLookup func(pluginName string) *Manifest

// ActorResolver returns the actor that should be stamped onto emitted events.
type ActorResolver func(ctx context.Context, pluginName string) core.Actor

// PluginEventEmitter emits host-owned core events on behalf of plugins.
type PluginEventEmitter struct {
	store          core.EventStore
	lookupManifest ManifestLookup
	resolveActor   ActorResolver
}

// NewPluginEventEmitter creates a new shared host event emitter.
func NewPluginEventEmitter(store core.EventStore, lookup ManifestLookup, resolve ActorResolver) *PluginEventEmitter {
	return &PluginEventEmitter{
		store:          store,
		lookupManifest: lookup,
		resolveActor:   resolve,
	}
}

// Emit validates the target stream namespace, stamps host-owned fields, and
// appends the event to the backing store.
func (e *PluginEventEmitter) Emit(ctx context.Context, pluginName string, intent pluginsdk.EmitIntent) error {
	namespace := streamNamespace(intent.Stream)
	manifest := e.lookupManifest(pluginName)
	if manifest == nil || !declaresEmitNamespace(manifest.Emits, namespace) {
		return oops.With("plugin", pluginName).With("stream", intent.Stream).
			Errorf("plugin may not emit to namespace %q", namespace)
	}

	event := core.Event{
		ID:        core.NewULID(),
		Stream:    intent.Stream,
		Type:      core.EventType(intent.Type),
		Timestamp: time.Now().UTC(),
		Actor:     e.resolveActor(ctx, pluginName),
		Payload:   []byte(intent.Payload),
	}

	if err := e.store.Append(ctx, event); err != nil {
		return oops.With("plugin", pluginName).With("stream", intent.Stream).Wrap(err)
	}
	return nil
}

func streamNamespace(stream string) string {
	if stream == "" {
		return ""
	}
	if idx := strings.IndexByte(stream, ':'); idx >= 0 {
		return stream[:idx]
	}
	return stream
}

func declaresEmitNamespace(namespaces []string, namespace string) bool {
	for _, declared := range namespaces {
		if declared == namespace {
			return true
		}
	}
	return false
}
