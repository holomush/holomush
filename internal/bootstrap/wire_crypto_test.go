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

// TestBuildPluginEmitterSucceedsRegardlessOfCryptoConfig pins that the emitter
// constructor is crypto-config-agnostic: holomush-dj95.3 removed the
// WithCryptoEnabled gate, so the host-side sensitivity fence runs
// unconditionally and BuildPluginEmitter no longer branches on cfg.Crypto.
func TestBuildPluginEmitterSucceedsRegardlessOfCryptoConfig(t *testing.T) {
	for _, cryptoEnabled := range []bool{false, true} {
		name := "crypto config disabled"
		if cryptoEnabled {
			name = "crypto config enabled"
		}
		t.Run(name, func(t *testing.T) {
			enabled := cryptoEnabled
			cfg := eventbus.Config{Crypto: eventbus.CryptoConfig{Enabled: &enabled}}
			deps := bootstrap.PluginEmitterDeps{
				Publisher: &noopPublisher{},
				Manifests: func(string) *bootstrap.Manifest { return nil },
				Resolver:  func(context.Context, string) (bootstrap.Actor, error) { return bootstrap.Actor{}, nil },
			}
			emitter, err := bootstrap.BuildPluginEmitter(context.Background(), cfg, deps)
			require.NoError(t, err)
			assert.NotNil(t, emitter)
		})
	}
}

type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, eventbus.Event) error { return nil }
