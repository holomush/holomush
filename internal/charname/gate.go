// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname

import (
	"context"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/charname/syntax"
)

// SkeletonLookup answers "is any existing character already holding this
// confusable skeleton?" against the stored corpus.
//
// # Why there are two booleans
//
// exists is the ordinary answer. unverifiable is the D-30 fail-closed channel:
// an implementation sets it when at least one characters row still has
// name_skeleton IS NULL, which means the corpus cannot answer the question.
//
// A gate that reads a NULL skeleton and reports "no collision" ADMITS a
// confusable of an existing row. That window is not only this phase's backfill
// gap — it reopens after every future Unicode-version upgrade whose recompute
// is interrupted, which is precisely the state characters.name_skeleton_
// unicode_version exists to make visible. So the flag is part of the interface
// rather than a convention a caller may forget: Gate.Check refuses admission
// outright when it is set.
//
// # Why excluding is a parameter
//
// 01-SPEC.md §6.1.4/§702-706 settles that a request whose normalized
// uniqueness key matches the current one but whose display form differs — a
// case or spacing variant — is a REAL rename, and does not collide with
// itself, "because the row it would collide with is its own". Without an
// excluded-id channel, renaming Alaric -> ALARIC computes the identical
// skeleton, matches the character's own row and is refused forever. excluding
// is nil when the caller is creating rather than renaming.
type SkeletonLookup interface {
	SkeletonExists(ctx context.Context, skeleton string, excluding *ulid.ULID) (exists bool, unverifiable bool, err error)
}

// checkOptions carries the optional inputs to Gate.Check.
type checkOptions struct {
	excluding *ulid.ULID
}

// CheckOption adjusts a single Gate.Check call.
//
// Options are variadic on purpose: Check(ctx, name) keeps compiling at every
// call site later plans add, so a new channel is additive rather than a
// signature churn rippling across four plans.
type CheckOption func(*checkOptions)

// ExcludingCharacter tells Check to ignore the given character's own row when
// looking for a skeleton collision. Supply it when renaming an existing
// character; omit it when creating a new one.
func ExcludingCharacter(id ulid.ULID) CheckOption {
	return func(o *checkOptions) { o.excluding = &id }
}

// Gate is the single admission decision for character names.
//
// It is the composition point every later mechanism plugs into — the
// mixed-script step, the block list, and the Admitted token all attach HERE
// rather than at call sites, so adding a rule cannot miss a writer.
type Gate struct {
	// Skeletons resolves confusable collisions against the stored corpus.
	Skeletons SkeletonLookup
}

// Check runs the full character-name admission decision and returns the
// normalized pair plus the skeleton that was adjudicated.
//
// Order is load-bearing:
//
//  1. Normalize        — §6.1.1; a submission with no normal form stops here
//  2. syntax.ValidateName on the DISPLAY form
//  3. MixedScript      — §6.1.2 Mechanism A, on the DISPLAY form
//  4. Skeleton(key) and the corpus lookup
//
// Step 3 sits before step 4 deliberately: a cross-script splice is decidable
// from the submission alone, so refusing it here costs no database round trip
// and hands an attacker no timing signal about the corpus. It sits AFTER step 2
// because the syntactic rules already admit only Unicode letters, which keeps
// the script set free of the punctuation and digit noise a raw submission
// carries.
//
// # Check SUBSUMES the syntactic rules, deliberately
//
// Step 2 is not a convenience. Check covers normalization and confusability;
// the syntactic rules separately own rune-length bounds, letters-and-spaces,
// and the whitespace rules. If they stayed apart, the admission token a later
// plan mints from a successful Check would attest LESS than every writer
// requires — a name carrying digits, punctuation, or 200 runes would receive
// an Admitted and be written by any call site that forgot the second call.
// The alternative fix ("every writer requires both proofs") reintroduces
// exactly the remember-to-call-it class the token exists to remove. So the
// gate subsumes, and one Check verdict proves the whole contract.
//
// Step 2 runs on the post-Normalize display form, not the raw submission: by
// then the leading, trailing and doubled spaces a player typed have already
// been collapsed, so padding is forgiven while genuinely invalid spelling is
// not.
func (g *Gate) Check(ctx context.Context, submitted string, opts ...CheckOption) (Normalized, string, error) {
	var cfg checkOptions
	for _, opt := range opts {
		opt(&cfg)
	}

	normalized, err := Normalize(submitted)
	if err != nil {
		return Normalized{}, "", err
	}

	if syntaxErr := syntax.ValidateName(normalized.Display); syntaxErr != nil {
		// Wrap rather than replace: the *syntax.ValidationError survives in the
		// chain so the create path's existing CHARACTER_INVALID_NAME mapping
		// keeps working.
		return Normalized{}, "", oops.Code("NAME_INVALID_SYNTAX").Wrap(syntaxErr)
	}

	if mixedErr := MixedScript(normalized.Display); mixedErr != nil {
		return Normalized{}, "", mixedErr
	}

	skeleton := Skeleton(normalized.Key)

	exists, unverifiable, err := g.Skeletons.SkeletonExists(ctx, skeleton, cfg.excluding)
	if err != nil {
		return Normalized{}, "", oops.Code("NAME_SKELETON_LOOKUP_FAILED").Wrap(err)
	}
	if unverifiable {
		return Normalized{}, "", oops.
			Code("NAME_SKELETON_UNVERIFIABLE").
			Errorf("character name cannot be checked for confusability right now; try again shortly")
	}
	if exists {
		// The message names NO colliding character. Naming it — or its id —
		// would turn this gate into a name-enumeration oracle: an attacker
		// could probe skeletons and read back who holds each one.
		return Normalized{}, "", oops.
			Code("NAME_CONFUSABLE").
			Errorf("that character name is too similar to an existing one")
	}

	return normalized, skeleton, nil
}
