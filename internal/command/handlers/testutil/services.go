// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package testutil

import (
	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/property"
	"github.com/holomush/holomush/internal/world"
)

// ServicesBuilder builds command.Services with reasonable defaults for tests.
type ServicesBuilder struct {
	config command.ServicesConfig
}

// NewServicesBuilder creates a builder with default services.
func NewServicesBuilder() *ServicesBuilder {
	return &ServicesBuilder{
		config: command.ServicesConfig{
			Session:     core.NewSessionManager(),
			Engine:      policytest.AllowAllEngine(),
			Events:      core.NewMemoryEventStore(),
			Broadcaster: core.NewBroadcaster(),
		},
	}
}

// WithWorld sets the world service.
func (b *ServicesBuilder) WithWorld(worldService *world.Service) *ServicesBuilder {
	b.config.World = worldService
	return b
}

// WithWorldFixture sets the world service from a fixture.
func (b *ServicesBuilder) WithWorldFixture(fixture *WorldServiceFixture) *ServicesBuilder {
	if fixture != nil {
		b.config.World = fixture.Service
	}
	return b
}

// WithSession sets the session service.
func (b *ServicesBuilder) WithSession(session core.SessionService) *ServicesBuilder {
	b.config.Session = session
	return b
}

// WithEngine sets the ABAC policy engine.
func (b *ServicesBuilder) WithEngine(engine policy.AccessPolicyEngine) *ServicesBuilder {
	b.config.Engine = engine
	return b
}

// WithEvents sets the event store.
func (b *ServicesBuilder) WithEvents(events core.EventStore) *ServicesBuilder {
	b.config.Events = events
	return b
}

// WithBroadcaster sets the event broadcaster.
func (b *ServicesBuilder) WithBroadcaster(broadcaster command.EventBroadcaster) *ServicesBuilder {
	b.config.Broadcaster = broadcaster
	return b
}

// WithAliasCache sets the alias cache.
func (b *ServicesBuilder) WithAliasCache(cache *command.AliasCache) *ServicesBuilder {
	b.config.AliasCache = cache
	return b
}

// WithAliasRepo sets the alias writer.
func (b *ServicesBuilder) WithAliasRepo(repo command.AliasWriter) *ServicesBuilder {
	b.config.AliasRepo = repo
	return b
}

// WithRegistry sets the command registry.
func (b *ServicesBuilder) WithRegistry(registry *command.Registry) *ServicesBuilder {
	b.config.Registry = registry
	return b
}

// WithPropertyRegistry sets the property registry.
func (b *ServicesBuilder) WithPropertyRegistry(registry *property.Registry) *ServicesBuilder {
	b.config.PropertyRegistry = registry
	return b
}

// Build returns a Services instance for tests.
func (b *ServicesBuilder) Build() *command.Services {
	return command.NewTestServices(b.config)
}
