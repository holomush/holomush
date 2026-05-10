// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package policy_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/policy"
	"github.com/holomush/holomush/internal/eventbus"
)

// fakePublisher captures Publish calls.
type fakePublisher struct {
	mu   sync.Mutex
	evts []eventbus.Event
	err  error
}

func (p *fakePublisher) Publish(_ context.Context, e eventbus.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.evts = append(p.evts, e)
	return nil
}

func (p *fakePublisher) Events() []eventbus.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]eventbus.Event, len(p.evts))
	copy(out, p.evts)
	return out
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// emitDeps builds EmitDeps for the given gameID with a fakePublisher and config.
// Using per-test gameIDs gives each test an isolated subject namespace.
func emitDeps(t *testing.T, gameID string, pub *fakePublisher, cfg policy.CryptoEffectiveConfig) policy.EmitDeps {
	t.Helper()
	return policy.EmitDeps{
		GameID:          gameID,
		ServerStartULID: ulid.Make().String(),
		ServerIdentity:  "holomush@test",
		Pool:            testPool,
		Publisher:       pub,
		Clock:           fixedClock{t: time.Unix(1700000000, 0).UTC()},
		Config:          cfg,
	}
}

// cleanupSubject registers a t.Cleanup to delete rows for the given subject.
func cleanupSubject(t *testing.T, subject string) {
	t.Helper()
	_, _ = testPool.Exec(context.Background(), `DELETE FROM events_audit WHERE subject = $1`, subject)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM events_audit WHERE subject = $1`, subject)
	})
}

// TestEmitCurrentSnapshotGenesis — empty events_audit, emit publishes one
// event with prev_hash=nil.
func TestEmitCurrentSnapshotGenesis(t *testing.T) {
	const policyName = "dual_control_required"
	const gameID = "genesis-game"
	subject := "events." + gameID + ".system.crypto_policy." + policyName
	cleanupSubject(t, subject)

	pub := &fakePublisher{}
	deps := emitDeps(t, gameID, pub, policy.CryptoEffectiveConfig{DualControlRequired: []string{"rekey"}})
	require.NoError(t, policy.EmitCurrentSnapshot(context.Background(), deps, policyName))
	require.Len(t, pub.Events(), 1)
}

// TestEmitCurrentSnapshotExtension — pre-seed an existing chain row,
// emit reads it, computes its hash as new event's prev_hash.
func TestEmitCurrentSnapshotExtension(t *testing.T) {
	const policyName = "dual_control_required"
	const gameID = "ext-game"
	subject := "events." + gameID + ".system.crypto_policy." + policyName
	cleanupSubject(t, subject)

	// Seed a prior row with one op.
	priorPayload := policy.PolicySetPayload{
		PolicyName:      policyName,
		PolicySnapshot:  map[string]any{"required_op_kinds": []any{"rekey"}},
		PrevHash:        nil,
		ServerStartULID: "01HZSEED0000000000000000A",
		ServerIdentity:  "holomush@seed",
		Timestamp:       time.Unix(1700000000, 0).UTC(),
	}
	priorHash, err := policy.ComputePolicyHash(&priorPayload)
	require.NoError(t, err)
	priorPayload.PolicyHash = priorHash
	insertChainRow(t, subject, 500, priorPayload)

	pub := &fakePublisher{}
	// Config adds a second op — snapshot differs from seeded row → publish.
	deps := emitDeps(t, gameID, pub, policy.CryptoEffectiveConfig{DualControlRequired: []string{"rekey", "admin_read_stream"}})
	deps.Clock = fixedClock{t: time.Unix(1700000060, 0).UTC()}
	require.NoError(t, policy.EmitCurrentSnapshot(context.Background(), deps, policyName))
	require.Len(t, pub.Events(), 1, "extension should publish exactly one event")
}

// TestEmitCurrentSnapshotIdempotentOnNoChange — pre-seed with same effective
// config as deps.Config. Emit returns nil and does not publish.
func TestEmitCurrentSnapshotIdempotentOnNoChange(t *testing.T) {
	const policyName = "dual_control_required"
	const gameID = "idem-game"
	subject := "events." + gameID + ".system.crypto_policy." + policyName
	cleanupSubject(t, subject)

	// Seed a row whose snapshot matches what snapshotFromConfig will produce.
	// snapshotFromConfig("dual_control_required", {DualControlRequired:["rekey"]}) →
	// {"required_op_kinds":["rekey"]}
	priorPayload := policy.PolicySetPayload{
		PolicyName:      policyName,
		PolicySnapshot:  map[string]any{"required_op_kinds": []any{"rekey"}},
		PrevHash:        nil,
		ServerStartULID: "01HZSEED0000000000000000B",
		ServerIdentity:  "holomush@seed",
		Timestamp:       time.Unix(1700000000, 0).UTC(),
	}
	priorHash, err := policy.ComputePolicyHash(&priorPayload)
	require.NoError(t, err)
	priorPayload.PolicyHash = priorHash
	insertChainRow(t, subject, 600, priorPayload)

	pub := &fakePublisher{}
	// Same config as what's seeded → idempotent skip.
	deps := emitDeps(t, gameID, pub, policy.CryptoEffectiveConfig{DualControlRequired: []string{"rekey"}})
	deps.Clock = fixedClock{t: time.Unix(1700000060, 0).UTC()}
	require.NoError(t, policy.EmitCurrentSnapshot(context.Background(), deps, policyName))
	assert.Empty(t, pub.Events(), "no-change emit must NOT publish")
}

// TestEmitCurrentSnapshotFailsOnPublishError — Publisher returns error;
// EmitCurrentSnapshot returns wrapped error with POLICY_EMIT_PUBLISH_FAILED.
func TestEmitCurrentSnapshotFailsOnPublishError(t *testing.T) {
	const policyName = "dual_control_required"
	const gameID = "fail-game"
	subject := "events." + gameID + ".system.crypto_policy." + policyName
	cleanupSubject(t, subject)

	pub := &fakePublisher{err: errors.New("simulated publish failure")}
	deps := emitDeps(t, gameID, pub, policy.CryptoEffectiveConfig{DualControlRequired: []string{"rekey"}})
	err := policy.EmitCurrentSnapshot(context.Background(), deps, policyName)
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "POLICY_EMIT_PUBLISH_FAILED", o.Code())
}
