// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package audit

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/eventbustest"
)

type retryFixedJS struct{ js jetstream.JetStream }

func (r retryFixedJS) JS() jetstream.JetStream { return r.js }

type retryFixedPool struct{ pool *pgxpool.Pool }

func (r retryFixedPool) Pool() *pgxpool.Pool { return r.pool }

// TestAuditSubsystemPrepareFailureAfterLateInitRestoresPreviousState pins
// D-13.2 row 10 (round 8): lateInit writes s.cfg.Owners/s.pluginMgr BEFORE
// the fallible newProjection call. When newProjection fails, Prepare MUST
// restore both to their pre-lateInit values so a retry does not observe
// stale ownership/consumer wiring from the failed attempt.
//
// The failure is injected via an invalid consumer name (containing a
// space) — NATS rejects it as a durable consumer name, so
// createConsumerWithRetry / CreateOrUpdateConsumer fails deterministically
// AFTER the boot gate and lateInit have already run.
func TestAuditSubsystemPrepareFailureAfterLateInitRestoresPreviousState(t *testing.T) {
	pool := auditIdemPool(t)
	bus := eventbustest.New(t)

	sub := NewSubsystem(retryFixedJS{js: bus.JS}, retryFixedPool{pool: pool}, Config{
		ConsumerName: "invalid consumer name", // space is rejected by NATS
	})

	firstOwners, err := NewOwnerMap(nil)
	require.NoError(t, err)
	firstPCM := NewPluginConsumerManager(bus.JS)
	sub.SetLateInitProvider(func() (*OwnerMap, *PluginConsumerManager) {
		return firstOwners, firstPCM
	})

	prepErr := sub.Prepare(context.Background())
	require.Error(t, prepErr, "invalid consumer name must fail newProjection")

	// The failed Prepare must restore the pre-lateInit state: both fields
	// started nil (this Subsystem was never successfully prepared before),
	// so they must be nil again after the failure — not left holding
	// firstOwners/firstPCM from the failed attempt's lateInit call.
	sub.mu.Lock()
	gotOwners := sub.cfg.Owners
	gotPCM := sub.pluginMgr
	gotPrepared := sub.preparedProjection
	sub.mu.Unlock()

	assert.Nil(t, gotOwners, "a failed Prepare must not leave the failed attempt's Owners in place")
	assert.Nil(t, gotPCM, "a failed Prepare must not leave the failed attempt's pluginMgr in place")
	assert.Nil(t, gotPrepared, "a failed Prepare must not leave a partially-set preparedProjection")

	require.NoError(t, sub.Stop(context.Background()))
}
