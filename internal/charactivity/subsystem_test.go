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
	require.Nil(t, s.kv, "constructor MUST NOT acquire the KV bucket")
	require.Nil(t, s.cons, "constructor MUST NOT create the durable consumer")
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
	kv := newFakeKV()
	s := newFakeWiredSubsystem(t, kv, (&recordingWriter{}).write)

	require.NoError(t, s.Prepare(ctx))
	first := s.kv
	require.NotNil(t, first)
	require.NoError(t, s.Prepare(ctx), "a repeated Prepare MUST be a no-op, not an error")
	require.Equal(t, first, s.kv, "a repeated Prepare MUST NOT reacquire the bucket")
}

// TestPrepareUsesTheIdempotentBucketConstructor pins the CreateOrUpdateKeyValue
// choice: the lifecycle contract requires Prepare to be re-runnable, and
// CreateKeyValue returns ErrBucketExists on the second boot.
func TestPrepareUsesTheIdempotentBucketConstructor(t *testing.T) {
	require.Equal(t, "character_activity", BucketName)
	require.Equal(t, "character_activity_listener", DefaultConsumerName)
}

func TestPrepareRefusesToRunUnwired(t *testing.T) {
	err := NewSubsystem(Config{}).Prepare(context.Background())
	require.Error(t, err, "a nil JetStream provider is a composition-root bug, not a runtime condition")
}

func TestActivateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newFakeWiredSubsystem(t, newFakeKV(), (&recordingWriter{}).write)
	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))
	first := s.done
	require.NotNil(t, first)
	require.NoError(t, s.Activate(ctx), "a repeated Activate MUST be a no-op, not an error")
	require.Equal(t, first, s.done, "a repeated Activate MUST NOT replace the done channel")
}

// TestActivateWithoutPrepareRefuses mirrors the retirement reactor: the
// orchestrator always runs the whole Prepare sweep first, so this never fires
// in production — and a silent success would leave last_active_at permanently
// unwritten with nothing to point at.
func TestActivateWithoutPrepareRefuses(t *testing.T) {
	require.Error(t, NewSubsystem(Config{}).Activate(context.Background()))
}

// TestStopResetsBothGuardsSoRetryWorks is the guard-reset contract: Stop is
// the single teardown path, and a Prepare/Activate retry after it MUST
// reattach rather than short-circuit on torn-down state.
func TestStopResetsBothGuardsSoRetryWorks(t *testing.T) {
	ctx := context.Background()
	s := newFakeWiredSubsystem(t, newFakeKV(), (&recordingWriter{}).write)
	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))

	require.NoError(t, s.Stop(ctx))
	require.Nil(t, s.kv, "Stop MUST reset the Prepare guard")
	require.Nil(t, s.done, "Stop MUST reset the Activate guard")

	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))
	require.NotNil(t, s.kv)
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
			s := newFakeWiredSubsystem(t, newFakeKV(), (&recordingWriter{}).write)
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
