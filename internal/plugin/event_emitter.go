// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package plugins provides plugin management and lifecycle control.
package plugins

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/holomush/holomush/internal/core"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/samber/oops"
)

// ManifestLookup returns the manifest for a loaded plugin.
type ManifestLookup func(pluginName string) *Manifest

// ActorResolver returns the actor that should be stamped onto emitted events.
type ActorResolver func(ctx context.Context, pluginName string) (core.Actor, error)

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
	namespace, err := streamNamespace(intent.Stream)
	if err != nil {
		return oops.With("plugin", pluginName).With("stream", intent.Stream).Wrap(err)
	}
	if e.lookupManifest == nil {
		return oops.With("plugin", pluginName).With("stream", intent.Stream).
			New("plugin manifest lookup is not configured")
	}
	manifest := e.lookupManifest(pluginName)
	if manifest == nil || !declaresEmitNamespace(manifest.Emits, namespace) {
		return oops.With("plugin", pluginName).With("stream", intent.Stream).
			Errorf("plugin may not emit to namespace %q", namespace)
	}

	if e.resolveActor == nil {
		return oops.With("plugin", pluginName).With("stream", intent.Stream).
			New("plugin actor resolver is not configured")
	}
	actor, err := e.resolveActor(ctx, pluginName)
	if err != nil {
		return oops.With("plugin", pluginName).With("stream", intent.Stream).Wrap(err)
	}
	if err := validateResolvedActor(actor); err != nil {
		return oops.With("plugin", pluginName).With("stream", intent.Stream).Wrap(err)
	}
	if e.store == nil {
		return oops.With("plugin", pluginName).With("stream", intent.Stream).
			New("plugin event store is not configured")
	}
	if !json.Valid([]byte(intent.Payload)) {
		return oops.With("plugin", pluginName).With("stream", intent.Stream).
			New("event payload must be valid JSON")
	}

	event := core.NewEvent(intent.Stream, core.EventType(intent.Type), actor, []byte(intent.Payload))

	if err := e.store.Append(ctx, event); err != nil {
		return oops.With("plugin", pluginName).With("stream", intent.Stream).Wrap(err)
	}
	return nil
}

func streamNamespace(stream string) (string, error) {
	if strings.TrimSpace(stream) != stream || stream == "" {
		return "", oops.New("stream must not be empty or padded with whitespace")
	}
	suffix := ""
	hasSeparator := false
	if idx := strings.IndexByte(stream, ':'); idx >= 0 {
		hasSeparator = true
		suffix = stream[idx+1:]
		stream = stream[:idx]
	}
	if stream == "" {
		return "", oops.New("stream namespace must not be empty")
	}
	if suffix != "" && strings.TrimSpace(suffix) != suffix {
		return "", oops.New("stream suffix must not be padded with whitespace")
	}
	if hasSeparator && suffix == "" {
		return "", oops.New("stream suffix must not be empty")
	}
	if !namePattern.MatchString(stream) {
		return "", oops.With("stream_namespace", stream).
			New("stream namespace must match plugin naming pattern")
	}
	return stream, nil
}

func validateResolvedActor(actor core.Actor) error {
	if actor.ID == "" {
		return oops.New("plugin actor resolver returned actor with empty ID")
	}
	switch actor.Kind {
	case core.ActorCharacter, core.ActorSystem, core.ActorPlugin:
		return nil
	default:
		return oops.With("actor_kind", actor.Kind).
			New("plugin actor resolver returned unknown actor kind")
	}
}

func declaresEmitNamespace(namespaces []string, namespace string) bool {
	for _, declared := range namespaces {
		if declared == namespace {
			return true
		}
	}
	return false
}
