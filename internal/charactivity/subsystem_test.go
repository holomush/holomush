// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charactivity

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/lifecycle"
)

func TestNewSubsystemDefaultsANilLoggerToTheProcessDefault(t *testing.T) {
	s := NewSubsystem(Config{})
	require.NotNil(t, s.cfg.Logger, "a nil Logger MUST fall back to slog.Default()")
}

// TestNewSubsystemDefaultsToFileStorage together with the MemoryStorage
// test below is the D-42 hazard proof: FileStorage is the ZERO VALUE of
// jetstream.StorageType, so "the field is unset" and "the bucket is
// disk-backed" are the SAME state. Without the WithStorage seam a test
// could not distinguish them, and every test bucket would silently be
// file-backed.
func TestNewSubsystemDefaultsToFileStorage(t *testing.T) {
	require.Equal(t, jetstream.FileStorage, NewSubsystem(Config{}).storage)
}

func TestFileStorageIsTheZeroValueOfStorageType(t *testing.T) {
	var zero jetstream.StorageType
	require.Equal(t, jetstream.FileStorage, zero,
		"if this ever stops holding, the WithStorage seam's rationale changes")
}

func TestNewSubsystemWithStorageForcesTheRequestedBackingStore(t *testing.T) {
	tests := []struct {
		name string
		want jetstream.StorageType
	}{
		{"memory storage for tests", jetstream.MemoryStorage},
		{"file storage for production", jetstream.FileStorage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, NewSubsystemWithStorage(Config{}, tt.want).storage)
		})
	}
}

// TestNewSubsystemAcquiresNoLiveResources pins the constructor contract
// cmd/holomush's real-constructor graph tests depend on: they build every
// production subsystem with a zero-value config and read DependsOn() live,
// never calling Prepare.
func TestNewSubsystemAcquiresNoLiveResources(t *testing.T) {
	s := NewSubsystem(Config{})
	require.False(t, s.prepared, "constructor MUST NOT mark the subsystem prepared")
	require.Nil(t, s.done, "constructor MUST NOT launch a work loop")
}

func TestIDReturnsSubsystemCharacterActivity(t *testing.T) {
	require.Equal(t, lifecycle.SubsystemCharacterActivity, NewSubsystem(Config{}).ID())
}

func TestDependsOnDeclaresDatabaseAndEventBus(t *testing.T) {
	require.ElementsMatch(t, []lifecycle.SubsystemID{
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemEventBus,
	}, NewSubsystem(Config{}).DependsOn())
}

func TestDependsOnExcludesItselfSoTheGraphStaysAcyclic(t *testing.T) {
	s := NewSubsystem(Config{})
	require.NotContains(t, s.DependsOn(), s.ID())
}

func TestPrepareIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := NewSubsystem(Config{})
	require.NoError(t, s.Prepare(ctx))
	require.True(t, s.prepared)
	require.NoError(t, s.Prepare(ctx), "a repeated Prepare MUST be a no-op, not an error")
	require.True(t, s.prepared)
}

func TestActivateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := NewSubsystem(Config{})
	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))
	first := s.done
	require.NotNil(t, first)
	require.NoError(t, s.Activate(ctx), "a repeated Activate MUST be a no-op, not an error")
	require.Equal(t, first, s.done, "a repeated Activate MUST NOT replace the done channel")
}

func TestActivateWithoutPrepareStillSucceeds(t *testing.T) {
	// The orchestrator runs the whole Prepare sweep before the whole
	// Activate sweep, so this ordering never occurs in production — but
	// Activate must not panic if a test or a rollback path reaches it.
	require.NoError(t, NewSubsystem(Config{}).Activate(context.Background()))
}

// TestStopResetsBothGuardsSoRetryWorks is the guard-reset contract: Stop is
// the single teardown path, and a Prepare/Activate retry after it MUST
// reattach rather than short-circuit on torn-down state.
func TestStopResetsBothGuardsSoRetryWorks(t *testing.T) {
	ctx := context.Background()
	s := NewSubsystemWithStorage(Config{}, jetstream.MemoryStorage)
	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))

	require.NoError(t, s.Stop(ctx))
	require.False(t, s.prepared, "Stop MUST reset the Prepare guard")
	require.Nil(t, s.done, "Stop MUST reset the Activate guard")

	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))
	require.True(t, s.prepared)
	require.NotNil(t, s.done)
	require.Equal(t, jetstream.MemoryStorage, s.storage,
		"Stop MUST NOT reset the configured backing store — it is construction-time config, not acquired state")
}

func TestStopIsSafeInEveryLifecycleState(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		reach func(t *testing.T, s *Subsystem)
	}{
		{"never prepared", func(*testing.T, *Subsystem) {}},
		{"prepared but never activated", func(t *testing.T, s *Subsystem) {
			t.Helper()
			require.NoError(t, s.Prepare(ctx))
		}},
		{"fully activated", func(t *testing.T, s *Subsystem) {
			t.Helper()
			require.NoError(t, s.Prepare(ctx))
			require.NoError(t, s.Activate(ctx))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSubsystem(Config{})
			tt.reach(t, s)
			require.NoError(t, s.Stop(ctx))
			require.NoError(t, s.Stop(ctx), "Stop MUST be idempotent")
		})
	}
}

// TestSubsystemSatisfiesTheLifecycleInterface is the compile-time proof the
// skeleton is registerable today, before 03-05 fills the bodies.
func TestSubsystemSatisfiesTheLifecycleInterface(t *testing.T) {
	var s lifecycle.Subsystem = NewSubsystem(Config{})
	require.Equal(t, lifecycle.SubsystemCharacterActivity, s.ID())
}
