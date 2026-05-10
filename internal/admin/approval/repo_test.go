// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package approval_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/approval"
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
