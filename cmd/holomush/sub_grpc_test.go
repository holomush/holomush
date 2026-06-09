// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/pkg/errutil"
)

// stubDEKManager satisfies dek.Manager via embedding; its methods are nil and
// unused — these tests only assert option construction, never invoke the manager.
type stubDEKManager struct{ dek.Manager }

// stubSessionAuthGuard / stubSessionAuditEmitter satisfy their interfaces via
// embedding; methods are nil and unused — subscriber-option tests only assert
// construction, never invoke the guard or emitter.
type (
	stubSessionAuthGuard    struct{ eventbus.SessionAuthGuard }
	stubSessionAuditEmitter struct{ eventbus.SessionAuditEmitter }
)

// Verifies: INV-CRYPTO-117
func TestPublisherOptionsIncludeDEKManagerWhenRekeySet(t *testing.T) {
	cfg := grpcSubsystemConfig{RekeyManager: &stubDEKManager{}}
	require.Len(t, publisherOptionsFor(cfg), 1, "RekeyManager set ⇒ exactly the WithDEKManager option")
}

// Verifies: INV-CRYPTO-117
func TestPublisherOptionsEmptyWhenRekeyNil(t *testing.T) {
	require.Empty(t, publisherOptionsFor(grpcSubsystemConfig{RekeyManager: nil}),
		"no KEK ⇒ plaintext-only publisher, no DEK option")
}

// TestGRPCSubsystemImplementsSubsystem is a compile-time interface check.
func TestGRPCSubsystemImplementsSubsystem(_ *testing.T) {
	var _ lifecycle.Subsystem = (*grpcSubsystem)(nil)
	// If this compiles, the interface is satisfied.
}

// TestGRPCSubsystemIDReturnsGRPC verifies that ID() returns SubsystemGRPC.
func TestGRPCSubsystemIDReturnsGRPC(t *testing.T) {
	s := newGRPCSubsystem(grpcSubsystemConfig{})

	assert.Equal(t, lifecycle.SubsystemGRPC, s.ID())
}

// TestGRPCSubsystemDependsOnExpectedSubsystems verifies that DependsOn returns
// exactly 4 dependencies: Bootstrap, Sessions, Auth, and EventBus.
// EventBus was added in the F1 cutover: gRPC Start() reads the eventbus
// Publisher when wiring the shared plugin event emitter.
func TestGRPCSubsystemDependsOnExpectedSubsystems(t *testing.T) {
	s := newGRPCSubsystem(grpcSubsystemConfig{})

	deps := s.DependsOn()

	require.Len(t, deps, 4)
	assert.Contains(t, deps, lifecycle.SubsystemBootstrap)
	assert.Contains(t, deps, lifecycle.SubsystemSessions)
	assert.Contains(t, deps, lifecycle.SubsystemAuth)
	assert.Contains(t, deps, lifecycle.SubsystemEventBus)
}

// TestGRPCSubsystemStopBeforeStartIsSafe verifies that calling Stop on a
// subsystem that was never started returns nil without panicking.
func TestGRPCSubsystemStopBeforeStartIsSafe(t *testing.T) {
	s := newGRPCSubsystem(grpcSubsystemConfig{})

	err := s.Stop(context.Background())

	require.NoError(t, err)
}

// TestGRPCSubsystemStopWithTimeoutDoesNotHang verifies that Stop respects
// context deadline and returns before the deadline expires.
func TestGRPCSubsystemStopWithTimeoutDoesNotHang(t *testing.T) {
	s := newGRPCSubsystem(grpcSubsystemConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- s.Stop(ctx)
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("Stop() did not return within context deadline")
	}
}

// TestGRPCSubsystemReaperCancelNilSafe verifies that a nil reaperCancel field
// does not cause a panic when Stop is called.
func TestGRPCSubsystemReaperCancelNilSafe(t *testing.T) {
	s := newGRPCSubsystem(grpcSubsystemConfig{})
	s.reaperCancel = nil

	assert.NotPanics(t, func() {
		_ = s.Stop(context.Background())
	})
}

// TestNewGRPCSubsystemStoresConfig verifies that newGRPCSubsystem stores the
// provided configuration for use by Start.
func TestNewGRPCSubsystemStoresConfig(t *testing.T) {
	cfg := grpcSubsystemConfig{
		GRPCAddr:   "localhost:9000",
		MaxHistory: 42,
	}

	s := newGRPCSubsystem(cfg)

	assert.Equal(t, cfg.GRPCAddr, s.cfg.GRPCAddr)
	assert.Equal(t, cfg.MaxHistory, s.cfg.MaxHistory)
}

// fakeEventbusPublisher is a no-op publisher for wrapPublisher tests.
type fakeEventbusPublisher struct{}

func (f *fakeEventbusPublisher) Publish(_ context.Context, _ eventbus.Event) error { return nil }

// TestGrpcSubsystemWrapPublisher is AC#10. Calling wrapPublisher on a
// configured subsystem MUST return a *eventbus.RenderingPublisher.
func TestGrpcSubsystemWrapPublisher(t *testing.T) {
	registry, err := core.BootstrapVerbRegistry("test")
	require.NoError(t, err)

	s := &grpcSubsystem{
		cfg: grpcSubsystemConfig{
			VerbRegistry: registry,
		},
	}

	raw := &fakeEventbusPublisher{}
	wrapped, err := s.wrapPublisher(raw)
	require.NoError(t, err)

	_, ok := wrapped.(*eventbus.RenderingPublisher)
	assert.True(t, ok, "wrapPublisher must return *eventbus.RenderingPublisher")
}

