// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package approval_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/admin/approval"
	adminauth "github.com/holomush/holomush/internal/admin/auth"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// --- fakes (handler-test only) ---

type fakeSessions struct {
	identity adminauth.OperatorIdentity
	err      error
}

func (f *fakeSessions) Issue(_ adminauth.OperatorIdentity) (string, time.Time, error) {
	return "", time.Time{}, errors.New("not used in handler tests")
}

func (f *fakeSessions) Get(_ string) (adminauth.OperatorIdentity, error) { return f.identity, f.err }
func (f *fakeSessions) Revoke(_ string) error                            { return nil }

// fakeApprovalRepo is the narrow fake for Repo.MarkApproved.
type fakeApprovalRepo struct {
	markErr   error
	markCalls int
}

func (r *fakeApprovalRepo) Open(_ context.Context, _ approval.OpenRequest) (approval.RequestID, error) {
	return approval.RequestID{}, errors.New("not used")
}

func (r *fakeApprovalRepo) Get(_ context.Context, _ approval.RequestID) (approval.Approval, error) {
	return approval.Approval{}, errors.New("not used")
}

func (r *fakeApprovalRepo) MarkApproved(_ context.Context, _ approval.RequestID, _ string) error {
	r.markCalls++
	return r.markErr
}

func (r *fakeApprovalRepo) WaitForApproval(_ context.Context, _ approval.RequestID, _ time.Time) (approval.Approval, error) {
	return approval.Approval{}, errors.New("not used")
}

// fakeResolver implements access.SubjectResolver. The grant set is stored as
// []string and published under access.PlayerGrantsAttribute matching the
// pattern from internal/access/grants_test.go and ingame_test.go.
type fakeResolver struct {
	grants []string
	err    error
}

func (r *fakeResolver) ResolveSubjectAttributes(_ context.Context, _ string, _ string) (*types.AttributeBags, error) {
	if r.err != nil {
		return nil, r.err
	}
	bags := &types.AttributeBags{
		Subject: map[string]any{},
	}
	if r.grants != nil {
		bags.Subject[access.PlayerGrantsAttribute] = r.grants
	}
	return bags, nil
}

// Compile-time assertion.
var _ access.SubjectResolver = (*fakeResolver)(nil)

// fakeRoleHasher implements approval.RoleHasher.
type fakeRoleHasher struct {
	has bool
	err error
}

func (r *fakeRoleHasher) PlayerHasRole(_ context.Context, _, _ string) (bool, error) {
	return r.has, r.err
}

// --- helpers ---

func mkRequestIDBytes(t *testing.T) []byte {
	t.Helper()
	id := ulid.Make()
	return id.Bytes()
}

func newApproveHandler(sessions *fakeSessions, repo *fakeApprovalRepo, resolver *fakeResolver, roles *fakeRoleHasher) *approval.ApproveHandler {
	return approval.NewApproveHandler(sessions, repo, resolver, roles)
}

// --- tests ---

func TestApproveHandlerRequiresValidSession(t *testing.T) {
	sessions := &fakeSessions{err: oops.Code("DENY_SESSION_INVALID").Errorf("nope")}
	repo := &fakeApprovalRepo{}
	resolver := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoleHasher{has: true}
	h := newApproveHandler(sessions, repo, resolver, roles)

	rid := mkRequestIDBytes(t)
	req := connect.NewRequest(&adminv1.ApproveRequest{SessionToken: "bad", RequestId: rid})
	_, err := h.Approve(context.Background(), req)
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodeUnauthenticated, ce.Code())
	assert.Equal(t, 0, repo.markCalls)
}

func TestApproveHandlerRejectsExpiredSession(t *testing.T) {
	sessions := &fakeSessions{err: oops.Code("DENY_SESSION_EXPIRED").Errorf("expired")}
	repo := &fakeApprovalRepo{}
	resolver := &fakeResolver{}
	roles := &fakeRoleHasher{has: true}
	h := newApproveHandler(sessions, repo, resolver, roles)

	rid := mkRequestIDBytes(t)
	req := connect.NewRequest(&adminv1.ApproveRequest{SessionToken: "tk", RequestId: rid})
	_, err := h.Approve(context.Background(), req)
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodeUnauthenticated, ce.Code())
}

func TestApproveHandlerRequiresCapability(t *testing.T) {
	sessions := &fakeSessions{identity: adminauth.OperatorIdentity{PlayerID: "01HZA00000000000000000000"}}
	repo := &fakeApprovalRepo{}
	resolver := &fakeResolver{grants: []string{}} // no cap
	roles := &fakeRoleHasher{has: true}
	h := newApproveHandler(sessions, repo, resolver, roles)

	rid := mkRequestIDBytes(t)
	req := connect.NewRequest(&adminv1.ApproveRequest{SessionToken: "tk", RequestId: rid})
	_, err := h.Approve(context.Background(), req)
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodePermissionDenied, ce.Code())
	assert.Equal(t, 0, repo.markCalls)
}

