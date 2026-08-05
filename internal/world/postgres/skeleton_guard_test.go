// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/internal/world/wmodel"
	"github.com/holomush/holomush/pkg/errutil"
)

// These specs prove D-30 part 2 against REAL Postgres: the confusable guarantee
// is enforced by serialization, not left advisory.
//
// The case they exist for is the one no unique index can catch. Two names whose
// SKELETONS are equal but whose NORMALIZED NAMES differ — a whole-script
// homoglyph pair — pass charname.Gate's pre-check concurrently and both insert,
// and migration 000056's normalized_name unique index structurally cannot
// separate them, because differing normalized names is precisely what makes a
// pair confusable.

// confusablePair returns two character names that share one skeleton and have
// DIFFERENT normalized names: a Latin form and its whole-script Cyrillic
// homoglyph, both carrying the same random Latin prefix so the pair is unique
// against whatever else the shared database holds.
func confusablePair(t *testing.T) (latin, cyrillic string) {
	t.Helper()
	prefix := randomFixtureLetters(8)
	latin = prefix + " cocoa"
	cyrillic = prefix + " сосоа" // Cyrillic с о с о а

	require.Equal(t,
		charname.Skeleton(mustKeyOf(t, latin)),
		charname.Skeleton(mustKeyOf(t, cyrillic)),
		"the fixture pair must actually share a skeleton, or this whole file proves nothing")
	require.NotEqual(t, mustKeyOf(t, latin), mustKeyOf(t, cyrillic),
		"the fixture pair must have DIFFERENT normalized names — that is what makes "+
			"000056's unique index structurally unable to catch it")
	return latin, cyrillic
}

func mustKeyOf(t *testing.T, name string) string {
	t.Helper()
	n, err := charname.Normalize(name)
	require.NoError(t, err)
	return n.Key
}

// newGuardChar builds an unsaved character for the guard specs.
func newGuardChar(ctx context.Context, t *testing.T, name string) *world.Character {
	t.Helper()
	playerID := createTestPlayer(ctx, t)
	locationID := createTestLocation(ctx, t)
	return &world.Character{
		ID:          ulid.Make(),
		PlayerID:    playerID,
		Name:        name,
		Description: "guard fixture",
		LocationID:  &locationID,
		CreatedAt:   time.Now().UTC(),
	}
}

// TestConcurrentCreatesSharingOneSkeletonProduceExactlyOneSuccess is the D-30
// part 2 proof. It runs repeatedly so a single passing run cannot be luck.
func TestConcurrentCreatesSharingOneSkeletonProduceExactlyOneSuccess(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	const iterations = 20
	for i := 0; i < iterations; i++ {
		latinName, cyrillicName := confusablePair(t)

		latin := newGuardChar(ctx, t, latinName)
		cyrillic := newGuardChar(ctx, t, cyrillicName)
		latinToken := admit(ctx, t, latinName)
		cyrillicToken := admit(ctx, t, cyrillicName)

		results := make([]error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			results[0] = delErr(repo.Create(ctx, latin, latinToken))
		}()
		go func() {
			defer wg.Done()
			results[1] = delErr(repo.Create(ctx, cyrillic, cyrillicToken))
		}()
		wg.Wait()

		t.Cleanup(func() {
			_ = delErr(repo.Delete(ctx, latin.ID, 0))
			_ = delErr(repo.Delete(ctx, cyrillic.ID, 0))
		})

		var succeeded, refused int
		for _, err := range results {
			if err == nil {
				succeeded++
				continue
			}
			refused++
			errutil.AssertErrorCode(t, err, "NAME_CONFUSABLE")
		}
		require.Equal(t, 1, succeeded, "iteration %d: exactly one of a confusable pair may land", i)
		require.Equal(t, 1, refused, "iteration %d: the other must be refused NAME_CONFUSABLE", i)
	}
}

// TestConcurrentCreatesOfNonConfusableNamesBothSucceed is the paired positive
// control: the refusal above cannot be passing because concurrency itself is
// broken.
func TestConcurrentCreatesOfNonConfusableNamesBothSucceed(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	first := newGuardChar(ctx, t, charFixtureName("unrelated one"))
	second := newGuardChar(ctx, t, charFixtureName("unrelated two"))
	firstToken := admit(ctx, t, first.Name)
	secondToken := admit(ctx, t, second.Name)

	t.Cleanup(func() {
		_ = delErr(repo.Delete(ctx, first.ID, 0))
		_ = delErr(repo.Delete(ctx, second.ID, 0))
	})

	results := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); results[0] = delErr(repo.Create(ctx, first, firstToken)) }()
	go func() { defer wg.Done(); results[1] = delErr(repo.Create(ctx, second, secondToken)) }()
	wg.Wait()

	require.NoError(t, results[0])
	require.NoError(t, results[1])
}

