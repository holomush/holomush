// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/grpc/focus"
)

// stubCoordinator embeds the interface and overrides only AutoFocusOnJoin, so
// it satisfies focus.Coordinator without implementing all methods.
type stubCoordinator struct {
	focus.Coordinator
	gotChar, gotScene ulid.ULID
	resp              focus.AutoFocusOnJoinResponse
}

func (s *stubCoordinator) AutoFocusOnJoin(_ context.Context, charID, sceneID ulid.ULID) (focus.AutoFocusOnJoinResponse, error) {
	s.gotChar, s.gotScene = charID, sceneID
	return s.resp, nil
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
