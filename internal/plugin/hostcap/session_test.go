// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/session"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// --- fake session.Access ----------------------------------------------------

type fakeSessionAccess struct {
	// findByCharacterNameResult is returned by FindByCharacterName.
	findByCharacterNameResult *session.Info
	findByCharacterNameErr    error

	// listActiveResult is returned by ListActive.
	listActiveResult []*session.Info
	listActiveErr    error

	// updateLastWhisperedErr is returned by UpdateLastWhispered.
	updateLastWhisperedErr error

	// captured args for assertion.
	lastWhisperedSessionID string
	lastWhisperedName      string
}

func (f *fakeSessionAccess) FindByCharacterName(_ context.Context, _ string) (*session.Info, error) {
	return f.findByCharacterNameResult, f.findByCharacterNameErr
}

func (f *fakeSessionAccess) ListActive(_ context.Context) ([]*session.Info, error) {
	return f.listActiveResult, f.listActiveErr
}

func (f *fakeSessionAccess) UpdateLastWhispered(_ context.Context, sessionID, name string) error {
	f.lastWhisperedSessionID = sessionID
	f.lastWhisperedName = name
	return f.updateLastWhisperedErr
}

// Unused session.Access methods required to satisfy the interface.
func (f *fakeSessionAccess) FindByCharacter(_ context.Context, _ ulid.ULID) (*session.Info, error) {
	return nil, nil
}

func (f *fakeSessionAccess) DeleteByCharacter(_ context.Context, _ ulid.ULID) (*session.Info, error) {
	return nil, nil
}

func (f *fakeSessionAccess) UpdateActivity(_ context.Context, _ string) error { return nil }

func (f *fakeSessionAccess) UpdateLastPaged(_ context.Context, _, _ string) error { return nil }

// --- fake SessionAdmin -------------------------------------------------------

type fakeSessionAdmin struct {
	broadcastErr      error
	disconnectErr     error
	lastBroadcastMsg  string
	lastDisconnectID  string
	lastDisconnectRsn string
}

func (f *fakeSessionAdmin) BroadcastSystemMessage(_ context.Context, message string) error {
	f.lastBroadcastMsg = message
	return f.broadcastErr
}

func (f *fakeSessionAdmin) DisconnectSession(_ context.Context, sessionID, reason string) error {
	f.lastDisconnectID = sessionID
	f.lastDisconnectRsn = reason
	return f.disconnectErr
}

// --- sessionHostCaps ---------------------------------------------------------

// sessionHostCaps is a focused HostCapabilities stub for session tests.
// It extends stubHostCaps with configurable session port methods.
type sessionHostCaps struct {
	stubHostCaps
	access *fakeSessionAccess
	admin  *fakeSessionAdmin
}

func (c *sessionHostCaps) SessionAccess() session.Access      { return c.access }
func (c *sessionHostCaps) SessionAdmin() hostcap.SessionAdmin { return c.admin }

// newSessionCaps returns a stub with the given fake access and admin ports.
func newSessionCaps(access *fakeSessionAccess, admin *fakeSessionAdmin) *sessionHostCaps {
	return &sessionHostCaps{access: access, admin: admin}
}

// sessionULID is a fixed ULID for use in tests.
var sessionULID = ulid.Make().String()

// characterULID is a fixed character ULID for session.Info population.
var characterULID = ulid.Make()

// locationULID is a fixed location ULID for session.Info population.
var locationULID = ulid.Make()

// makeSessionInfo builds a representative session.Info for assertions.
func makeSessionInfo() *session.Info {
	return &session.Info{
		ID:            sessionULID,
		CharacterID:   characterULID,
		CharacterName: "Alice",
		LocationID:    locationULID,
		GridPresent:   true,
		LastWhispered: "Bob",
	}
}

// ============================================================================
// SessionService (sessionServer) tests
// ============================================================================

