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

// requireOpaqueInternal asserts err is a gRPC Internal status whose message is
// the static "internal error" and leaks no inner detail (grpc-errors.md). Shared
// by every session/admin error-path case below.
func requireOpaqueInternal(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal error", st.Message(), "inner error detail must not leak to the caller")
	assert.NotContains(t, st.Message(), "secret")
}

// newSessionServer builds a SessionService server over the given access port.
func newSessionServer(access *fakeSessionAccess) hostv1.SessionServiceServer {
	return hostcap.NewSessionServer(hostcap.NewBase(newSessionCaps(access, &fakeSessionAdmin{}), "core-communication"))
}

// ============================================================================
// SessionService (sessionServer) tests
// ============================================================================

func TestSessionServerFindByName(t *testing.T) {
	tests := []struct {
		name   string
		access *fakeSessionAccess
		check  func(t *testing.T, resp *hostv1.FindByNameResponse, err error)
	}{
		{
			name:   "resolves via access and maps to wire session",
			access: &fakeSessionAccess{findByCharacterNameResult: makeSessionInfo()},
			check: func(t *testing.T, resp *hostv1.FindByNameResponse, err error) {
				require.NoError(t, err)
				require.NotNil(t, resp.GetSession())
				assert.Equal(t, sessionULID, resp.GetSession().GetId())
				assert.Equal(t, characterULID.String(), resp.GetSession().GetCharacterId())
				assert.Equal(t, "Alice", resp.GetSession().GetCharacterName())
				assert.Equal(t, locationULID.String(), resp.GetSession().GetLocationId())
				assert.True(t, resp.GetSession().GetGridPresent())
				assert.Equal(t, "Bob", resp.GetSession().GetLastWhispered())
			},
		},
		{
			name:   "returns empty session when not found",
			access: &fakeSessionAccess{},
			check: func(t *testing.T, resp *hostv1.FindByNameResponse, err error) {
				require.NoError(t, err)
				assert.Nil(t, resp.GetSession(), "absent session must produce nil session field")
			},
		},
		{
			name:   "returns opaque internal error on access failure",
			access: &fakeSessionAccess{findByCharacterNameErr: errors.New("secret pg connection string")},
			check: func(t *testing.T, _ *hostv1.FindByNameResponse, err error) {
				requireOpaqueInternal(t, err)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newSessionServer(tc.access)
			resp, err := srv.FindByName(context.Background(), &hostv1.FindByNameRequest{Name: "alice"})
			tc.check(t, resp, err)
		})
	}
}

func TestSessionServerListActive(t *testing.T) {
	tests := []struct {
		name   string
		access *fakeSessionAccess
		check  func(t *testing.T, resp *hostv1.ListActiveResponse, err error)
	}{
		{
			name:   "returns all active sessions mapped to wire format",
			access: &fakeSessionAccess{listActiveResult: []*session.Info{makeSessionInfo()}},
			check: func(t *testing.T, resp *hostv1.ListActiveResponse, err error) {
				require.NoError(t, err)
				require.Len(t, resp.GetSessions(), 1)
				assert.Equal(t, sessionULID, resp.GetSessions()[0].GetId())
				assert.Equal(t, "Alice", resp.GetSessions()[0].GetCharacterName())
			},
		},
		{
			name:   "returns empty slice when none active",
			access: &fakeSessionAccess{listActiveResult: nil},
			check: func(t *testing.T, resp *hostv1.ListActiveResponse, err error) {
				require.NoError(t, err)
				assert.Empty(t, resp.GetSessions())
			},
		},
		{
			name:   "returns opaque internal error on access failure",
			access: &fakeSessionAccess{listActiveErr: errors.New("secret replica lag detail")},
			check: func(t *testing.T, _ *hostv1.ListActiveResponse, err error) {
				requireOpaqueInternal(t, err)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newSessionServer(tc.access)
			resp, err := srv.ListActive(context.Background(), &hostv1.ListActiveRequest{})
			tc.check(t, resp, err)
		})
	}
}

