// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package setup_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/setup"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/test/testutil"
)

func TestBuildABACStack_WarnsWhenPropertyRepoMissing(t *testing.T) {
	// Capture slog output; assert the WARN names the affected seeds.
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	_, err = setup.BuildABACStack(context.Background(), setup.ABACConfig{
		Pool:          pool,
		CharacterRepo: worldpostgres.NewCharacterRepository(pool),
		LocationRepo:  worldpostgres.NewLocationRepository(pool),
		ObjectRepo:    worldpostgres.NewObjectRepository(pool),
		// PropertyRepo + ParentLocationResolver INTENTIONALLY OMITTED — exercises
		// the 8d. else-branch WARN path. Per holomush-72ou INV-PLUGIN-25.
	})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "PropertyRepo or ParentLocationResolver not provided")
	assert.Contains(t, out, "seed:property-public-read")
	assert.Contains(t, out, "holomush-72ou")
}
