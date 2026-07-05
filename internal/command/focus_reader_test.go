// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/session"
)

type fakeConnGetter struct {
	conn *session.Connection
	err  error
}

func (f fakeConnGetter) GetConnection(_ context.Context, _ ulid.ULID) (*session.Connection, error) {
	return f.conn, f.err
}

func TestStoreFocusReaderReturnsSceneKindWhenSceneFocused(t *testing.T) {
	fk := &session.FocusKey{Kind: session.FocusKindScene, TargetID: ulid.Make()}
	r := command.NewStoreFocusReader(fakeConnGetter{conn: &session.Connection{FocusKey: fk}})
	kind, err := r.ConnectionFocusKind(context.Background(), ulid.Make())
	require.NoError(t, err)
	assert.Equal(t, session.FocusKindScene, kind)
}

func TestStoreFocusReaderReturnsEmptyKindWhenGridFocused(t *testing.T) {
	r := command.NewStoreFocusReader(fakeConnGetter{conn: &session.Connection{FocusKey: nil}})
	kind, err := r.ConnectionFocusKind(context.Background(), ulid.Make())
	require.NoError(t, err)
	assert.Equal(t, session.FocusKind(""), kind)
}

func TestStoreFocusReaderTreatsConnectionNotFoundAsAbsentFocus(t *testing.T) {
	r := command.NewStoreFocusReader(fakeConnGetter{err: oops.Code("CONNECTION_NOT_FOUND").Errorf("gone")})
	kind, err := r.ConnectionFocusKind(context.Background(), ulid.Make())
	require.NoError(t, err)
	assert.Equal(t, session.FocusKind(""), kind)
}

func TestStoreFocusReaderPropagatesInfraError(t *testing.T) {
	r := command.NewStoreFocusReader(fakeConnGetter{err: oops.Code("STORE_UNAVAILABLE").Errorf("db down")})
	_, err := r.ConnectionFocusKind(context.Background(), ulid.Make())
	require.Error(t, err)
}
