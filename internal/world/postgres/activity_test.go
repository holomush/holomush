// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

// recordingExecer captures the one statement UpdateCharacterLastActive issues
// and returns a caller-chosen CommandTag / error.
type recordingExecer struct {
	sql  string
	args []any
	tag  pgconn.CommandTag
	err  error
}

func (r *recordingExecer) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	r.sql = sql
	r.args = args
	return r.tag, r.err
}

func TestUpdateCharacterLastActiveIssuesTheMonotonicGuardedUpdate(t *testing.T) {
	ctx := context.Background()
	id := ulid.Make()
	ex := &recordingExecer{tag: pgconn.NewCommandTag("UPDATE 1")}

	require.NoError(t, UpdateCharacterLastActive(ctx, ex, id, 1234567890))

	lowered := strings.ToLower(ex.sql)
	assert.Contains(t, lowered, "update characters",
		"the fenced characters mutation must live here and target characters")
	assert.Contains(t, lowered, "last_active_at < ",
		"the monotonic guard is the whole idempotency argument — a stale value must match zero rows")
	assert.NotContains(t, lowered, "version",
		"last_active_at is an operational column: the flush must never touch characters.version")
	assert.Equal(t, []any{id.String(), int64(1234567890)}, ex.args)
}

func TestUpdateCharacterLastActiveTreatsZeroRowsAffectedAsSuccess(t *testing.T) {
	ctx := context.Background()
	// UPDATE 0 is the stale-value, unknown-character and duplicate-flush
	// outcome all at once. None of them is an error: the buffered value simply
	// did not advance anything.
	ex := &recordingExecer{tag: pgconn.NewCommandTag("UPDATE 0")}

	assert.NoError(t, UpdateCharacterLastActive(ctx, ex, ulid.Make(), 42))
}

func TestUpdateCharacterLastActiveWrapsDatabaseFailuresWithItsOwnCode(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("connection reset")
	ex := &recordingExecer{err: boom}

	err := UpdateCharacterLastActive(ctx, ex, ulid.Make(), 42)

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHARACTER_ACTIVITY_FLUSH_FAILED")
	assert.ErrorIs(t, err, boom)
}