func TestApproveHandlerRequiresAdminRole(t *testing.T) {
	sessions := &fakeSessions{identity: adminauth.OperatorIdentity{PlayerID: "01HZA00000000000000000000"}}
	repo := &fakeApprovalRepo{}
	resolver := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoleHasher{has: false} // no admin
	h := newApproveHandler(sessions, repo, resolver, roles)

	rid := mkRequestIDBytes(t)
	req := connect.NewRequest(&adminv1.ApproveRequest{SessionToken: "tk", RequestId: rid})
	_, err := h.Approve(context.Background(), req)
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodePermissionDenied, ce.Code())
	assert.Equal(t, 0, repo.markCalls)
}

func TestApproveHandlerCallsRepoMarkApproved(t *testing.T) {
	sessions := &fakeSessions{identity: adminauth.OperatorIdentity{PlayerID: "01HZA00000000000000000000"}}
	repo := &fakeApprovalRepo{}
	resolver := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoleHasher{has: true}
	h := newApproveHandler(sessions, repo, resolver, roles)

	rid := mkRequestIDBytes(t)
	req := connect.NewRequest(&adminv1.ApproveRequest{SessionToken: "tk", RequestId: rid})
	resp, err := h.Approve(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 1, repo.markCalls)
}

func TestApproveHandlerSurfacesSelfApprovalDenial(t *testing.T) {
	sessions := &fakeSessions{identity: adminauth.OperatorIdentity{PlayerID: "01HZA00000000000000000000"}}
	repo := &fakeApprovalRepo{markErr: oops.Code("DENY_DUAL_CONTROL_SELF").Errorf("self")}
	resolver := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoleHasher{has: true}
	h := newApproveHandler(sessions, repo, resolver, roles)

	rid := mkRequestIDBytes(t)
	req := connect.NewRequest(&adminv1.ApproveRequest{SessionToken: "tk", RequestId: rid})
	_, err := h.Approve(context.Background(), req)
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodeFailedPrecondition, ce.Code())
}

func TestApproveHandlerSurfacesAlreadyApprovedDenial(t *testing.T) {
	sessions := &fakeSessions{identity: adminauth.OperatorIdentity{PlayerID: "01HZA00000000000000000000"}}
	repo := &fakeApprovalRepo{markErr: oops.Code("DENY_APPROVAL_ALREADY_APPROVED").Errorf("already")}
	resolver := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoleHasher{has: true}
	h := newApproveHandler(sessions, repo, resolver, roles)

	rid := mkRequestIDBytes(t)
	req := connect.NewRequest(&adminv1.ApproveRequest{SessionToken: "tk", RequestId: rid})
	_, err := h.Approve(context.Background(), req)
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodeFailedPrecondition, ce.Code())
}

func TestApproveHandlerSurfacesExpiredDenial(t *testing.T) {
	sessions := &fakeSessions{identity: adminauth.OperatorIdentity{PlayerID: "01HZA00000000000000000000"}}
	repo := &fakeApprovalRepo{markErr: oops.Code("DENY_APPROVAL_EXPIRED").Errorf("expired")}
	resolver := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoleHasher{has: true}
	h := newApproveHandler(sessions, repo, resolver, roles)

	rid := mkRequestIDBytes(t)
	req := connect.NewRequest(&adminv1.ApproveRequest{SessionToken: "tk", RequestId: rid})
	_, err := h.Approve(context.Background(), req)
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodeFailedPrecondition, ce.Code())
}

func TestApproveHandlerRejectsInvalidRequestID(t *testing.T) {
	cases := []struct {
		name string
		rid  []byte
	}{
		{"empty", []byte{}},
		{"too short", []byte{0x01, 0x02}},
		{"too long", make([]byte, 17)},
		{"all zero (16 bytes)", make([]byte, 16)},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			sessions := &fakeSessions{identity: adminauth.OperatorIdentity{PlayerID: "01HZA00000000000000000000"}}
			repo := &fakeApprovalRepo{}
			resolver := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
			roles := &fakeRoleHasher{has: true}
			h := newApproveHandler(sessions, repo, resolver, roles)

			req := connect.NewRequest(&adminv1.ApproveRequest{SessionToken: "tk", RequestId: tc.rid})
			_, err := h.Approve(context.Background(), req)
			require.Error(t, err)
			var ce *connect.Error
			require.True(t, errors.As(err, &ce))
			assert.Equal(t, connect.CodeInvalidArgument, ce.Code())
			assert.Equal(t, 0, repo.markCalls)
		})
	}
}
