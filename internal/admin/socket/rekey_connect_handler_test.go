// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/samber/oops"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/admin/socket"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// trackingDenyMapper records each error it receives and returns the
// configured mapped error. Used to assert the connect adapter routes
// inner errors through the deny-mapper exactly once.
type trackingDenyMapper struct {
	calls  int
	last   error
	mapped error
}

func (m *trackingDenyMapper) Map(err error) error {
	m.calls++
	m.last = err
	return m.mapped
}

// newConnectHandlerWithBrokenSession builds a RekeyConnectHandler whose
// inner RekeyHandler will fail at the session-validation step (because the
// configured token does not match), producing a deterministic
// DENY_SESSION_INVALID oops error that the deny mapper then receives.
func newConnectHandlerWithBrokenSession(
	t *testing.T,
	mapper *trackingDenyMapper,
) *socket.RekeyConnectHandler {
	t.Helper()
	sessions := &fakeRekeySessionStore{
		token:    rekeyTestToken,
		identity: socket.OperatorSession{PlayerID: rekeyTestPlayerID, TOTPVerified: true},
	}
	grants := &fakeRekeyResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRekeyRoleChecker{roles: map[string][]string{rekeyTestPlayerID: {access.RoleAdmin}}}
	repo := &fakeCheckpointRepo{}
	inner := socket.NewRekeyHandler(sessions, grants, roles, &fakeOrchRunner{}, &fakeAbortRunner{}, repo)
	return socket.NewRekeyConnectHandler(inner, mapper.Map)
}

// newConnectHandlerHappy builds a RekeyConnectHandler whose inner
// RekeyHandler succeeds on the unary paths (RekeyAbort, RekeyStatus).
// The fake checkpoint repo carries one matching row keyed by rekeyTestRID.
func newConnectHandlerHappy(
	t *testing.T,
	mapper *trackingDenyMapper,
) *socket.RekeyConnectHandler {
	t.Helper()
	sessions := &fakeRekeySessionStore{
		token:    rekeyTestToken,
		identity: socket.OperatorSession{PlayerID: rekeyTestPlayerID, TOTPVerified: true},
	}
	grants := &fakeRekeyResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRekeyRoleChecker{roles: map[string][]string{rekeyTestPlayerID: {access.RoleAdmin}}}
	repo := &fakeCheckpointRepo{
		byID: map[[16]byte]socket.CheckpointView{
			rekeyTestRID: seedView(rekeyTestRID, "phase1_complete", "01CTX"),
		},
	}
	inner := socket.NewRekeyHandler(sessions, grants, roles, &fakeOrchRunner{}, &fakeAbortRunner{}, repo)
	return socket.NewRekeyConnectHandler(inner, mapper.Map)
}

