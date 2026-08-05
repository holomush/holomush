// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/charname"
)

// This file holds the fixtures every character-creating integration test in
// this package needs after plan 02-06 made charname.Admitted the only way to
// write characters.name.
//
// There is NO test escape hatch for Admitted BY DESIGN — its single constructor
// is the whole guarantee — so a fixture mints a REAL token through a REAL
// charname.Gate. What the fixture supplies is a stub SkeletonLookup: the gate's
// corpus read is a pre-check, and the assertion that actually matters
// (serialization + fail-closed) is made by guardSkeleton inside the write
// transaction against the REAL corpus, which these fixtures do not stub.

// fixtureLookup is a charname.SkeletonLookup that reports no collision. It
// keeps ORDINARY fixtures independent of whatever other tests have left in the
// shared database; the confusable, self-exclusion and fail-closed behaviours are
// asserted directly against the real corpus in skeleton_guard_test.go.
type fixtureLookup struct{}

func (fixtureLookup) SkeletonExists(_ context.Context, _ string, _ *ulid.ULID) (bool, bool, error) {
	return false, false, nil
}

// admit mints a real admission token for a fixture name, repairing the corpus
// first.
//
// The backfill is not optional housekeeping: migration 000001_baseline.sql seeds
// a bootstrap character with no name_skeleton, and several fixtures in this
// package insert characters by direct SQL, so a stock database ALWAYS has rows
// the guard correctly refuses to adjudicate against (D-30). Without the repair
// every Create in this package would fail NAME_SKELETON_UNVERIFIABLE — and the
// guard would be RIGHT.
func admit(ctx context.Context, t *testing.T, name string) charname.Admitted {
	t.Helper()
	backfillCharacterSkeletons(ctx, t)

	gate := &charname.Gate{Skeletons: fixtureLookup{}}
	token, err := gate.Admit(ctx, name)
	require.NoError(t, err, "fixture name %q must be admissible", name)
	return token
}

// backfillCharacterSkeletons populates the identity columns of every characters
// row that is missing them. It stands in for plan 02-12's 000055 Go migration
// and is the same stand-in plans 02-01 and 02-05 introduced for their suites.
func backfillCharacterSkeletons(ctx context.Context, t *testing.T) {
	t.Helper()
	rows, err := testPool.Query(ctx, `
		SELECT id, name FROM characters
		WHERE normalized_name IS NULL
		   OR name_skeleton IS NULL
		   OR name_skeleton_unicode_version IS NULL
	`)
	require.NoError(t, err)

	type pending struct{ id, key, skeleton string }
	var todo []pending
	for rows.Next() {
		var id, name string
		require.NoError(t, rows.Scan(&id, &name))
		normalized, nErr := charname.Normalize(name)
		if nErr != nil {
			// A legacy row whose name has no normal form cannot be backfilled;
			// stamp a deterministic placeholder so the corpus stays verifiable.
			todo = append(todo, pending{id: id, key: id, skeleton: id})
			continue
		}
		todo = append(todo, pending{
			id:       id,
			key:      normalized.Key,
			skeleton: charname.Skeleton(normalized.Key),
		})
	}
	require.NoError(t, rows.Err())
	rows.Close()

	for _, p := range todo {
		_, err := testPool.Exec(ctx, `
			UPDATE characters
			SET normalized_name = $2, name_skeleton = $3, name_skeleton_unicode_version = $4
			WHERE id = $1
		`, p.id, p.key, p.skeleton, charname.UnicodeVersion)
		require.NoError(t, err)
	}
}

// fixtureNameLetters is the alphabet charFixtureName draws from. Letters only,
// because syntax.ValidateName admits Unicode letters and single spaces and
// nothing else — a ULID suffix (which carries digits) is not a legal character
// name and never was.
const fixtureNameLetters = "abcdefghijklmnopqrstuvwxyz"

// charFixtureName returns a gate-admissible, corpus-unique fixture name:
// prefix, a space, and eight random lowercase letters.
//
// Uniqueness matters more than it used to. Before this phase two fixtures could
// share a name freely; now a second character whose SKELETON matches a live row
// is refused by guardSkeleton, and these tests share one database across every
// test in the package. The random suffix makes that collision effectively
// impossible without coupling fixtures to each other's cleanup.
func charFixtureName(prefix string) string {
	return prefix + " " + randomFixtureLetters(8)
}

// randomFixtureLetters returns n random lowercase Latin letters.
func randomFixtureLetters(n int) string {
	buf := make([]byte, n)
	for i := range buf {
		// crypto/rand, never math/rand (project rule).
		v, err := rand.Int(rand.Reader, big.NewInt(int64(len(fixtureNameLetters))))
		if err != nil {
			panic("fixture name generation failed: " + err.Error())
		}
		buf[i] = fixtureNameLetters[v.Int64()]
	}
	return string(buf)
}

// characterDBIdentity reads a character's three derived identity columns
// directly, out-of-band of the repository, so a test can assert what actually
// landed rather than what a method returned.
func characterDBIdentity(ctx context.Context, t *testing.T, id ulid.ULID) (key, skeleton, unicodeVersion string) {
	t.Helper()
	err := testPool.QueryRow(ctx, `
		SELECT normalized_name, name_skeleton, name_skeleton_unicode_version
		FROM characters WHERE id = $1
	`, id.String()).Scan(&key, &skeleton, &unicodeVersion)
	require.NoError(t, err)
	return key, skeleton, unicodeVersion
}

// mustAdmitKey returns the §6.1.1 uniqueness key a name normalizes to.
func mustAdmitKey(ctx context.Context, t *testing.T, name string) string {
	t.Helper()
	return admit(ctx, t, name).Key()
}
