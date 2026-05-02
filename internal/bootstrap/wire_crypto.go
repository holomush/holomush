// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package bootstrap — Phase 3a crypto wiring (holomush-ojw1.1).
//
// BuildPluginEmitter constructs a PluginEventEmitter; when
// cfg.Crypto.Enabled is true, the caller MUST also wire a DEK manager
// into the publisher via eventbus.WithDEKManager (this happens during
// JetStreamPublisher construction, not here — this helper produces
// the emitter half only).
package bootstrap

import (
	"context"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	plugins "github.com/holomush/holomush/internal/plugin"
)

// Manifest is re-exported for callers that don't want to import the
// internal/plugin package directly.
type Manifest = plugins.Manifest

// Actor is re-exported similarly. Note: the canonical Actor type is
// core.Actor, NOT plugins.Actor (which doesn't exist). We alias core
// here for callers' convenience.
type Actor = core.Actor

// PluginEmitterDeps bundles the constructor inputs.
type PluginEmitterDeps struct {
	Publisher eventbus.Publisher
	Manifests plugins.ManifestLookup
	Resolver  plugins.ActorResolver
}

// BuildPluginEmitter constructs a PluginEventEmitter from cfg + deps.
// The crypto enable flag affects publisher wiring (DEK manager,
// sensitivity branch), NOT the emitter's structure — the emitter
// always runs the fence; the fence's effective output depends on the
// manifest only. The flag matters in the publisher.
func BuildPluginEmitter(_ context.Context, _ eventbus.Config, deps PluginEmitterDeps) (*plugins.PluginEventEmitter, error) {
	return plugins.NewPluginEventEmitter(deps.Publisher, deps.Manifests, deps.Resolver), nil
}
