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
// cfg.Crypto.Enabled gates the host-side sensitivity fence inside the
// emitter (see plugins.WithCryptoEnabled). When false (the Phase 3a
// default), the emitter skips the fence and stamps Sensitive=false
// unconditionally, matching pre-Phase-3a behavior. When true, the
// fence runs and the publisher's DEK-manager-aware crypto branch
// activates downstream.
func BuildPluginEmitter(_ context.Context, cfg eventbus.Config, deps PluginEmitterDeps) (*plugins.PluginEventEmitter, error) {
	return plugins.NewPluginEventEmitter(deps.Publisher, deps.Manifests, deps.Resolver,
		plugins.WithCryptoEnabled(cfg.Crypto.Enabled),
	), nil
}
