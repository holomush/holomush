// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/policy"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/lifecycle"
)

// subsystemFakePub captures Publish calls for unit tests.
type subsystemFakePub struct {
	mu   sync.Mutex
	evts []eventbus.Event
	err  error
}

func (p *subsystemFakePub) Publish(_ context.Context, e eventbus.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.evts = append(p.evts, e)
	return nil
}

func (p *subsystemFakePub) Events() []eventbus.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]eventbus.Event, len(p.evts))
	copy(out, p.evts)
	return out
}

type subsystemClock struct{ t time.Time }

func (c subsystemClock) Now() time.Time { return c.t }

func TestCryptoPolicySubsystemIDReturnsCryptoPolicy(t *testing.T) {
	s := policy.NewCryptoPolicySubsystem(policy.CryptoPolicySubsystemConfig{})
	assert.Equal(t, lifecycle.SubsystemCryptoPolicy, s.ID())
}

// TestCryptoPolicySubsystemDependsOnAuditProjectionAndCryptoWiringSuperset
// asserts the exact grown DependsOn set (07-09 item 9) — AuditProjection
// plus THE RULE's wiring consumer superset {Database, Auth, ABAC,
// EventBus}.
func TestCryptoPolicySubsystemDependsOnAuditProjectionAndCryptoWiringSuperset(t *testing.T) {
	s := policy.NewCryptoPolicySubsystem(policy.CryptoPolicySubsystemConfig{})
	assert.Equal(t, []lifecycle.SubsystemID{
		lifecycle.SubsystemAuditProjection,
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemAuth,
		lifecycle.SubsystemABAC,
		lifecycle.SubsystemEventBus,
	}, s.DependsOn())
}

func TestCryptoPolicySubsystemStopIsNoOp(t *testing.T) {
	s := policy.NewCryptoPolicySubsystem(policy.CryptoPolicySubsystemConfig{})
	require.NoError(t, s.Stop(context.Background()))
}

// TestCryptoPolicySubsystemActivateEmitsNothingForEmptyPolicyNames documents
// the loop's no-op shape: an empty PolicyNames list returns nil and
// publishes nothing.
func TestCryptoPolicySubsystemActivateEmitsNothingForEmptyPolicyNames(t *testing.T) {
	pub := &subsystemFakePub{}
	s := policy.NewCryptoPolicySubsystem(policy.CryptoPolicySubsystemConfig{
		EmitDeps: policy.EmitDeps{
			GameID:          "testgame",
			ServerStartULID: ulid.Make().String(),
			ServerIdentity:  "holomush@test",
			Pool:            nil, // not used when PolicyNames is empty
			Publisher:       pub,
			Clock:           subsystemClock{t: time.Unix(1700000000, 0).UTC()},
		},
		PolicyNames: nil,
	})
	require.NoError(t, s.Prepare(context.Background()))
	require.NoError(t, s.Activate(context.Background()))
	assert.Empty(t, pub.Events())
}

// The repeated-Activate no-op and mid-loop-failure retry-suffix behaviors
// (D-13.2 row 15) require a real Postgres pool (EmitCurrentSnapshot's
// loadChainEntries queries events_audit) and are proven at integration tier
// in subsystem_integration_test.go.
