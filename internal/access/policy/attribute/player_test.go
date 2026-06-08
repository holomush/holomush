// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/pkg/errutil"
)

const (
	testOperatorULID    = "01HZAVGE83MGFEXQQH5SP9NXKF"
	testNonOperatorULID = "01HZAVGE83MGFEXQQH5SP9NXKG"
)

// --- Task 5: scaffolding -----------------------------------------------------

func TestPlayerProviderNamespace(t *testing.T) {
	p := NewPlayerAttributeProvider(nil)
	assert.Equal(t, "player", p.Namespace())
}

func TestPlayerProviderSchema(t *testing.T) {
	p := NewPlayerAttributeProvider(nil)
	schema := p.Schema()
	require.NotNil(t, schema)
	assert.Equal(t, types.AttrTypeString, schema.Attributes["id"])
	assert.Equal(t, types.AttrTypeStringList, schema.Attributes["grants"])
	assert.Equal(t, types.AttrTypeBool, schema.Attributes["is_guest"])
	assert.Equal(t, types.AttrTypeBool, schema.Attributes["has_is_guest"])
	assert.Len(t, schema.Attributes, 4, "v1 schema exposes id, grants, is_guest, has_is_guest")
}

func TestPlayerProviderResolveResourceAlwaysNil(t *testing.T) {
	p := NewPlayerAttributeProvider(nil)
	attrs, err := p.ResolveResource(context.Background(), "player:01HZAVGE83MGFEXQQH5SP9NXKF")
	assert.NoError(t, err)
	assert.Nil(t, attrs, "players are subjects, never resources in this design")

	attrs, err = p.ResolveResource(context.Background(), "character:01HZAVGE83MGFEXQQH5SP9NXKG")
	assert.NoError(t, err)
	assert.Nil(t, attrs)
}

func TestPlayerProviderConstructorDeduplicates(t *testing.T) {
	p := NewPlayerAttributeProvider([]string{"01A", "01A", "01B"})
	assert.Equal(t, 2, p.operatorCount())
}

func TestPlayerProviderConstructorEmptyInput(t *testing.T) {
	p := NewPlayerAttributeProvider(nil)
	assert.Equal(t, 0, p.operatorCount())

	p = NewPlayerAttributeProvider([]string{})
	assert.Equal(t, 0, p.operatorCount())
}

// --- Task 6: ResolveSubject --------------------------------------------------

func TestPlayerProviderResolveSubjectOperator(t *testing.T) {
	p := NewPlayerAttributeProvider([]string{testOperatorULID})
	attrs, err := p.ResolveSubject(context.Background(), "player:"+testOperatorULID)
	require.NoError(t, err)
	require.NotNil(t, attrs)

	assert.Equal(t, testOperatorULID, attrs["id"])
	assert.Equal(t, []string{access.CapabilityCryptoOperator}, attrs["grants"])
}

func TestPlayerProviderResolveSubjectNonOperator(t *testing.T) {
	p := NewPlayerAttributeProvider([]string{testOperatorULID})
	attrs, err := p.ResolveSubject(context.Background(), "player:"+testNonOperatorULID)
	require.NoError(t, err)
	require.NotNil(t, attrs)

	assert.Equal(t, testNonOperatorULID, attrs["id"])
	grants, ok := attrs["grants"].([]string)
	require.True(t, ok, "grants must be []string")
	assert.NotNil(t, grants, "grants slice must be non-nil even when empty")
	assert.Empty(t, grants)
}

func TestPlayerProviderResolveSubjectNonPlayerNamespace(t *testing.T) {
	p := NewPlayerAttributeProvider([]string{testOperatorULID})

	cases := []string{
		"character:" + testOperatorULID,
		"location:01HZAVGE83MGFEXQQH5SP9NXKH",
		"plugin:my-plugin",
	}
	for _, sid := range cases {
		t.Run(sid, func(t *testing.T) {
			attrs, err := p.ResolveSubject(context.Background(), sid)
			assert.NoError(t, err)
			assert.Nil(t, attrs, "non-player subject must return nil bag")
		})
	}
}

func TestPlayerProviderResolveSubjectMalformedSubject(t *testing.T) {
	p := NewPlayerAttributeProvider(nil)

	cases := []struct {
		name       string
		subjectID  string
		wantCode   string
		wantSubstr string
	}{
		{
			name:       "empty post-colon",
			subjectID:  "player:",
			wantCode:   "INVALID_ENTITY_ID",
			wantSubstr: "invalid subject ID format",
		},
		{
			name:       "no colon",
			subjectID:  "playerULID",
			wantCode:   "INVALID_ENTITY_ID",
			wantSubstr: "invalid subject ID format",
		},
		{
			name:       "non-ULID under player namespace",
			subjectID:  "player:not-a-ulid",
			wantCode:   "INVALID_PLAYER_ID",
			wantSubstr: "invalid player ULID",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attrs, err := p.ResolveSubject(context.Background(), tc.subjectID)
			assert.Nil(t, attrs)
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, tc.wantCode)
			assert.Contains(t, err.Error(), tc.wantSubstr)
		})
	}
}

// --- Task 8: contract + concurrency + no-mutation ----------------------------

func TestPlayerProviderContract(t *testing.T) {
	p := NewPlayerAttributeProvider(nil)
	assertProviderContract(t, p)
}

