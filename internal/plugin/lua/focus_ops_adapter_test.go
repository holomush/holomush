// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
)

// stubCoordinator embeds the interface and overrides only the methods under
// test, so it satisfies focus.Coordinator without implementing all methods.
type stubCoordinator struct {
	focus.Coordinator
	gotChar, gotScene ulid.ULID
	resp              focus.AutoFocusOnJoinResponse

	// SetConnectionFocus capture.
	gotConnID      ulid.ULID
	gotFocusKey    *session.FocusKey
	gotIsSceneGrid bool
	setConnErr     error
}

func (s *stubCoordinator) AutoFocusOnJoin(_ context.Context, charID, sceneID ulid.ULID) (focus.AutoFocusOnJoinResponse, error) {
	s.gotChar, s.gotScene = charID, sceneID
	return s.resp, nil
}

func (s *stubCoordinator) SetConnectionFocus(_ context.Context, connectionID ulid.ULID, focusKey *session.FocusKey, isSceneGrid bool) (focus.SetConnectionFocusResult, error) {
	s.gotConnID, s.gotFocusKey, s.gotIsSceneGrid = connectionID, focusKey, isSceneGrid
	return focus.SetConnectionFocusResult{}, s.setConnErr
}

func TestLuaAdapterAutoFocusOnJoinDelegatesAndTranslates(t *testing.T) {
	charID := ulid.Make()
	sceneID := ulid.Make()
	focusedConn := ulid.Make()
	stub := &stubCoordinator{resp: focus.AutoFocusOnJoinResponse{
		FocusedConnectionIDs: []ulid.ULID{focusedConn},
		TotalConnectionCount: 1,
	}}
	adapter := &coordinatorFocusOpsAdapter{c: stub}

	focused, skipped, failed, total, err := adapter.AutoFocusOnJoin(context.Background(), charID, sceneID)
	require.NoError(t, err)
	assert.Equal(t, charID, stub.gotChar, "adapter MUST forward characterID to the coordinator")
	assert.Equal(t, sceneID, stub.gotScene, "adapter MUST forward sceneID to the coordinator")
	assert.Equal(t, []ulid.ULID{focusedConn}, focused)
	assert.Empty(t, skipped)
	assert.Empty(t, failed)
	assert.Equal(t, uint32(1), total)
}

func TestLuaAdapterSetConnectionFocusDelegates(t *testing.T) {
	connID := ulid.Make()
	sceneID := ulid.Make()
	fk := &session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}
	stub := &stubCoordinator{}
	adapter := &coordinatorFocusOpsAdapter{c: stub}

	err := adapter.SetConnectionFocus(context.Background(), connID, fk, false)
	require.NoError(t, err)
	assert.Equal(t, connID, stub.gotConnID, "adapter MUST forward connectionID to the coordinator")
	assert.Equal(t, fk, stub.gotFocusKey, "adapter MUST forward focusKey to the coordinator")
	assert.False(t, stub.gotIsSceneGrid, "adapter MUST forward isSceneGrid to the coordinator")
}

func TestLuaAdapterSetConnectionFocusPropagatesError(t *testing.T) {
	wantErr := errors.New("boom")
	stub := &stubCoordinator{setConnErr: wantErr}
	adapter := &coordinatorFocusOpsAdapter{c: stub}

	err := adapter.SetConnectionFocus(context.Background(), ulid.Make(), nil, true)
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr, "adapter MUST propagate the coordinator's error")
	assert.True(t, stub.gotIsSceneGrid, "adapter MUST forward isSceneGrid=true (scene→grid)")
}
