// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package auth_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	authpg "github.com/holomush/holomush/internal/auth/postgres"
	bootstrapsetup "github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/charname/blocklist"
	"github.com/holomush/holomush/internal/naming"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/telnet"
	"github.com/holomush/holomush/internal/world"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/pkg/errutil"
)

// These specs prove the PRODUCTION create path is gated.
//
// That distinction is the whole point of plan 02-06's Theme-A blocker: before
// this plan, charname.Gate compiled, passed every unit test, and was constructed
// at NO composition root — so a name submitted through CharacterService.Create
// reached no confusable check and no block list. A spec that built its own gate
// and called Check would have been green throughout. These drive
// CharacterService and GuestService instead.

// gateTestEnv is the production create path, assembled the way the composition
// roots assemble it.
type gateTestEnv struct {
	pool       *pgxpool.Pool
	characters *auth.CharacterService
	guests     *auth.GuestService
	startLoc   ulid.ULID
}

// newGateTestEnv builds a CharacterService and a GuestService over one pool,
// sharing ONE charname.Gate — the same shape bootstrapsetup and cmd/holomush
// build.
func newGateTestEnv(t *testing.T, blockPatterns []string) *gateTestEnv {
	t.Helper()
	ctx := context.Background()
	pool := reaperPool(t)

	// A verifiable corpus. 000001_baseline seeds a bootstrap character with no
	// name_skeleton, and the gate correctly refuses to adjudicate against a
	// corpus it cannot verify (D-30) — so without this every spec below would
	// fail NAME_SKELETON_UNVERIFIABLE and prove nothing about the gate.
	admitReapName(ctx, t, pool, "Corpus Warmup")

	charRepo := worldpg.NewCharacterRepository(pool)
	locRepo := worldpg.NewLocationRepository(pool)

	startLoc := ulid.Make()
	_, err := pool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Gate Test Start', '', 'persistent', 'last:0', (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
	`, startLoc.String())
	require.NoError(t, err)

	// The block list arrives as a compiled Snapshot rather than the production
	// Cache: both satisfy charname.BlockList, and the reload behaviour the Cache
	// exists for is 02-05's to assert, not this spec's.
	snapshot, err := blocklist.Compile(blockPatterns)
	require.NoError(t, err)
	gate := &charname.Gate{
		Skeletons: worldpg.NewSkeletonLookup(pool),
		BlockList: snapshot,
	}

	transactor := worldpg.NewTransactor(pool)
	bindingRepo := worldpg.NewBindingRepository(pool)
	genesis, err := auth.NewCharacterGenesisService(
		charRepo, transactor, bindingRepo,
		worldpg.NewOutboxStore(pool), worldpg.NewReapingGuard(pool),
	)
	require.NoError(t, err)

	characters, err := auth.NewCharacterService(
		bootstrapsetup.NewCharRepoAdapter(pool, charRepo),
		bootstrapsetup.NewLocRepoAdapter(&startLoc, locRepo),
		genesis,
		gate,
	)
	require.NoError(t, err)

	playerRepo := authpg.NewPlayerRepository(pool)
	reaping, err := auth.NewCharacterReapingService(
		charRepo, charRepo,
		worldpg.NewPropertyRepository(pool),
		bindingRepo, transactor,
		worldpg.NewOutboxStore(pool),
		playerRepo, playerRepo,
	)
	require.NoError(t, err)

	guests, err := auth.NewGuestService(
		telnet.NewGuestAuthenticator(naming.NewGemstoneElementTheme(), startLoc),
		playerRepo,
		&gateGuestCharRepo{pool: pool},
		store.NewPostgresPlayerSessionStore(pool),
		genesis,
		reaping,
		gate,
	)
	require.NoError(t, err)

	return &gateTestEnv{pool: pool, characters: characters, guests: guests, startLoc: startLoc}
}

// gateGuestCharRepo is the auth.GuestCharacterRepository the guest path reads
// through. It carries the SAME transitional predicate every other site carries.
//
// REMOVE the NULL branch with migration 000056; see plan 02-12.
type gateGuestCharRepo struct{ pool *pgxpool.Pool }

func (r *gateGuestCharRepo) ExistsByNormalizedName(ctx context.Context, key string, excluding *ulid.ULID) (bool, error) {
	var excludingArg *string
	if excluding != nil {
		s := excluding.String()
		excludingArg = &s
	}
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM characters
		WHERE (normalized_name = $1
		       OR (normalized_name IS NULL AND LOWER(name) = LOWER($1)))
		  AND ($2::text IS NULL OR id::text <> $2)
	)`, key, excludingArg).Scan(&exists)
	return exists, err
}

func (e *gateTestEnv) seedPlayer(t *testing.T) ulid.ULID {
	t.Helper()
	playerID := ulid.Make()
	_, err := e.pool.Exec(context.Background(),
		`INSERT INTO players (id, username, password_hash) VALUES ($1, $2, '')`,
		playerID.String(), "gate_"+playerID.String())
	require.NoError(t, err)
	return playerID
}