func TestPlayerProviderConcurrentResolves(t *testing.T) {
	p := NewPlayerAttributeProvider([]string{
		testOperatorULID,
		"01HZAVGE83MGFEXQQH5SP9NXKH",
		"01HZAVGE83MGFEXQQH5SP9NXKJ",
	})

	cases := []struct {
		subject    string
		wantInList bool
	}{
		{"player:" + testOperatorULID, true},
		{"player:01HZAVGE83MGFEXQQH5SP9NXKH", true},
		{"player:01HZAVGE83MGFEXQQH5SP9NXKJ", true},
		{"player:" + testNonOperatorULID, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.subject, func(t *testing.T) {
			t.Parallel()
			attrs, err := p.ResolveSubject(context.Background(), tc.subject)
			require.NoError(t, err)
			grants, ok := attrs["grants"].([]string)
			require.True(t, ok)
			if tc.wantInList {
				assert.Contains(t, grants, access.CapabilityCryptoOperator)
			} else {
				assert.NotContains(t, grants, access.CapabilityCryptoOperator)
			}
		})
	}
}

// TestPlayerProviderNoMutationAPI enforces INV-B6: the operator set is
// captured at construction and read-only thereafter. No exported method on
// *PlayerAttributeProvider may have a mutator-shaped name. Reflection-based
// meta-test catches future refactors that would silently re-introduce a
// reload/set/add API.
func TestPlayerProviderNoMutationAPI(t *testing.T) {
	typ := reflect.TypeOf(&PlayerAttributeProvider{})

	bannedPrefixes := []string{
		"Set", "Add", "Remove", "Reload", "Clear", "Update", "Insert", "Delete",
	}
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		for _, banned := range bannedPrefixes {
			assert.False(
				t,
				strings.HasPrefix(m.Name, banned),
				"PlayerAttributeProvider must not expose mutator %q (INV-B6 — operator set is read-only post-construction)",
				m.Name,
			)
		}
	}
}

// --- Task 13: is_guest attribute (omit-don't-sentinel, ADR holomush-ti1b) ----

// TestPlayerProviderEmitsIsGuestWitnessWhenLookupConfigured verifies that when
// a PlayerKindLookup is configured, ResolveSubject emits both is_guest and
// has_is_guest on every code path: true/true for a guest player, false/true for
// a registered player. Per ADR holomush-ti1b: the value key is always emitted
// when the lookup resolves, and the witness is always present.
func TestPlayerProviderEmitsIsGuestWitnessWhenLookupConfigured(t *testing.T) {
	guestID := testOperatorULID
	regularID := testNonOperatorULID

	lookup := func(_ context.Context, playerID string) (bool, error) {
		return playerID == guestID, nil
	}

	p := NewPlayerAttributeProvider(nil, WithPlayerKindLookup(lookup))

	t.Run("guest player emits is_guest true and has_is_guest true", func(t *testing.T) {
		attrs, err := p.ResolveSubject(context.Background(), "player:"+guestID)
		require.NoError(t, err)
		require.NotNil(t, attrs)
		assert.Equal(t, true, attrs["is_guest"], "is_guest must be true for guest player")
		assert.Equal(t, true, attrs["has_is_guest"], "has_is_guest must always be present when lookup configured")
	})

	t.Run("registered player emits is_guest false and has_is_guest true", func(t *testing.T) {
		attrs, err := p.ResolveSubject(context.Background(), "player:"+regularID)
		require.NoError(t, err)
		require.NotNil(t, attrs)
		assert.Equal(t, false, attrs["is_guest"], "is_guest must be false for registered player")
		assert.Equal(t, true, attrs["has_is_guest"], "has_is_guest must always be present when lookup configured")
	})
}

// TestPlayerProviderOmitsIsGuestWhenLookupAbsentOrFails verifies the
// omit-don't-sentinel invariant (ADR holomush-ti1b): when no lookup is
// configured or the lookup returns an error, the "is_guest" key MUST be absent
// from the attribute bag (never emitted as "" or false). The witness
// has_is_guest MUST be false on every unresolved path.
func TestPlayerProviderOmitsIsGuestWhenLookupAbsentOrFails(t *testing.T) {
	t.Run("no lookup configured: is_guest key absent and has_is_guest false", func(t *testing.T) {
		p := NewPlayerAttributeProvider(nil)
		attrs, err := p.ResolveSubject(context.Background(), "player:"+testOperatorULID)
		require.NoError(t, err)
		require.NotNil(t, attrs)

		_, ok := attrs["is_guest"]
		assert.False(t, ok, "is_guest key MUST be absent when no lookup configured (omit-don't-sentinel, ADR holomush-ti1b)")
		assert.Equal(t, false, attrs["has_is_guest"], "has_is_guest must be false when lookup not configured")
	})

	t.Run("lookup returns error: is_guest key absent and has_is_guest false", func(t *testing.T) {
		lookup := func(_ context.Context, _ string) (bool, error) {
			return false, oops.Code("PLAYER_NOT_FOUND").Errorf("player not found")
		}
		p := NewPlayerAttributeProvider(nil, WithPlayerKindLookup(lookup))
		attrs, err := p.ResolveSubject(context.Background(), "player:"+testOperatorULID)
		require.NoError(t, err, "lookup errors must not bubble out of ResolveSubject")
		require.NotNil(t, attrs)

		_, ok := attrs["is_guest"]
		assert.False(t, ok, "is_guest key MUST be absent when lookup errors (omit-don't-sentinel, ADR holomush-ti1b)")
		assert.Equal(t, false, attrs["has_is_guest"], "has_is_guest must be false when lookup fails")
	})
}
