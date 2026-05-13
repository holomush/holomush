// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package approval_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/approval"
	"github.com/holomush/holomush/pkg/errutil"
)

// fakeClock is a deterministic Clock for unit tests.
type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }

func TestNewPostgresRepoSubstitutesNilClock(t *testing.T) {
	r := approval.NewPostgresRepo(nil, nil)
	require.NotNil(t, r) // should not panic; the nil clock is replaced
}

func TestNewPostgresRepoAcceptsExplicitClock(t *testing.T) {
	now := time.Now()
	clk := &fakeClock{t: now}
	r := approval.NewPostgresRepo(nil, clk)
	require.NotNil(t, r)
}

func TestRequestIDStringIsULID(t *testing.T) {
	var id approval.RequestID
	id[0] = 0x01 // non-zero so the string isn't all-zeros
	s := id.String()
	assert.Len(t, s, 26, "ULID string is 26 chars")
}

// TestGetByOpArgsHash_RejectsEmptyExcludePlayerID verifies that GetByOpArgsHash
// fails closed when excludePlayerID is empty, ensuring the SQL dual-control
// exclusion clause is never executed with an empty comparand.
func TestGetByOpArgsHash_RejectsEmptyExcludePlayerID(t *testing.T) {
	// NewPostgresRepo(nil pool, nil clock) is valid for unit tests that exercise
	// pre-query validation guards (no DB call is made).
	r := approval.NewPostgresRepo(nil, nil)

	cases := []struct {
		name string
		id   string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
		{"tab only", "\t"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.GetByOpArgsHash(context.Background(), "readstream", []byte("hash"), tc.id)
			require.Error(t, err, "empty excludePlayerID must be rejected")
			errutil.AssertErrorCode(t, err, "APPROVAL_INVALID_ARGUMENT")
		})
	}
}