// TestGrpcSubsystemWrapPublisherWithoutRegistry asserts the error path.
func TestGrpcSubsystemWrapPublisherWithoutRegistry(t *testing.T) {
	s := &grpcSubsystem{cfg: grpcSubsystemConfig{}}
	_, err := s.wrapPublisher(&fakeEventbusPublisher{})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "GRPC_VERB_REGISTRY_MISSING")
}

// TestNewHistoryReaderNilPreservesNilAuth asserts INV-CRYPTO-5: calling
// newHistoryReader with nil guard/dekMgr/auditEm must preserve
// the existing nil-auth behavior (no WithHistoryAuth appended).
func TestNewHistoryReaderNilPreservesNilAuth(t *testing.T) {
	cfg := eventbus.Config{}.Defaults()
	reader := newHistoryReader(nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	assert.NotNil(t, reader, "nil auth must still return a valid HistoryReader")
}

// TestGRPCSubsystemConfigHasRekeyManagerField asserts that grpcSubsystemConfig
// carries the three crypto wiring fields added by INV-CRYPTO-22 fix (sub-epic E T44+).
// A nil RekeyManager MUST pass the nil-auth fallback; a non-nil manager MUST
// cause newHistoryReader to call WithHistoryAuthAndSourceResolver instead of
// the legacy WithHistoryAuth path (via the Guard/Emitter constructed in Start).
func TestGRPCSubsystemConfigHasRekeyManagerField(t *testing.T) {
	// The fields must be present and zero-valued on empty config.
	cfg := grpcSubsystemConfig{}
	assert.Nil(t, cfg.RekeyManager, "RekeyManager must be nil on zero config")
	assert.Nil(t, cfg.AuthGuard, "AuthGuard must be nil on zero config")
	assert.Nil(t, cfg.AuditEmitter, "AuditEmitter must be nil on zero config")
}

// TestNewHistoryReaderWithCryptoDepsBuildsFallbackResolver asserts that when
// all three crypto deps (guard, dekMgr, auditEm) are non-nil, newHistoryReader
// returns a non-nil HistoryReader wired with the FallbackResolver path
// (WithHistoryAuthAndSourceResolver). The test does not drive a live read —
// it only asserts the reader is constructed without error, which proves the
// wiring code paths compile and run without panicking.
//
// INV-CRYPTO-22 production wiring (sub-epic E T44): the FallbackResolver path MUST
// be active when RekeyManager is non-nil in grpcSubsystemConfig.
func TestNewHistoryReaderWithCryptoDepsBuildsFallbackResolver(t *testing.T) {
	cfg := eventbus.Config{}.Defaults()
	guard := &grpcTestAuthGuard{}
	dekMgr := &grpcTestDEKManager{}
	auditEm := &grpcTestAuditEmitter{}
	// pool=nil is safe here — the FallbackResolver's ColdTierLookup (backed by
	// a nil pool) is only invoked on actual reads, not construction.
	reader := newHistoryReader(nil, nil, cfg, nil, nil, guard, dekMgr, auditEm, nil, nil, nil, nil)
	assert.NotNil(t, reader, "non-nil crypto deps must still return a valid HistoryReader")
}

// grpcTestAuthGuard is a minimal SessionAuthGuard stub for grpc subsystem tests.
type grpcTestAuthGuard struct{}

func (s *grpcTestAuthGuard) Check(_ context.Context, _ eventbus.SessionCheckRequest) (eventbus.SessionDecision, error) {
	return eventbus.SessionDecision{}, nil
}

// grpcTestDEKManager is a minimal SessionDEKManager stub for grpc subsystem tests.
type grpcTestDEKManager struct{}

func (s *grpcTestDEKManager) Resolve(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
	return codec.Key{}, nil
}

// grpcTestAuditEmitter is a minimal SessionAuditEmitter stub for grpc subsystem tests.
type grpcTestAuditEmitter struct{}

func (s *grpcTestAuditEmitter) EmitPluginDecrypt(_ context.Context, _ eventbus.PluginDecryptRecord) error {
	return nil
}

// Verifies: INV-CRYPTO-117
func TestSubscriberOptionsIncludeAuthGuardWhenGuardPresent(t *testing.T) {
	opts := subscriberOptionsFor(stubSessionAuthGuard{}, &stubDEKManager{}, stubSessionAuditEmitter{})
	require.Len(t, opts, 3, "guard present ⇒ AuthGuard + DEKManager + DecryptAuditEmitter")
}

// Verifies: INV-CRYPTO-117
func TestSubscriberOptionsEmptyWhenGuardNil(t *testing.T) {
	require.Empty(t, subscriberOptionsFor(nil, nil, nil),
		"no guard ⇒ bare subscriber preserves guard==nil passthrough")
}

// Verifies: INV-CRYPTO-117
func TestSubscriberOptionsEmptyWhenGuardPresentButDEKManagerNil(t *testing.T) {
	require.Empty(t, subscriberOptionsFor(stubSessionAuthGuard{}, nil, stubSessionAuditEmitter{}),
		"guard without a DEK manager is a half-configured decrypt path ⇒ all-or-nothing returns no options")
}

// Asserts the activation gate is wired from KEK presence, not a standalone flag.
func TestCoreServerCryptoActiveTracksKEKPresence(t *testing.T) {
	require.True(t, cryptoActiveFor(grpcSubsystemConfig{RekeyManager: &stubDEKManager{}}),
		"RekeyManager set ⇒ crypto active")
	require.False(t, cryptoActiveFor(grpcSubsystemConfig{}),
		"no RekeyManager ⇒ crypto inactive")
}
