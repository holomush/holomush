// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/world"
)

// --- fakes ---

type fakePlayerRepo struct {
	count   int
	created *auth.Player
}

func (f *fakePlayerRepo) Count(_ context.Context) (int, error)           { return f.count, nil }
func (f *fakePlayerRepo) Create(_ context.Context, p *auth.Player) error { f.created = p; return nil }

func (f *fakePlayerRepo) GetByID(_ context.Context, _ ulid.ULID) (*auth.Player, error) {
	return nil, nil
}

func (f *fakePlayerRepo) GetByUsername(_ context.Context, _ string) (*auth.Player, error) {
	return nil, nil
}

func (f *fakePlayerRepo) GetByEmail(_ context.Context, _ string) (*auth.Player, error) {
	return nil, nil
}

func (f *fakePlayerRepo) Update(_ context.Context, _ *auth.Player) error { return nil }

func (f *fakePlayerRepo) UpdatePassword(_ context.Context, _ ulid.ULID, _ string) error { return nil }

func (f *fakePlayerRepo) UpdatePasswordAndClearLockout(_ context.Context, _ ulid.ULID, _ string) error {
	return nil
}
func (f *fakePlayerRepo) Delete(_ context.Context, _ ulid.ULID) error { return nil }
func (f *fakePlayerRepo) ListIdleGuests(_ context.Context, _ time.Time) ([]*auth.Player, error) {
	return nil, nil
}
func (f *fakePlayerRepo) DeleteGuestPlayer(_ context.Context, _ ulid.ULID) error { return nil }

type fakeCharService struct {
	createdName string
	playerID    ulid.ULID
}

func (f *fakeCharService) Create(_ context.Context, playerID ulid.ULID, name string) (*world.Character, error) {
	f.playerID = playerID
	f.createdName = name
	return &world.Character{
		ID:       ulid.Make(),
		PlayerID: playerID,
		Name:     name,
	}, nil
}

type fakeRoleStore struct {
	addedRoles  []struct{ charID, role string }
	playerRoles map[string][]string
}

