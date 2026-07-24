// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focuscontract_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/focuscontract"
	"github.com/holomush/holomush/internal/session"
)

// fakeCoordinator is a no-op focuscontract.Coordinator implementation declared
// entirely outside internal/grpc. Its existence proves the focus contract is
// implementable by packages that must not import the grpc tree.
type fakeCoordinator struct{}

func (*fakeCoordinator) JoinFocus(context.Context, string, session.FocusKey) error { return nil }

func (*fakeCoordinator) LeaveFocus(context.Context, string, session.FocusKey) error { return nil }

func (*fakeCoordinator) LeaveFocusByTarget(
	context.Context,
	session.FocusKey,
) (session.LeaveByTargetResult, error) {
	return session.LeaveByTargetResult{}, nil
}

func (*fakeCoordinator) PresentFocus(context.Context, string, session.FocusKey) error { return nil }

func (*fakeCoordinator) RestoreFocus(context.Context, string) (focuscontract.RestorePlan, error) {
	return focuscontract.RestorePlan{}, nil
}

func (*fakeCoordinator) IsAnyConnFocused(context.Context, ulid.ULID, ulid.ULID) (bool, error) {
	return false, nil
}

func (*fakeCoordinator) RestoreConnectionFocus(context.Context, string, ulid.ULID) error {
	return nil
}

func (*fakeCoordinator) SetConnectionFocus(
	context.Context,
	ulid.ULID,
	*session.FocusKey,
	bool,
) (focuscontract.SetConnectionFocusResult, error) {
	return focuscontract.SetConnectionFocusResult{}, nil
}

func (*fakeCoordinator) AutoFocusOnJoin(
	context.Context,
	ulid.ULID,
	ulid.ULID,
) (focuscontract.AutoFocusOnJoinResponse, error) {
	return focuscontract.AutoFocusOnJoinResponse{}, nil
}

func (*fakeCoordinator) GetConnectionFocus(
	context.Context,
	ulid.ULID,
) (*session.FocusKey, error) {
	return nil, nil //nolint:nilnil // absent focus is the documented nil,nil case
}

// Compile-time proof that a type declared outside internal/grpc satisfies the
// contract. This assertion is the whole point of the focuscontract leaf.
var _ focuscontract.Coordinator = (*fakeCoordinator)(nil)

func TestFakeCoordinatorSatisfiesContractFromOutsideGRPC(t *testing.T) {
	var coord focuscontract.Coordinator = &fakeCoordinator{}
	require.NotNil(t, coord)

	plan, err := coord.RestoreFocus(t.Context(), "session-1")
	require.NoError(t, err)
	assert.Empty(t, plan.Streams)
}

func TestRestorePlanCarriesStreamsWithReplayModeAndTailCount(t *testing.T) {
	notBefore := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)

	plan := focuscontract.RestorePlan{
		Streams: []focuscontract.StreamWithMode{{
			Stream:    "scene:01ARZ3NDEKTSV4RRFFQ69G5FAV",
			Mode:      focuscontract.ReplayModeBoundedTail,
			TailCount: 25,
			NotBefore: notBefore,
		}},
		PresentingStream: "scene:01ARZ3NDEKTSV4RRFFQ69G5FAV",
	}

	require.Len(t, plan.Streams, 1)
	assert.Equal(t, "scene:01ARZ3NDEKTSV4RRFFQ69G5FAV", plan.Streams[0].Stream)
	assert.Equal(t, focuscontract.ReplayModeBoundedTail, plan.Streams[0].Mode)
	assert.Equal(t, session.ReplayModeBoundedTail, plan.Streams[0].Mode)
	assert.Equal(t, 25, plan.Streams[0].TailCount)
	assert.Equal(t, notBefore, plan.Streams[0].NotBefore)
	assert.Equal(t, "scene:01ARZ3NDEKTSV4RRFFQ69G5FAV", plan.PresentingStream)
}

func TestAutoFocusOnJoinResponseCarriesPerConnectionFailures(t *testing.T) {
	connID := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	locID := ulid.MustParse("01BX5ZZKBKACTAV9WEVGEMMVRZ")

	resp := focuscontract.AutoFocusOnJoinResponse{
		SessionID:            "session-1",
		CharLocationID:       locID,
		FocusedConnectionIDs: []ulid.ULID{connID},
		SkippedConnectionIDs: []ulid.ULID{},
		FailedConnectionIDs: []focuscontract.AutoFocusFailure{{
			ConnectionID: connID,
			Reason:       "membership_absent",
		}},
		TotalConnectionCount: 2,
	}

	require.Len(t, resp.FailedConnectionIDs, 1)
	assert.Equal(t, connID, resp.FailedConnectionIDs[0].ConnectionID)
	assert.Equal(t, "membership_absent", resp.FailedConnectionIDs[0].Reason)
	assert.Equal(t, locID, resp.CharLocationID)
	assert.Equal(t, "session-1", resp.SessionID)
	assert.Equal(t, uint32(2), resp.TotalConnectionCount)
}

func TestSetConnectionFocusResultCarriesPriorFocusKey(t *testing.T) {
	locID := ulid.MustParse("01BX5ZZKBKACTAV9WEVGEMMVRZ")
	prior := session.FocusKey{}

	res := focuscontract.SetConnectionFocusResult{
		OldFocusKey:    &prior,
		SessionID:      "session-1",
		CharLocationID: locID,
	}

	require.NotNil(t, res.OldFocusKey)
	assert.Equal(t, "session-1", res.SessionID)
	assert.Equal(t, locID, res.CharLocationID)
}