// TestRenameToACaseVariantOfItsOwnNameSucceedsWhileTheSameTargetAgainstAnotherCharacterIsRefused
// asserts both directions of the B-18 self-exclusion channel on ONE fixture, so
// the "succeeds" half cannot pass because nothing was ever seeded.
func TestRenameToACaseVariantOfItsOwnNameSucceedsWhileTheSameTargetAgainstAnotherCharacterIsRefused(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	ownName := charFixtureName("alaric")
	own := newGuardChar(ctx, t, ownName)
	require.NoError(t, delErr(repo.Create(ctx, own, admit(ctx, t, ownName))))
	t.Cleanup(func() { _ = delErr(repo.Delete(ctx, own.ID, 0)) })

	other := newGuardChar(ctx, t, charFixtureName("bystander"))
	require.NoError(t, delErr(repo.Create(ctx, other, admit(ctx, t, other.Name))))
	t.Cleanup(func() { _ = delErr(repo.Delete(ctx, other.ID, 0)) })

	// The SPEC-settled case-variant rename: same uniqueness key, different
	// display form. Its skeleton matches its OWN row and nothing else.
	variant := upperASCII(ownName)

	_, err := repo.Rename(ctx, own.ID, admit(ctx, t, variant), 0, renameIntent(own.ID))
	require.NoError(t, err, "a character must be able to re-case its own name (01-SPEC.md:702-706)")

	got, err := repo.Get(ctx, own.ID)
	require.NoError(t, err)
	assert.Equal(t, variant, got.Name)

	// The SAME target name against a DIFFERENT character is a genuine collision.
	_, err = repo.Rename(ctx, other.ID, admit(ctx, t, variant), 0, renameIntent(other.ID))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "NAME_CONFUSABLE")
}

// TestCreateAndRenameRefuseWhileAnyCharacterRowHasANullSkeletonAndSucceedAfterBackfill
// is D-30's fail-closed rule, one layer below charname.Gate.
func TestCreateAndRenameRefuseWhileAnyCharacterRowHasANullSkeletonAndSucceedAfterBackfill(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	seeded := newGuardChar(ctx, t, charFixtureName("seeded"))
	seededToken := admit(ctx, t, seeded.Name)
	require.NoError(t, delErr(repo.Create(ctx, seeded, seededToken)))
	t.Cleanup(func() { _ = delErr(repo.Delete(ctx, seeded.ID, 0)) })

	// Tokens are minted BEFORE the corpus is broken: admit() repairs it, and the
	// point of this spec is that the WRITE refuses, not that the gate does.
	fresh := newGuardChar(ctx, t, charFixtureName("fresh"))
	freshToken := admit(ctx, t, fresh.Name)
	renameToken := admit(ctx, t, charFixtureName("renamed"))

	// Break the corpus: one row with no skeleton is enough to make it
	// unanswerable, which is the whole D-30 sequencing constraint.
	_, err := testPool.Exec(ctx,
		`UPDATE characters SET name_skeleton = NULL WHERE id = $1`, seeded.ID.String())
	require.NoError(t, err)

	createErr := delErr(repo.Create(ctx, fresh, freshToken))
	require.Error(t, createErr)
	errutil.AssertErrorCode(t, createErr, "NAME_SKELETON_UNVERIFIABLE")
	assert.Equal(t, 0, characterRowCount(ctx, t, fresh.ID), "the refused create must have written nothing")

	_, renameErr := repo.Rename(ctx, seeded.ID, renameToken, 0, renameIntent(seeded.ID))
	require.Error(t, renameErr)
	errutil.AssertErrorCode(t, renameErr, "NAME_SKELETON_UNVERIFIABLE")
	stored, err := repo.Get(ctx, seeded.ID)
	require.NoError(t, err)
	assert.Equal(t, seededToken.Display(), stored.Name, "the refused rename must have written nothing")

	// Repair the corpus and the SAME calls succeed — the paired positive control.
	backfillCharacterSkeletons(ctx, t)

	require.NoError(t, delErr(repo.Create(ctx, fresh, freshToken)))
	t.Cleanup(func() { _ = delErr(repo.Delete(ctx, fresh.ID, 0)) })

	_, err = repo.Rename(ctx, seeded.ID, renameToken, 0, renameIntent(seeded.ID))
	require.NoError(t, err)
}

// TestRenameCommitsItsOutboxEnvelopeWithTheRenamedRow is B-12 / INV-WORLD-4: a
// caller outside the world service that discards the returned delta still
// produces a feed entry.
func TestRenameCommitsItsOutboxEnvelopeWithTheRenamedRow(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	char := newGuardChar(ctx, t, charFixtureName("feedbound"))
	require.NoError(t, delErr(repo.Create(ctx, char, admit(ctx, t, char.Name))))
	t.Cleanup(func() { _ = delErr(repo.Delete(ctx, char.ID, 0)) })

	intent := renameIntent(char.ID)
	newName := charFixtureName("feedbound renamed")

	// The delta is DISCARDED on purpose: that is the caller shape B-12 is about.
	_, err := repo.Rename(ctx, char.ID, admit(ctx, t, newName), 0, intent)
	require.NoError(t, err)

	got, err := repo.Get(ctx, char.ID)
	require.NoError(t, err)
	require.Equal(t, newName, got.Name)

	_, _, _, found := outboxRow(ctx, t, intent.EventID)
	assert.True(t, found,
		"the rename envelope must be committed with the renamed row, even for a caller that discards the delta")
}