func TestSessionServerSetLastWhispered(t *testing.T) {
	tests := []struct {
		name   string
		access *fakeSessionAccess
		check  func(t *testing.T, access *fakeSessionAccess, resp *hostv1.SetLastWhisperedResponse, err error)
	}{
		{
			name:   "delegates session_id and name to UpdateLastWhispered",
			access: &fakeSessionAccess{},
			check: func(t *testing.T, access *fakeSessionAccess, resp *hostv1.SetLastWhisperedResponse, err error) {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				assert.Equal(t, sessionULID, access.lastWhisperedSessionID)
				assert.Equal(t, "Bob", access.lastWhisperedName)
			},
		},
		{
			name:   "returns opaque internal error on update failure",
			access: &fakeSessionAccess{updateLastWhisperedErr: errors.New("secret store error")},
			check: func(t *testing.T, _ *fakeSessionAccess, _ *hostv1.SetLastWhisperedResponse, err error) {
				requireOpaqueInternal(t, err)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newSessionServer(tc.access)
			resp, err := srv.SetLastWhispered(context.Background(), &hostv1.SetLastWhisperedRequest{
				SessionId: sessionULID,
				Name:      "Bob",
			})
			tc.check(t, tc.access, resp, err)
		})
	}
}

// ============================================================================
// SessionAdminService (sessionAdminServer) tests
// ============================================================================

func TestSessionAdminServerBroadcast(t *testing.T) {
	tests := []struct {
		name  string
		admin hostcap.SessionAdmin // nil exercises the fail-closed guard
		check func(t *testing.T, admin *fakeSessionAdmin, err error)
	}{
		{
			name:  "delegates message to BroadcastSystemMessage",
			admin: &fakeSessionAdmin{},
			check: func(t *testing.T, admin *fakeSessionAdmin, err error) {
				require.NoError(t, err)
				assert.Equal(t, "Server restart in 5 minutes.", admin.lastBroadcastMsg)
			},
		},
		{
			name:  "returns opaque internal error on admin failure",
			admin: &fakeSessionAdmin{broadcastErr: errors.New("secret internal broadcast failure")},
			check: func(t *testing.T, _ *fakeSessionAdmin, err error) {
				requireOpaqueInternal(t, err)
			},
		},
		{
			name:  "fails closed with Unimplemented when SessionAdmin is nil",
			admin: nil,
			check: func(t *testing.T, _ *fakeSessionAdmin, err error) {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, codes.Unimplemented, st.Code(), "nil SessionAdmin must fail closed with Unimplemented, not NPE")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// A nil admin maps to the bare stub whose SessionAdmin() returns nil
			// (the unwired case both runtimes hit); a non-nil admin is wired through.
			var srv hostv1.SessionAdminServiceServer
			fake, _ := tc.admin.(*fakeSessionAdmin)
			if tc.admin == nil {
				srv = hostcap.NewSessionAdminServer(hostcap.NewBase(stubHostCaps{}, "core-communication"))
			} else {
				srv = hostcap.NewSessionAdminServer(hostcap.NewBase(newSessionCaps(&fakeSessionAccess{}, fake), "core-communication"))
			}
			_, err := srv.Broadcast(context.Background(), &hostv1.BroadcastRequest{Message: "Server restart in 5 minutes."})
			tc.check(t, fake, err)
		})
	}
}

func TestSessionAdminServerDisconnect(t *testing.T) {
	tests := []struct {
		name  string
		admin hostcap.SessionAdmin // nil exercises the fail-closed guard
		check func(t *testing.T, admin *fakeSessionAdmin, err error)
	}{
		{
			name:  "delegates session_id and reason to DisconnectSession",
			admin: &fakeSessionAdmin{},
			check: func(t *testing.T, admin *fakeSessionAdmin, err error) {
				require.NoError(t, err)
				assert.Equal(t, sessionULID, admin.lastDisconnectID)
				assert.Equal(t, "idle timeout", admin.lastDisconnectRsn)
			},
		},
		{
			name:  "returns opaque internal error on admin failure",
			admin: &fakeSessionAdmin{disconnectErr: errors.New("secret disconnect failure")},
			check: func(t *testing.T, _ *fakeSessionAdmin, err error) {
				requireOpaqueInternal(t, err)
			},
		},
		{
			name:  "fails closed with Unimplemented when SessionAdmin is nil",
			admin: nil,
			check: func(t *testing.T, _ *fakeSessionAdmin, err error) {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, codes.Unimplemented, st.Code(), "nil SessionAdmin must fail closed with Unimplemented, not NPE")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var srv hostv1.SessionAdminServiceServer
			fake, _ := tc.admin.(*fakeSessionAdmin)
			if tc.admin == nil {
				srv = hostcap.NewSessionAdminServer(hostcap.NewBase(stubHostCaps{}, "core-communication"))
			} else {
				srv = hostcap.NewSessionAdminServer(hostcap.NewBase(newSessionCaps(&fakeSessionAccess{}, fake), "core-communication"))
			}
			_, err := srv.Disconnect(context.Background(), &hostv1.DisconnectRequest{
				SessionId: sessionULID,
				Reason:    "idle timeout",
			})
			tc.check(t, fake, err)
		})
	}
}
