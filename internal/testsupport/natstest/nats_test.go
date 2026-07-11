// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package natstest_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/testsupport/natstest"
)

func TestNatstestStartsJetStreamNodeReachableViaAccountInfo(t *testing.T) {
	ctx := context.Background()

	env, err := natstest.StartNATS(ctx)
	require.NoError(t, err, "StartNATS should return a running container")
	t.Cleanup(func() {
		_ = env.Terminate(context.Background())
	})

	require.NotEmpty(t, env.URL, "env.URL should be a dialable client URL")

	conn := env.Conn(t)
	require.True(t, conn.IsConnected(), "connection should be established to the container")

	js, err := jetstream.New(conn)
	require.NoError(t, err, "jetstream context should initialize")

	infoCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err = js.AccountInfo(infoCtx)
	require.NoError(t, err, "JetStream AccountInfo should succeed against the -js node")
}

func TestNatstestConnHandsOutIndependentPerReplicaConnections(t *testing.T) {
	ctx := context.Background()

	env, err := natstest.StartNATS(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = env.Terminate(context.Background())
	})

	connA := env.Conn(t)
	connB := env.Conn(t)

	// Distinct connection objects, both live — the per-replica shape that the
	// shared in-process eventbustest connection cannot express (CLUSTER-03).
	assert.NotSame(t, connA, connB, "each Conn call must yield an independent *nats.Conn")
	assert.True(t, connA.IsConnected())
	assert.True(t, connB.IsConnected())

	// Distinct server-assigned client IDs prove two real, separate sessions on
	// the broker rather than one multiplexed handle.
	idA, err := connA.GetClientID()
	require.NoError(t, err)
	idB, err := connB.GetClientID()
	require.NoError(t, err)
	assert.NotEqual(t, idA, idB, "independent connections must have distinct broker client IDs")
}