func (f *fakeRoleStore) GetRoles(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (f *fakeRoleStore) AddRole(_ context.Context, charID, role string) error {
	f.addedRoles = append(f.addedRoles, struct{ charID, role string }{charID, role})
	return nil
}
func (f *fakeRoleStore) RemoveRole(_ context.Context, _, _ string) error { return nil }
func (f *fakeRoleStore) PlayerHasRole(_ context.Context, playerID, role string) (bool, error) {
	for _, r := range f.playerRoles[playerID] {
		if r == role {
			return true, nil
		}
	}
	return false, nil
}

type fakeHasher struct{}

func (f *fakeHasher) Hash(password string) (string, error) { return "hashed:" + password, nil }
func (f *fakeHasher) Verify(password, hash string) (bool, error) {
	return hash == "hashed:"+password, nil
}
func (f *fakeHasher) NeedsUpgrade(_ string) bool { return false }

type fakeNameTheme struct{}

func (f *fakeNameTheme) Name() string               { return "fake" }
func (f *fakeNameTheme) Generate() (string, string) { return "Stardust", "Glimmer" }

// --- helpers ---

func makeDeps() (SeedAdminDeps, *fakePlayerRepo, *fakeCharService, *fakeRoleStore) {
	pr := &fakePlayerRepo{}
	cs := &fakeCharService{}
	rs := &fakeRoleStore{}
	deps := SeedAdminDeps{
		PlayerRepo:  pr,
		CharService: cs,
		RoleStore:   rs,
		Hasher:      &fakeHasher{},
		NameTheme:   &fakeNameTheme{},
	}
	return deps, pr, cs, rs
}

// --- tests ---

func TestSeedAdmin_CreatesOnEmptyDB(t *testing.T) {
	t.Setenv("HOLOMUSH_ADMIN_USERNAME", "")
	t.Setenv("HOLOMUSH_ADMIN_PASSWORD", "")
	t.Setenv("HOLOMUSH_ADMIN_CHARACTER", "")

	deps, pr, cs, rs := makeDeps()

	err := SeedAdmin(context.Background(), deps)
	require.NoError(t, err)

	// Player created
	require.NotNil(t, pr.created)
	assert.Equal(t, "admin", pr.created.Username)

	// Character created for the same player
	assert.Equal(t, pr.created.ID, cs.playerID)
	assert.Equal(t, "Stardust", cs.createdName) // from fakeNameTheme

	// Admin role assigned
	require.Len(t, rs.addedRoles, 1)
	assert.Equal(t, "admin", rs.addedRoles[0].role)
}

func TestSeedAdmin_SkipsWhenPlayersExist(t *testing.T) {
	t.Setenv("HOLOMUSH_ADMIN_USERNAME", "")
	t.Setenv("HOLOMUSH_ADMIN_PASSWORD", "")
	t.Setenv("HOLOMUSH_ADMIN_CHARACTER", "")

	deps, pr, cs, rs := makeDeps()
	pr.count = 5

	err := SeedAdmin(context.Background(), deps)
	require.NoError(t, err)

	assert.Nil(t, pr.created)
	assert.Empty(t, cs.createdName)
	assert.Empty(t, rs.addedRoles)
}

func TestSeedAdmin_UsesEnvVarOverrides(t *testing.T) {
	t.Setenv("HOLOMUSH_ADMIN_USERNAME", "superadmin")
	t.Setenv("HOLOMUSH_ADMIN_PASSWORD", "s3cret!")
	t.Setenv("HOLOMUSH_ADMIN_CHARACTER", "Gandalf")

	deps, pr, cs, _ := makeDeps()

	err := SeedAdmin(context.Background(), deps)
	require.NoError(t, err)

	require.NotNil(t, pr.created)
	assert.Equal(t, "superadmin", pr.created.Username)
	assert.Equal(t, "hashed:s3cret!", pr.created.PasswordHash)
	assert.Equal(t, "Gandalf", cs.createdName)
}

// orderingCharService and orderingRoleStore record into a shared sequence so a
// test can assert the admin role is assigned AFTER the character is created
// (round-4 B4 ordering: role_store's own-pool insert must not run while the
// character row is uncommitted in the genesis transaction).
type orderingCharService struct {
	seq      *[]string
	playerID ulid.ULID
	charID   ulid.ULID
}

func (o *orderingCharService) Create(_ context.Context, playerID ulid.ULID, name string) (*world.Character, error) {
	*o.seq = append(*o.seq, "character")
	o.playerID = playerID
	o.charID = ulid.Make()
	return &world.Character{ID: o.charID, PlayerID: playerID, Name: name}, nil
}

type orderingRoleStore struct {
	seq        *[]string
	roleCharID string
}

func (o *orderingRoleStore) GetRoles(_ context.Context, _ string) ([]string, error) { return nil, nil }

func (o *orderingRoleStore) AddRole(_ context.Context, charID, _ string) error {
	*o.seq = append(*o.seq, "role")
	o.roleCharID = charID
	return nil
}
func (o *orderingRoleStore) RemoveRole(_ context.Context, _, _ string) error { return nil }
func (o *orderingRoleStore) PlayerHasRole(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

func TestSeedAdminAssignsRoleAfterCharacterCreate(t *testing.T) {
	t.Setenv("HOLOMUSH_ADMIN_USERNAME", "")
	t.Setenv("HOLOMUSH_ADMIN_PASSWORD", "")
	t.Setenv("HOLOMUSH_ADMIN_CHARACTER", "")

	seq := []string{}
	cs := &orderingCharService{seq: &seq}
	rs := &orderingRoleStore{seq: &seq}
	deps := SeedAdminDeps{
		PlayerRepo:  &fakePlayerRepo{},
		CharService: cs,
		RoleStore:   rs,
		Hasher:      &fakeHasher{},
		NameTheme:   &fakeNameTheme{},
	}

	require.NoError(t, SeedAdmin(context.Background(), deps))

	// character created BEFORE the role is assigned (post-commit ordering).
	require.Equal(t, []string{"character", "role"}, seq)
	// the role is assigned to the character that was just created.
	assert.Equal(t, cs.charID.String(), rs.roleCharID)
}

func TestSeedAdmin_GeneratesPasswordWhenNotSet(t *testing.T) {
	t.Setenv("HOLOMUSH_ADMIN_USERNAME", "")
	t.Setenv("HOLOMUSH_ADMIN_PASSWORD", "")
	t.Setenv("HOLOMUSH_ADMIN_CHARACTER", "")

	deps, pr, _, _ := makeDeps()

	err := SeedAdmin(context.Background(), deps)
	require.NoError(t, err)

	require.NotNil(t, pr.created)

	// Password hash has the form "hashed:<base64>".
	hash := pr.created.PasswordHash
	assert.True(t, len(hash) > len("hashed:"))
	password := hash[len("hashed:"):]

	// 18 random bytes -> 24 base64 chars.
	decoded, err := base64.URLEncoding.DecodeString(password)
	require.NoError(t, err)
	assert.Len(t, decoded, 18)
	assert.Len(t, password, 24)
}