// TestSessionServerFindByNameResolvesViaAccessReturnsSession verifies that
// FindByName delegates to session.Access.FindByCharacterName and maps the
// result to the wire SessionInfo.
func TestSessionServerFindByNameResolvesViaAccessReturnsSession(t *testing.T) {
	info := makeSessionInfo()
	access := &fakeSessionAccess{findByCharacterNameResult: info}
	caps := newSessionCaps(access, &fakeSessionAdmin{})

	srv := hostcap.NewSessionServer(hostcap.NewBase(caps, "core-communication"))
	resp, err := srv.FindByName(context.Background(), &hostv1.FindByNameRequest{Name: "alice"})
	require.NoError(t, err)
	require.NotNil(t, resp.GetSession())
	assert.Equal(t, sessionULID, resp.GetSession().GetId())
	assert.Equal(t, characterULID.String(), resp.GetSession().GetCharacterId())
	assert.Equal(t, "Alice", resp.GetSession().GetCharacterName())
	assert.Equal(t, locationULID.String(), resp.GetSession().GetLocationId())
	assert.True(t, resp.GetSession().GetGridPresent())
	assert.Equal(t, "Bob", resp.GetSession().GetLastWhispered())
}

// TestSessionServerFindByNameReturnsEmptySessionWhenNotFound verifies that
// FindByName returns an empty (nil session field) response when no session
// matches the name, mirroring the Lua nil-return semantics.
func TestSessionServerFindByNameReturnsEmptySessionWhenNotFound(t *testing.T) {
	access := &fakeSessionAccess{findByCharacterNameResult: nil, findByCharacterNameErr: nil}
	caps := newSessionCaps(access, &fakeSessionAdmin{})

	srv := hostcap.NewSessionServer(hostcap.NewBase(caps, "core-communication"))
	resp, err := srv.FindByName(context.Background(), &hostv1.FindByNameRequest{Name: "nobody"})
	require.NoError(t, err)
	assert.Nil(t, resp.GetSession(), "absent session must produce nil session field")
}

