// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package testutil

import (
	"testing"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
)

// WorldServiceMocks exposes mocked dependencies for WorldService.
type WorldServiceMocks struct {
	AccessControl *worldtest.MockAccessControl
	LocationRepo  *worldtest.MockLocationRepository
	ExitRepo      *worldtest.MockExitRepository
	ObjectRepo    *worldtest.MockObjectRepository
	SceneRepo     *worldtest.MockSceneRepository
	CharacterRepo *worldtest.MockCharacterRepository
	EventEmitter  *worldtest.MockEventEmitter
}

// WorldServiceFixture bundles a world service with its mocks.
type WorldServiceFixture struct {
	Service *world.Service
	Mocks   WorldServiceMocks
}

// WorldServiceBuilder builds a WorldServiceFixture with configurable mocks.
type WorldServiceBuilder struct {
	accessControl *worldtest.MockAccessControl
	locationRepo  *worldtest.MockLocationRepository
	exitRepo      *worldtest.MockExitRepository
	objectRepo    *worldtest.MockObjectRepository
	sceneRepo     *worldtest.MockSceneRepository
	characterRepo *worldtest.MockCharacterRepository
	eventEmitter  *worldtest.MockEventEmitter
}

// NewWorldServiceBuilder returns a builder with default mocks.
func NewWorldServiceBuilder(t *testing.T) *WorldServiceBuilder {
	return &WorldServiceBuilder{
		accessControl: worldtest.NewMockAccessControl(t),
		locationRepo:  worldtest.NewMockLocationRepository(t),
		exitRepo:      worldtest.NewMockExitRepository(t),
		objectRepo:    worldtest.NewMockObjectRepository(t),
		sceneRepo:     worldtest.NewMockSceneRepository(t),
		characterRepo: worldtest.NewMockCharacterRepository(t),
		eventEmitter:  worldtest.NewMockEventEmitter(t),
	}
}

func (b *WorldServiceBuilder) WithAccessControl(accessControl *worldtest.MockAccessControl) *WorldServiceBuilder {
	b.accessControl = accessControl
	return b
}

func (b *WorldServiceBuilder) WithLocationRepo(locationRepo *worldtest.MockLocationRepository) *WorldServiceBuilder {
	b.locationRepo = locationRepo
	return b
}

func (b *WorldServiceBuilder) WithExitRepo(exitRepo *worldtest.MockExitRepository) *WorldServiceBuilder {
	b.exitRepo = exitRepo
	return b
}

func (b *WorldServiceBuilder) WithObjectRepo(objectRepo *worldtest.MockObjectRepository) *WorldServiceBuilder {
	b.objectRepo = objectRepo
	return b
}

func (b *WorldServiceBuilder) WithSceneRepo(sceneRepo *worldtest.MockSceneRepository) *WorldServiceBuilder {
	b.sceneRepo = sceneRepo
	return b
}

func (b *WorldServiceBuilder) WithCharacterRepo(characterRepo *worldtest.MockCharacterRepository) *WorldServiceBuilder {
	b.characterRepo = characterRepo
	return b
}

func (b *WorldServiceBuilder) WithEventEmitter(eventEmitter *worldtest.MockEventEmitter) *WorldServiceBuilder {
	b.eventEmitter = eventEmitter
	return b
}

// Build creates a WorldServiceFixture from the builder.
func (b *WorldServiceBuilder) Build() *WorldServiceFixture {
	if b.accessControl == nil {
		panic("testutil.WorldServiceBuilder: AccessControl is required")
	}

	service := world.NewService(world.ServiceConfig{
		LocationRepo:  b.locationRepo,
		ExitRepo:      b.exitRepo,
		ObjectRepo:    b.objectRepo,
		SceneRepo:     b.sceneRepo,
		CharacterRepo: b.characterRepo,
		AccessControl: b.accessControl,
		EventEmitter:  b.eventEmitter,
	})

	return &WorldServiceFixture{
		Service: service,
		Mocks: WorldServiceMocks{
			AccessControl: b.accessControl,
			LocationRepo:  b.locationRepo,
			ExitRepo:      b.exitRepo,
			ObjectRepo:    b.objectRepo,
			SceneRepo:     b.sceneRepo,
			CharacterRepo: b.characterRepo,
			EventEmitter:  b.eventEmitter,
		},
	}
}
