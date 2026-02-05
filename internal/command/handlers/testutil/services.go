// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package testutil

import (
	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/accesstest"
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
			Access:      accesstest.NewMockAccessControl(),
			Events:      core.NewMemoryEventStore(),
			Broadcaster: core.NewBroadcaster(),
		},
	}
}

func (b *ServicesBuilder) WithWorld(worldService *world.Service) *ServicesBuilder {
	b.config.World = worldService
	return b
}

func (b *ServicesBuilder) WithWorldFixture(fixture *WorldServiceFixture) *ServicesBuilder {
	if fixture != nil {
		b.config.World = fixture.Service
	}
	return b
}

func (b *ServicesBuilder) WithSession(session core.SessionService) *ServicesBuilder {
	b.config.Session = session
	return b
}

func (b *ServicesBuilder) WithAccess(accessControl access.AccessControl) *ServicesBuilder {
	b.config.Access = accessControl
	return b
}

func (b *ServicesBuilder) WithEvents(events core.EventStore) *ServicesBuilder {
	b.config.Events = events
	return b
}

func (b *ServicesBuilder) WithBroadcaster(broadcaster command.EventBroadcaster) *ServicesBuilder {
	b.config.Broadcaster = broadcaster
	return b
}

func (b *ServicesBuilder) WithAliasCache(cache *command.AliasCache) *ServicesBuilder {
	b.config.AliasCache = cache
	return b
}

func (b *ServicesBuilder) WithAliasRepo(repo command.AliasWriter) *ServicesBuilder {
	b.config.AliasRepo = repo
	return b
}

func (b *ServicesBuilder) WithRegistry(registry *command.Registry) *ServicesBuilder {
	b.config.Registry = registry
	return b
}

func (b *ServicesBuilder) WithPropertyRegistry(registry *property.PropertyRegistry) *ServicesBuilder {
	b.config.PropertyRegistry = registry
	return b
}

// Build returns a Services instance for tests.
func (b *ServicesBuilder) Build() *command.Services {
	return command.NewTestServices(b.config)
}