// TestCreateAndRenameRefuseAZeroAdmittedWithoutIssuingSQL pairs each refusal
// with the corresponding success on the same fixture, so neither can pass
// because the write itself is broken.
func TestCreateAndRenameRefuseAZeroAdmittedWithoutIssuingSQL(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	char := newGuardChar(ctx, t, charFixtureName("unadmitted"))

	var zero charname.Admitted
	err := delErr(repo.Create(ctx, char, zero))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHARACTER_NAME_NOT_ADMITTED")
	assert.Equal(t, 0, characterRowCount(ctx, t, char.ID), "a refused create must issue no SQL")

	// Paired success on the same fixture.
	require.NoError(t, delErr(repo.Create(ctx, char, admit(ctx, t, char.Name))))
	t.Cleanup(func() { _ = delErr(repo.Delete(ctx, char.ID, 0)) })

	_, err = repo.Rename(ctx, char.ID, zero, 0, renameIntent(char.ID))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHARACTER_NAME_NOT_ADMITTED")

	renamed := charFixtureName("admitted rename")
	_, err = repo.Rename(ctx, char.ID, admit(ctx, t, renamed), 0, renameIntent(char.ID))
	require.NoError(t, err)
}

// TestCreatePersistsAllFourIdentityColumnsFromTheTokenAndIgnoresTheStructName
// proves the token, not char.Name, is what reaches the database.
func TestCreatePersistsAllFourIdentityColumnsFromTheTokenAndIgnoresTheStructName(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	admittedName := charFixtureName("token wins")
	token := admit(ctx, t, admittedName)

	char := newGuardChar(ctx, t, admittedName)
	// Mutate the struct AFTER admission: a caller that did this must not be able
	// to slip an unadmitted string past the gate.
	char.Name = charFixtureName("struct loses")

	require.NoError(t, delErr(repo.Create(ctx, char, token)))
	t.Cleanup(func() { _ = delErr(repo.Delete(ctx, char.ID, 0)) })

	got, err := repo.Get(ctx, char.ID)
	require.NoError(t, err)
	assert.Equal(t, admittedName, got.Name)
	assert.Equal(t, admittedName, char.Name, "Create refreshes char.Name from the token")

	key, skeleton, version := characterDBIdentity(ctx, t, char.ID)
	assert.Equal(t, token.Key(), key)
	assert.Equal(t, token.Skeleton(), skeleton)
	assert.Equal(t, token.UnicodeVersion(), version)
}

// TestRenameSurfacesConcurrentEditAndNotFoundLikeEveryOtherVersionedWriter.
func TestRenameSurfacesConcurrentEditAndNotFoundLikeEveryOtherVersionedWriter(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	char := newGuardChar(ctx, t, charFixtureName("cas subject"))
	require.NoError(t, delErr(repo.Create(ctx, char, admit(ctx, t, char.Name))))
	t.Cleanup(func() { _ = delErr(repo.Delete(ctx, char.ID, 0)) })

	_, err := testPool.Exec(ctx, `UPDATE characters SET version = version + 1 WHERE id = $1`, char.ID.String())
	require.NoError(t, err)

	_, err = repo.Rename(ctx, char.ID, admit(ctx, t, charFixtureName("stale rename")), char.Version, renameIntent(char.ID))
	require.Error(t, err)
	assert.ErrorIs(t, err, world.ErrConcurrentEdit)
	errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)

	absent := ulid.Make()
	_, err = repo.Rename(ctx, absent, admit(ctx, t, charFixtureName("ghost rename")), 0, renameIntent(absent))
	require.Error(t, err)
	assert.ErrorIs(t, err, world.ErrNotFound)
	errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
}

// renameIntent builds the envelope intent Rename writes in its own transaction.
func renameIntent(characterID ulid.ULID) wmodel.EnvelopeIntent {
	return wmodel.NewEnvelopeIntent(wmodel.IntentParams{
		GameID:        "main",
		Kind:          "character_updated",
		SchemaVersion: 1,
		Actor:         "system",
		AggregateType: wmodel.AggregateCharacter,
		AggregateID:   characterID,
		Payload:       []byte(`{"renamed":true}`),
	})
}

// characterRowCount reports how many characters rows carry the given id — 0 or 1.
func characterRowCount(ctx context.Context, t *testing.T, id ulid.ULID) int {
	t.Helper()
	var n int
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT count(*) FROM characters WHERE id = $1`, id.String()).Scan(&n))
	return n
}

// upperASCII uppercases the ASCII letters of a name, producing the case variant
// 01-SPEC.md §702-706 settles as a real rename.
func upperASCII(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 'a' - 'A'
		}
	}
	return string(b)
}