// TestProductionCreatePathRejectsAConfusableName drives CharacterService.Create
// — the path a registered player's character creation actually takes.
func TestProductionCreatePathRejectsAConfusableName(t *testing.T) {
	ctx := context.Background()
	env := newGateTestEnv(t, nil)
	playerID := env.seedPlayer(t)

	// Paired positive control FIRST, on the same fixture: an ordinary name
	// succeeds, so the refusal below cannot pass because creation is broken.
	seeded, err := env.characters.Create(ctx, playerID, "cocoa")
	require.NoError(t, err)
	require.NotNil(t, seeded)
	assert.Equal(t, "cocoa", seeded.Name, "§6.1.5: the player's own capitalization is preserved")

	// A whole-script Cyrillic homoglyph of the seeded name.
	_, err = env.characters.Create(ctx, env.seedPlayer(t), "сосоа")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "NAME_CONFUSABLE")
}

// TestProductionCreatePathRejectsABlockListedName proves plan 02-05's matcher
// actually reaches the gate the create path uses.
func TestProductionCreatePathRejectsABlockListedName(t *testing.T) {
	ctx := context.Background()
	env := newGateTestEnv(t, []string{`^admin$`})
	playerID := env.seedPlayer(t)

	_, err := env.characters.Create(ctx, playerID, "Admin")
	require.Error(t, err, "the block list matches the case-folded KEY, so 'Admin' must be refused")
	errutil.AssertErrorCode(t, err, "NAME_BLOCKED")

	// Paired success on the same fixture.
	ok, err := env.characters.Create(ctx, playerID, "administrative assistant")
	require.NoError(t, err, "a name the pattern does not match must still be creatable")
	require.NotNil(t, ok)
}

// TestProductionCreatePathMapsASyntacticRefusalToTheExistingCode proves the
// gate's NAME_INVALID_SYNTAX is mapped, not leaked: the caller-visible contract
// for a digit-bearing name is unchanged.
func TestProductionCreatePathMapsASyntacticRefusalToTheExistingCode(t *testing.T) {
	ctx := context.Background()
	env := newGateTestEnv(t, nil)
	playerID := env.seedPlayer(t)

	_, err := env.characters.Create(ctx, playerID, "Alaric2")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHARACTER_INVALID_NAME")
}

// TestGuestProvisioningRetriesRatherThanPersistingABlockListedName is the guest
// twin. The guest path never ran the name pipeline at all before this plan
// (T-02-31), so a gate installed only in CharacterService would have left the
// automatic, high-volume path wide open.
func TestGuestProvisioningRetriesRatherThanPersistingABlockListedName(t *testing.T) {
	ctx := context.Background()

	// The gemstone/element namer produces two-word names; blocking every name
	// beginning with a vowel forces the retry loop to run for real without
	// making it impossible to succeed.
	env := newGateTestEnv(t, []string{`^[aeiou]`})

	result, err := env.guests.CreateGuest(ctx)
	require.NoError(t, err, "the retry loop must find an admissible name")
	require.NotNil(t, result)
	require.NotNil(t, result.Character)

	// Whatever name landed, it does NOT match the configured pattern — the
	// assertion the whole spec exists for.
	normalized, nErr := charname.Normalize(result.Character.Name)
	require.NoError(t, nErr)
	assert.NotContains(t, "aeiou", string(normalized.Key[0]),
		"a generated name matching a configured block pattern must be retried, never persisted")

	// And the stored row carries its identity columns, because it went through
	// the gated writer.
	var key, skeleton, version string
	require.NoError(t, env.pool.QueryRow(ctx, `
		SELECT normalized_name, name_skeleton, name_skeleton_unicode_version
		FROM characters WHERE id = $1
	`, result.Character.ID.String()).Scan(&key, &skeleton, &version))
	assert.Equal(t, normalized.Key, key)
	assert.NotEmpty(t, skeleton)
	assert.Equal(t, charname.UnicodeVersion, version)
}

// TestGateConstructionRefusesANilBlockListSubsystem pairs the refusal with a
// populated config that succeeds, on the same fixture.
func TestGateConstructionRefusesANilBlockListSubsystem(t *testing.T) {
	pool := reaperPool(t)

	gate, err := bootstrapsetup.NewCharacterNameGate(pool, nil)
	require.Error(t, err, "a production root must not build a gate with no block list")
	assert.Nil(t, gate)
	errutil.AssertErrorCode(t, err, "CHARACTER_NAME_GATE_MISCONFIGURED")

	gate, err = bootstrapsetup.NewCharacterNameGate(pool, blocklist.NewSubsystem(blocklist.SubsystemConfig{
		Source: func() blocklist.Source { return nil },
	}))
	require.NoError(t, err)
	require.NotNil(t, gate)
	assert.NotNil(t, gate.BlockList, "the gate must carry the subsystem's live matcher")
	assert.NotNil(t, gate.Skeletons)
}

// compile-time assertion: the spec's guest repo really is what the service takes.
var _ auth.GuestCharacterRepository = (*gateGuestCharRepo)(nil)

// unused keeps world imported for the compile-time assertion above when the
// spec set changes shape.
var _ = world.Character{}
