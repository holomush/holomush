// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package adminauth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	adminauth "github.com/holomush/holomush/internal/admin/auth"
)

// TestAssertOperatorAdminHappyPath verifies both checks (capability +
// admin role) succeed when both gates are present.
func TestAssertOperatorAdminHappyPath(t *testing.T) {
	pid := ulid.Make().String()
	res := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoles{playerRoles: map[string][]string{pid: {access.RoleAdmin}}}

	require.NoError(t, adminauth.AssertOperatorAdmin(context.Background(), res, roles, pid))
}

// TestAssertOperatorAdminDeniesNotOperator covers DENY_NOT_OPERATOR — the
// player lacks the crypto.operator capability. Role lookup MUST NOT execute.
func TestAssertOperatorAdminDeniesNotOperator(t *testing.T) {
	pid := ulid.Make().String()
	res := &fakeResolver{grants: []string{}}
	roles := &fakeRoles{}

	err := adminauth.AssertOperatorAdmin(context.Background(), res, roles, pid)
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "DENY_NOT_OPERATOR", o.Code())
	assert.Equal(t, 0, roles.calls, "role lookup must not execute when capability missing")
}

// TestAssertOperatorAdminDeniesNotAdminRole covers DENY_NOT_ADMIN_ROLE — the
// capability is present but no admin role.
func TestAssertOperatorAdminDeniesNotAdminRole(t *testing.T) {
	pid := ulid.Make().String()
	res := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoles{playerRoles: map[string][]string{}}

	err := adminauth.AssertOperatorAdmin(context.Background(), res, roles, pid)
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "DENY_NOT_ADMIN_ROLE", o.Code())
}

// TestAssertOperatorAdminWrapsResolverError covers
// INGAME_GRANT_LOOKUP_FAILED — the resolver returned an infrastructure
// error (e.g. DB outage). Role lookup MUST NOT execute.
func TestAssertOperatorAdminWrapsResolverError(t *testing.T) {
	pid := ulid.Make().String()
	infra := errors.New("attribute store unavailable")
	res := &fakeResolver{err: infra}
	roles := &fakeRoles{}

	err := adminauth.AssertOperatorAdmin(context.Background(), res, roles, pid)
	require.Error(t, err)
	assert.ErrorIs(t, err, infra, "resolver error must be wrapped, not swallowed")
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INGAME_GRANT_LOOKUP_FAILED", o.Code())
	assert.Equal(t, 0, roles.calls, "role lookup must not execute on resolver error")
}

// TestAssertOperatorAdminWrapsRoleStoreError covers
// INGAME_ROLE_LOOKUP_FAILED — capability check passed but the role store
// returned an infrastructure error.
func TestAssertOperatorAdminWrapsRoleStoreError(t *testing.T) {
	pid := ulid.Make().String()
	infra := errors.New("role store unavailable")
	res := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoles{err: infra}

	err := adminauth.AssertOperatorAdmin(context.Background(), res, roles, pid)
	require.Error(t, err)
	assert.ErrorIs(t, err, infra)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INGAME_ROLE_LOOKUP_FAILED", o.Code())
}
