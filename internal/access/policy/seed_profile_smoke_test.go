// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// This file is `package policy_test` — an EXTERNAL test package — on purpose.
//
// The plan directed that createSeedEngine (seed_smoke_test.go) be reimplemented
// as a thin call into abactest.NewSeedEngine, so there would be one builder
// rather than two that can diverge. That does not compile: seed_smoke_test.go is
// `package policy`, abactest imports internal/access/policy, and Go rejects an
// in-package test file importing a package that depends on the package under
// test — `import cycle not allowed in test`. An external test package is exempt,
// because it is compiled as a separate package.
//
// So createSeedEngine stays as it is, and the B-6 closure the delegation was
// meant to buy is bought here instead: if the exported
// NewCompiler → NewCache → Reload → NewEngine path could not build an
// equivalent engine over the seed corpus, internal/access/policy's OWN test
// suite goes RED — rather than three downstream plans discovering it.
package policy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/testsupport/abactest"
)

const (
	smokePlayerID   = "01JQ00000000000000000000P1"
	smokeCharacterD = "01JQ00000000000000000000C1"
	smokePropertyID = "01JQ0000000000000000000001"
)

// TestTheWholeSeedCorpusCompilesAndLoadsAgainstTheRealEngine is this plan's
// smoke assertion: every new DSL string compiles and the engine loads the whole
// corpus through the real compile-and-load path.
//
// The BEHAVIOURAL assertions deliberately live elsewhere — plan 02-08 owns the
// tier-floor and conjunction behaviour, plan 02-09 the admin-section behaviour.
// Asserting them here would duplicate their gates without their paired positive
// controls.
func TestTheWholeSeedCorpusCompilesAndLoadsAgainstTheRealEngine(t *testing.T) {
	t.Parallel()

	engine := abactest.NewSeedEngine(
		t,
		abactest.ViewerProvider(abactest.Viewer{
			Tier:     access.ViewerTierPlayer,
			PlayerID: smokePlayerID,
			Roles:    []string{"admin"},
		}),
		abactest.PlayerProvider(abactest.Player{
			ID:    smokePlayerID,
			Roles: []string{"admin"},
		}),
		abactest.PropertyProvider(abactest.PropertyFixture{
			ID:               smokePropertyID,
			Name:             "profile.pronouns",
			ParentType:       "character",
			Visibility:       "public",
			OwnerCharacterID: smokeCharacterD,
			CharacterOwners:  map[string]string{smokeCharacterD: smokePlayerID},
		}),
	)

	require.NotNil(t, engine)
	assert.False(t, engine.IsDegraded())

	// One evaluation, to prove the loaded snapshot is actually reachable rather
	// than merely constructed. A viewer:anonymous subject against an admin
	// section: the seeds are player-flavored there, so this resolves
	// EffectDefaultDeny — which is the engine answering, not failing.
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  access.ViewerSubject(access.ViewerTierAnonymous, ""),
		Action:   "read",
		Resource: access.AdminSectionResource("characters"),
	})
	require.NoError(t, err, "the engine MUST answer rather than error")
	assert.Equal(t, types.EffectDefaultDeny, decision.Effect(),
		"an anonymous viewer matches no admin-section policy, so the engine default-denies")
}