// TestSessionServerFindByNameReturnsOpaqueInternalErrorOnAccessFailure verifies
// that an internal error from session.Access does not leak inner details to the
// caller — the status message must be the static string "internal error".
func TestSessionServerFindByNameReturnsOpaqueInternalErrorOnAccessFailure(t *testing.T) {
	access := &fakeSessionAccess{findByCharacterNameErr: errors.New("secret pg connection string")}
	caps := newSessionCaps(access, &fakeSessionAdmin{})

	srv := hostcap.NewSessionServer(hostcap.NewBase(caps, "core-communication"))
	_, err := srv.FindByName(context.Background(), &hostv1.FindByNameRequest{Name: "alice"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal error", st.Message(), "inner error detail must not leak to the caller")
	assert.NotContains(t, st.Message(), "secret")
}

// TestSessionServerListActiveReturnsAllSessions verifies that ListActive
// delegates to session.Access.ListActive and maps every session to the wire
// format.
func TestSessionServerListActiveReturnsAllSessions(t *testing.T) {
	info := makeSessionInfo()
	access := &fakeSessionAccess{listActiveResult: []*session.Info{info}}
	caps := newSessionCaps(access, &fakeSessionAdmin{})

	srv := hostcap.NewSessionServer(hostcap.NewBase(caps, "core-communication"))
	resp, err := srv.ListActive(context.Background(), &hostv1.ListActiveRequest{})
	require.NoError(t, err)
	require.Len(t, resp.GetSessions(), 1)
	assert.Equal(t, sessionULID, resp.GetSessions()[0].GetId())
	assert.Equal(t, "Alice", resp.GetSessions()[0].GetCharacterName())
}

// TestSessionServerListActiveReturnsEmptySliceWhenNoneActive verifies that
// ListActive returns an empty sessions slice when there are no active sessions.
func TestSessionServerListActiveReturnsEmptySliceWhenNoneActive(t *testing.T) {
	access := &fakeSessionAccess{listActiveResult: nil}
	caps := newSessionCaps(access, &fakeSessionAdmin{})

	srv := hostcap.NewSessionServer(hostcap.NewBase(caps, "core-communication"))
	resp, err := srv.ListActive(context.Background(), &hostv1.ListActiveRequest{})
	require.NoError(t, err)
	assert.Empty(t, resp.GetSessions())
}

// TestSessionServerListActiveReturnsOpaqueInternalErrorOnAccessFailure verifies
// that an internal error from session.Access.ListActive does not leak inner
// details to the caller.
func TestSessionServerListActiveReturnsOpaqueInternalErrorOnAccessFailure(t *testing.T) {
	access := &fakeSessionAccess{listActiveErr: errors.New("secret replica lag detail")}
	caps := newSessionCaps(access, &fakeSessionAdmin{})

	srv := hostcap.NewSessionServer(hostcap.NewBase(caps, "core-communication"))
	_, err := srv.ListActive(context.Background(), &hostv1.ListActiveRequest{})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal error", st.Message(), "inner error detail must not leak to the caller")
	assert.NotContains(t, st.Message(), "secret")
}

// TestSessionServerSetLastWhisperedDelegatesToAccessUpdateLastWhispered verifies
// that SetLastWhispered calls session.Access.UpdateLastWhispered with the
// request's session_id and name and returns an empty response on success.
func TestSessionServerSetLastWhisperedDelegatesToAccessUpdateLastWhispered(t *testing.T) {
	access := &fakeSessionAccess{}
	caps := newSessionCaps(access, &fakeSessionAdmin{})

	srv := hostcap.NewSessionServer(hostcap.NewBase(caps, "core-communication"))
	resp, err := srv.SetLastWhispered(context.Background(), &hostv1.SetLastWhisperedRequest{
		SessionId: sessionULID,
		Name:      "Bob",
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, sessionULID, access.lastWhisperedSessionID)
	assert.Equal(t, "Bob", access.lastWhisperedName)
}

// TestSessionServerSetLastWhisperedReturnsOpaqueInternalErrorOnUpdateFailure
// verifies that an internal error from UpdateLastWhispered does not leak inner
// details to the caller.
func TestSessionServerSetLastWhisperedReturnsOpaqueInternalErrorOnUpdateFailure(t *testing.T) {
	access := &fakeSessionAccess{updateLastWhisperedErr: errors.New("secret store error")}
	caps := newSessionCaps(access, &fakeSessionAdmin{})

	srv := hostcap.NewSessionServer(hostcap.NewBase(caps, "core-communication"))
	_, err := srv.SetLastWhispered(context.Background(), &hostv1.SetLastWhisperedRequest{
		SessionId: sessionULID,
		Name:      "Bob",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal error", st.Message(), "inner error detail must not leak to the caller")
	assert.NotContains(t, st.Message(), "secret")
}

// ============================================================================
// SessionAdminService (sessionAdminServer) tests
// ============================================================================

// TestSessionAdminServerBroadcastDelegatesToSessionAdminBroadcastSystemMessage
// verifies that Broadcast calls SessionAdmin.BroadcastSystemMessage with the
// request message and returns an empty response on success.
func TestSessionAdminServerBroadcastDelegatesToSessionAdminBroadcastSystemMessage(t *testing.T) {
	admin := &fakeSessionAdmin{}
	caps := newSessionCaps(&fakeSessionAccess{}, admin)

	srv := hostcap.NewSessionAdminServer(hostcap.NewBase(caps, "core-communication"))
	resp, err := srv.Broadcast(context.Background(), &hostv1.BroadcastRequest{Message: "Server restart in 5 minutes."})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "Server restart in 5 minutes.", admin.lastBroadcastMsg)
}

// TestSessionAdminServerBroadcastReturnsOpaqueInternalErrorOnAdminFailure
// verifies that an internal error from BroadcastSystemMessage does not leak
// inner details to the caller — the status message must be "internal error".
func TestSessionAdminServerBroadcastReturnsOpaqueInternalErrorOnAdminFailure(t *testing.T) {
	admin := &fakeSessionAdmin{broadcastErr: errors.New("secret internal broadcast failure")}
	caps := newSessionCaps(&fakeSessionAccess{}, admin)

	srv := hostcap.NewSessionAdminServer(hostcap.NewBase(caps, "core-communication"))
	_, err := srv.Broadcast(context.Background(), &hostv1.BroadcastRequest{Message: "hello"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal error", st.Message(), "inner error detail must not leak to the caller")
	assert.NotContains(t, st.Message(), "secret")
}

// TestSessionAdminServerDisconnectDelegatesToSessionAdminDisconnectSession
// verifies that Disconnect calls SessionAdmin.DisconnectSession with the
// request's session_id and reason and returns an empty response on success.
func TestSessionAdminServerDisconnectDelegatesToSessionAdminDisconnectSession(t *testing.T) {
	admin := &fakeSessionAdmin{}
	caps := newSessionCaps(&fakeSessionAccess{}, admin)

	srv := hostcap.NewSessionAdminServer(hostcap.NewBase(caps, "core-communication"))
	resp, err := srv.Disconnect(context.Background(), &hostv1.DisconnectRequest{
		SessionId: sessionULID,
		Reason:    "idle timeout",
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, sessionULID, admin.lastDisconnectID)
	assert.Equal(t, "idle timeout", admin.lastDisconnectRsn)
}

// TestSessionAdminServerDisconnectReturnsOpaqueInternalErrorOnAdminFailure
// verifies that an internal error from DisconnectSession does not leak inner
// details to the caller.
func TestSessionAdminServerDisconnectReturnsOpaqueInternalErrorOnAdminFailure(t *testing.T) {
	admin := &fakeSessionAdmin{disconnectErr: errors.New("secret disconnect failure")}
	caps := newSessionCaps(&fakeSessionAccess{}, admin)

	srv := hostcap.NewSessionAdminServer(hostcap.NewBase(caps, "core-communication"))
	_, err := srv.Disconnect(context.Background(), &hostv1.DisconnectRequest{
		SessionId: sessionULID,
		Reason:    "reason",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal error", st.Message(), "inner error detail must not leak to the caller")
	assert.NotContains(t, st.Message(), "secret")
}

// TestSessionAdminServerBroadcastReturnsUnimplementedWhenAdminNil verifies the
// fail-closed nil-guard: when HostCapabilities.SessionAdmin() returns nil (the
// binary adapter AND the unwired Lua adapter both do today), Broadcast must
// return codes.Unimplemented rather than NPE on a nil-interface method call.
func TestSessionAdminServerBroadcastReturnsUnimplementedWhenAdminNil(t *testing.T) {
	// stubHostCaps.SessionAdmin() returns nil — the unwired case both runtimes hit.
	srv := hostcap.NewSessionAdminServer(hostcap.NewBase(stubHostCaps{}, "core-communication"))
	_, err := srv.Broadcast(context.Background(), &hostv1.BroadcastRequest{Message: "hello"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unimplemented, st.Code(), "nil SessionAdmin must fail closed with Unimplemented, not NPE")
}

// TestSessionAdminServerDisconnectReturnsUnimplementedWhenAdminNil verifies the
// same fail-closed nil-guard on the Disconnect path.
func TestSessionAdminServerDisconnectReturnsUnimplementedWhenAdminNil(t *testing.T) {
	srv := hostcap.NewSessionAdminServer(hostcap.NewBase(stubHostCaps{}, "core-communication"))
	_, err := srv.Disconnect(context.Background(), &hostv1.DisconnectRequest{
		SessionId: sessionULID,
		Reason:    "reason",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unimplemented, st.Code(), "nil SessionAdmin must fail closed with Unimplemented, not NPE")
}
