// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestCapabilityCryptoOperatorIsCryptoOperator(t *testing.T) {
	assert.Equal(t, "crypto.operator", access.CapabilityCryptoOperator)
}

// fakeResolver implements the SubjectResolver interface (single method)
// for HasPlayerGrant tests. *attribute.Resolver also satisfies
// SubjectResolver implicitly; tests that need full ABAC resolution
// construct a real one.
//
// nilBags=true forces the resolver to return (nil, nil) on success so
// HasPlayerGrant's nil-bags fail-closed guard can be exercised.
type fakeResolver struct {
	grants  []string
	err     error
	nilBags bool
}

func (f *fakeResolver) ResolveSubjectAttributes(_ context.Context, _ string, _ string) (*types.AttributeBags, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.nilBags {
		return nil, nil
	}
	bags := &types.AttributeBags{
		Subject: map[string]any{},
	}
	if f.grants != nil {
		bags.Subject[access.PlayerGrantsAttribute] = f.grants
	}
	return bags, nil
}

// Compile-time assertion that fakeResolver satisfies SubjectResolver.
var _ access.SubjectResolver = (*fakeResolver)(nil)

func TestHasPlayerGrant_OperatorPermits(t *testing.T) {
	res := &fakeResolver{grants: []string{"crypto.operator"}}
	ok, err := access.HasPlayerGrant(context.Background(), res, "01HZAVGE83MGFEXQQH5SP9NXKF", access.CapabilityCryptoOperator)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestHasPlayerGrant_NonOperatorDenies(t *testing.T) {
	res := &fakeResolver{grants: []string{}}
	ok, err := access.HasPlayerGrant(context.Background(), res, "01HZAVGE83MGFEXQQH5SP9NXKF", access.CapabilityCryptoOperator)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestHasPlayerGrant_DifferentGrantNotMatched(t *testing.T) {
	res := &fakeResolver{grants: []string{"other.grant"}}
	ok, err := access.HasPlayerGrant(context.Background(), res, "01HZAVGE83MGFEXQQH5SP9NXKF", access.CapabilityCryptoOperator)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestHasPlayerGrant_GenericOverGrantName(t *testing.T) {
	res := &fakeResolver{grants: []string{"some.future.grant"}}
	ok, err := access.HasPlayerGrant(context.Background(), res, "01HZAVGE83MGFEXQQH5SP9NXKF", "some.future.grant")
	require.NoError(t, err)
	assert.True(t, ok, "facade must match any grant string by exact equality, not just CapabilityCryptoOperator")
}

func TestHasPlayerGrant_PropagatesResolverError(t *testing.T) {
	boom := errors.New("resolver boom")
	res := &fakeResolver{err: boom}
	ok, err := access.HasPlayerGrant(context.Background(), res, "01HZAVGE83MGFEXQQH5SP9NXKF", access.CapabilityCryptoOperator)
	require.Error(t, err)
	assert.False(t, ok)
	assert.ErrorIs(t, err, boom, "resolver error must propagate")
}

func TestHasPlayerGrant_NilBagsFromResolverFailsClosed(t *testing.T) {
	res := &fakeResolver{nilBags: true}
	ok, err := access.HasPlayerGrant(context.Background(), res, "01HZAVGE83MGFEXQQH5SP9NXKF", access.CapabilityCryptoOperator)
	require.NoError(t, err)
	assert.False(t, ok, "nil bags from resolver must fail-closed without panic")
}

func TestHasPlayerGrant_RejectsEmptyPlayerID(t *testing.T) {
	res := &fakeResolver{grants: []string{"crypto.operator"}}
	ok, err := access.HasPlayerGrant(context.Background(), res, "", access.CapabilityCryptoOperator)
	require.Error(t, err)
	assert.False(t, ok)
	errutil.AssertErrorCode(t, err, "PLAYER_ID_EMPTY")
}

func TestHasPlayerGrant_RejectsEmptyGrant(t *testing.T) {
	res := &fakeResolver{grants: []string{"crypto.operator"}}
	ok, err := access.HasPlayerGrant(context.Background(), res, "01HZAVGE83MGFEXQQH5SP9NXKF", "")
	require.Error(t, err)
	assert.False(t, ok)
	errutil.AssertErrorCode(t, err, "GRANT_EMPTY")
}
