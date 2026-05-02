// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/bootstrap"
	"github.com/holomush/holomush/internal/eventbus"
)

func TestPluginEmitterDepsBuildSucceedsWithCryptoDisabled(t *testing.T) {
	cfg := eventbus.Config{} // Crypto.Enabled defaults false
	deps := bootstrap.PluginEmitterDeps{
		Publisher: &noopPublisher{},
		Manifests: func(string) *bootstrap.Manifest { return nil },
		Resolver:  func(context.Context, string) (bootstrap.Actor, error) { return bootstrap.Actor{}, nil },
	}
	emitter, err := bootstrap.BuildPluginEmitter(context.Background(), cfg, deps)
	require.NoError(t, err)
	assert.NotNil(t, emitter)
}

func TestPluginEmitterDepsBuildSucceedsWithCryptoEnabled(t *testing.T) {
	cfg := eventbus.Config{Crypto: eventbus.CryptoConfig{Enabled: true}}
	deps := bootstrap.PluginEmitterDeps{
		Publisher: &noopPublisher{},
		Manifests: func(string) *bootstrap.Manifest { return nil },
		Resolver:  func(context.Context, string) (bootstrap.Actor, error) { return bootstrap.Actor{}, nil },
	}
	emitter, err := bootstrap.BuildPluginEmitter(context.Background(), cfg, deps)
	require.NoError(t, err)
	assert.NotNil(t, emitter)
}

type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, eventbus.Event) error { return nil }
