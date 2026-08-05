// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestRoleStoreInterfaceMethodSetIsUnchangedByPlayerRoles pins the RoleStore
// INTERFACE's method set against the addition of PlayerRoles.
//
// PlayerRoles deliberately lands on the CONCRETE *PostgresRoleStore, never on
// the interface. The interface is consumed by setup.ABACConfig.RoleStore and is
// faked/mocked in several places; broadening it would force every implementor
// to grow a method only one of them can answer, for no benefit. The player-
// scoped lookup reaches the ABAC stack through a func field on ABACConfig
// (PlayerRoleLookup), which is the shipped seam shape — PlayerKindLookup is
// already exactly this.
//
// This test exists so a later "tidy-up" that moves PlayerRoles onto the
// interface has to do it deliberately, with this assertion in front of it.
func TestRoleStoreInterfaceMethodSetIsUnchangedByPlayerRoles(t *testing.T) {
	iface := reflect.TypeOf((*store.RoleStore)(nil)).Elem()

	got := make([]string, 0, iface.NumMethod())
	for i := range iface.NumMethod() {
		got = append(got, iface.Method(i).Name)
	}
	sort.Strings(got)

	assert.Equal(t,
		[]string{"AddRole", "GetRoles", "PlayerHasRole", "RemoveRole"},
		got,
		"store.RoleStore's method set MUST stay exactly these four. PlayerRoles "+
			"belongs on the concrete *PostgresRoleStore and reaches the ABAC stack "+
			"via the ABACConfig.PlayerRoleLookup func field.")
}

// TestPostgresRoleStorePlayerRolesRejectsAMalformedPlayerID asserts that a
// non-ULID player id is an ERROR, not an empty result.
//
// Silently returning an empty slice would let a caller read "bad input" as "no
// roles" — and every role-gated policy would then default-deny for a reason the
// caller cannot distinguish from a legitimately role-less player. The
// validation runs before the pool is touched, which is why a nil pool is safe
// here.
func TestPostgresRoleStorePlayerRolesRejectsAMalformedPlayerID(t *testing.T) {
	rs := store.NewPostgresRoleStore(nil)

	roles, err := rs.PlayerRoles(context.Background(), "not-a-ulid")

	require.Error(t, err, "a malformed player id MUST error rather than resolve to no roles")
	errutil.AssertErrorCode(t, err, "INVALID_PLAYER_ID")
	assert.Nil(t, roles, "no partial result may accompany the error")
}