// TestRekeyConnectHandler_DenyMapperWiring verifies that for every RPC
// method on the connect adapter, an error from the inner handler is routed
// through the deny mapper exactly once and the mapper's mapped error is
// returned to the caller verbatim. Streaming methods are exercised with a
// nil ServerStream because the inner method returns before touching the
// stream when session validation fails.
func TestRekeyConnectHandler_DenyMapperWiring(t *testing.T) {
	mapped := oops.Code("MAPPED").Errorf("mapped sentinel")

	t.Run("HandleRekey routes inner error through deny mapper", func(t *testing.T) {
		mapper := &trackingDenyMapper{mapped: mapped}
		h := newConnectHandlerWithBrokenSession(t, mapper)
		err := h.HandleRekey(context.Background(),
			connect.NewRequest(&adminv1.RekeyRequest{SessionToken: "wrong"}),
			nil)
		require.Equal(t, 1, mapper.calls, "deny mapper must be called exactly once")
		require.Error(t, mapper.last, "deny mapper must receive the inner error")
		oopsErr, ok := oops.AsOops(mapper.last)
		require.True(t, ok)
		require.Equal(t, "DENY_SESSION_INVALID", oopsErr.Code())
		require.ErrorIs(t, err, mapped, "adapter must return the mapper's mapped error")
	})

	t.Run("HandleRekeyResume routes inner error through deny mapper", func(t *testing.T) {
		mapper := &trackingDenyMapper{mapped: mapped}
		h := newConnectHandlerWithBrokenSession(t, mapper)
		err := h.HandleRekeyResume(context.Background(),
			connect.NewRequest(&adminv1.RekeyResumeRequest{SessionToken: "wrong"}),
			nil)
		require.Equal(t, 1, mapper.calls)
		require.ErrorIs(t, err, mapped)
	})

	t.Run("HandleRekeyAbort routes inner error through deny mapper", func(t *testing.T) {
		mapper := &trackingDenyMapper{mapped: mapped}
		h := newConnectHandlerWithBrokenSession(t, mapper)
		_, err := h.HandleRekeyAbort(context.Background(),
			connect.NewRequest(&adminv1.RekeyAbortRequest{SessionToken: "wrong"}))
		require.Equal(t, 1, mapper.calls)
		require.ErrorIs(t, err, mapped)
	})

	t.Run("HandleRekeyStatus routes inner error through deny mapper", func(t *testing.T) {
		mapper := &trackingDenyMapper{mapped: mapped}
		h := newConnectHandlerWithBrokenSession(t, mapper)
		_, err := h.HandleRekeyStatus(context.Background(),
			connect.NewRequest(&adminv1.RekeyStatusRequest{SessionToken: "wrong"}))
		require.Equal(t, 1, mapper.calls)
		require.ErrorIs(t, err, mapped)
	})

	t.Run("HandleRekeyList routes inner error through deny mapper", func(t *testing.T) {
		mapper := &trackingDenyMapper{mapped: mapped}
		h := newConnectHandlerWithBrokenSession(t, mapper)
		err := h.HandleRekeyList(context.Background(),
			connect.NewRequest(&adminv1.RekeyListRequest{SessionToken: "wrong"}),
			nil)
		require.Equal(t, 1, mapper.calls)
		require.ErrorIs(t, err, mapped)
	})
}

// TestRekeyConnectHandler_SuccessPath verifies that on the unary success
// paths (RekeyAbort, RekeyStatus) the deny mapper is NOT called and the
// adapter returns the inner handler's response wrapped in a connect.Response.
// The streaming success paths cannot be exercised here because
// *connect.ServerStream has no exported constructor; their non-error
// behavior is already covered indirectly by the underlying RekeyHandler
// tests.
func TestRekeyConnectHandler_SuccessPath(t *testing.T) {
	t.Run("HandleRekeyAbort success skips deny mapper", func(t *testing.T) {
		mapper := &trackingDenyMapper{mapped: oops.Errorf("should-not-be-returned")}
		h := newConnectHandlerHappy(t, mapper)
		res, err := h.HandleRekeyAbort(context.Background(),
			connect.NewRequest(&adminv1.RekeyAbortRequest{
				SessionToken: rekeyTestToken,
				RequestId:    rekeyTestRID[:],
			}))
		require.NoError(t, err)
		require.Equal(t, 0, mapper.calls, "deny mapper must not be called on success")
		require.NotNil(t, res)
		require.NotNil(t, res.Msg, "adapter must wrap inner response in connect.Response")
	})

	t.Run("HandleRekeyStatus success skips deny mapper", func(t *testing.T) {
		mapper := &trackingDenyMapper{mapped: oops.Errorf("should-not-be-returned")}
		h := newConnectHandlerHappy(t, mapper)
		res, err := h.HandleRekeyStatus(context.Background(),
			connect.NewRequest(&adminv1.RekeyStatusRequest{
				SessionToken: rekeyTestToken,
				RequestId:    rekeyTestRID[:],
			}))
		require.NoError(t, err)
		require.Equal(t, 0, mapper.calls, "deny mapper must not be called on success")
		require.NotNil(t, res)
		require.NotNil(t, res.Msg)
		require.Equal(t, rekeyTestRID[:], res.Msg.RequestId, "inner response must be the wrapped payload")
		require.Equal(t, "phase1_complete", res.Msg.Status)
	})
}
