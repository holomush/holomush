// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package bootstrap — plugin crypto wiring (holomush-ojw1.1).
//
// BuildPluginEmitter produces the emitter half of the plugin-crypto path; its
// host-side sensitivity fence runs unconditionally (holomush-dj95.3), so it
// does not branch on cfg.Crypto. Separately, when cfg.Crypto.Enabled is true
// the caller MUST also wire a DEK manager into the publisher via
// eventbus.WithDEKManager — that happens during JetStreamPublisher
// construction, not here.
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

// BuildPluginEmitter constructs a PluginEventEmitter from deps. The emitter
// runs the host-side sensitivity fence unconditionally — the plugin manifest
// declaration is the single source of truth, with no runtime gate
// (holomush-dj95.3 removed the WithCryptoEnabled fossil). The cfg argument is
// retained for call-site symmetry; crypto's downstream effect (DEK-manager
// wiring on the publisher) is configured during JetStreamPublisher
// construction, not here.
func BuildPluginEmitter(_ context.Context, _ eventbus.Config, deps PluginEmitterDeps) (*plugins.PluginEventEmitter, error) {
	return plugins.NewPluginEventEmitter(
		deps.Publisher, deps.Manifests, deps.Resolver,
	), nil
}
