// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/store"
)

// Compile-time interface check: *store.DatabaseSubsystem must satisfy lifecycle.Subsystem.
var _ lifecycle.Subsystem = (*store.DatabaseSubsystem)(nil)

func TestDatabaseSubsystemIDReturnsDatabase(t *testing.T) {
	sub := store.NewSubsystem(store.SubsystemConfig{})
	assert.Equal(t, lifecycle.SubsystemDatabase, sub.ID())
}

func TestDatabaseSubsystemDependsOnNothing(t *testing.T) {
	sub := store.NewSubsystem(store.SubsystemConfig{})
	assert.Empty(t, sub.DependsOn())
}

func TestDatabaseSubsystemPoolPanicsBeforeStart(t *testing.T) {
	sub := store.NewSubsystem(store.SubsystemConfig{})
	assert.Panics(t, func() { sub.Pool() })
}

func TestDatabaseSubsystemEventStorePanicsBeforeStart(t *testing.T) {
	sub := store.NewSubsystem(store.SubsystemConfig{})
	assert.Panics(t, func() { sub.EventStore() })
}

func TestDatabaseSubsystemGameIDPanicsBeforeStart(t *testing.T) {
	sub := store.NewSubsystem(store.SubsystemConfig{})
	assert.Panics(t, func() { sub.GameID() })
}

