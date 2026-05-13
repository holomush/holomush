// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package approval_test

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/approval"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

// testPool is the shared database pool for integration tests.
// Declared here (integration build tag) to avoid duplicate declaration with
// the unit-test file that uses none.
var testPool *pgxpool.Pool

// TestMain sets up a PostgreSQL testcontainer for integration tests.
func TestMain(m *testing.M) {
	ctx := context.Background()

	pgEnv, err := testutil.StartPostgres(ctx)
	if err != nil {
		panic("failed to start postgres container: " + err.Error())
	}

	migrator, err := store.NewMigrator(pgEnv.ConnStr)
	if err != nil {
		_ = pgEnv.Terminate(ctx)
		panic("failed to create migrator: " + err.Error())
	}
	if err := migrator.Up(); err != nil {
		_ = migrator.Close()
		_ = pgEnv.Terminate(ctx)
		panic("failed to run migrations: " + err.Error())
	}
	_ = migrator.Close()

	pool, err := pgxpool.New(ctx, pgEnv.ConnStr)
	if err != nil {
		_ = pgEnv.Terminate(ctx)
		panic("failed to create pool: " + err.Error())
	}

	testPool = pool

	code := m.Run()

	pool.Close()
	_ = pgEnv.Terminate(ctx)

	os.Exit(code)
}

// insertPlayerForApproval inserts a minimal player row for use as a
// foreign-key anchor. The username uses the full ULID string to avoid
// the unique-constraint collision that occurs when concurrent tests
// generate ULIDs with the same leading 8 chars (same millisecond).
func insertPlayerForApproval(t *testing.T, id string) {
	t.Helper()
	ctx := context.Background()
	// Prefix + full ULID keeps username unique across concurrent tests.
	username := "apr-" + id
	_, err := testPool.Exec(ctx,
		`INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'x')`,
		id, username)
	require.NoError(t, err, "insertPlayerForApproval")
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, id)
	})
}

// assertCode verifies err is an oops error with the expected code.
func assertCode(t *testing.T, err error, want string) {
	t.Helper()
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T", err)
	assert.Equal(t, want, oopsErr.Code())
}

// TestRepoOpenAndGet verifies that Open inserts a row and Get retrieves it.
func TestRepoOpenAndGet(t *testing.T) {
	r := approval.NewPostgresRepo(testPool, nil)
	primary := ulid.Make().String()
	insertPlayerForApproval(t, primary)

	id, err := r.Open(context.Background(), approval.OpenRequest{
		PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
	})
	require.NoError(t, err)

	got, err := r.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, primary, got.PrimaryPlayerID)
	assert.Equal(t, "rekey", got.OpKind)
	assert.False(t, got.CreatedAt.IsZero())
	assert.Nil(t, got.ApprovedAt)
}

// TestRepoReadFiltersExpired verifies Get returns APPROVAL_NOT_FOUND for
// expired rows (INV-D5).
func TestRepoReadFiltersExpired(t *testing.T) {
	r := approval.NewPostgresRepo(testPool, nil)
	primary := ulid.Make().String()
	insertPlayerForApproval(t, primary)

	id, err := r.Open(context.Background(), approval.OpenRequest{
		PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
	})
	require.NoError(t, err)

	// Force expiry server-side.
	tag, err := testPool.Exec(context.Background(),
		`UPDATE admin_approvals SET expires_at = now() - interval '1 minute' WHERE request_id = $1`,
		id[:])
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected(), "expected exactly one approval row to be force-expired")

	_, err = r.Get(context.Background(), id)
	require.Error(t, err)
	assertCode(t, err, "APPROVAL_NOT_FOUND")
}

// TestRepoMarkApproved verifies the happy-path second-op approval.
func TestRepoMarkApproved(t *testing.T) {
	r := approval.NewPostgresRepo(testPool, nil)
	primary := ulid.Make().String()
	secondOp := ulid.Make().String()
	insertPlayerForApproval(t, primary)
	insertPlayerForApproval(t, secondOp)

	id, err := r.Open(context.Background(), approval.OpenRequest{
		PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
	})
	require.NoError(t, err)

	require.NoError(t, r.MarkApproved(context.Background(), id, secondOp))

	got, err := r.Get(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, got.ApprovedAt)
	assert.Equal(t, secondOp, got.ApprovedByPlayerID)
}

// TestRepoMarkApprovedRejectsSelfApproval verifies INV-D6: self-approval is
// rejected at the SQL WHERE-predicate layer.
func TestRepoMarkApprovedRejectsSelfApproval(t *testing.T) {
	r := approval.NewPostgresRepo(testPool, nil)
	primary := ulid.Make().String()
	insertPlayerForApproval(t, primary)

	id, err := r.Open(context.Background(), approval.OpenRequest{
		PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
	})
	require.NoError(t, err)

	err = r.MarkApproved(context.Background(), id, primary)
	require.Error(t, err)
	assertCode(t, err, "DENY_DUAL_CONTROL_SELF")

	got, err := r.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Nil(t, got.ApprovedAt, "row must remain pending after self-approval rejection")
}

// TestRepoMarkApprovedRejectsExpiredRow verifies INV-D5: MarkApproved on an
// expired row returns DENY_APPROVAL_EXPIRED.
func TestRepoMarkApprovedRejectsExpiredRow(t *testing.T) {
	r := approval.NewPostgresRepo(testPool, nil)
	primary := ulid.Make().String()
	secondOp := ulid.Make().String()
	insertPlayerForApproval(t, primary)
	insertPlayerForApproval(t, secondOp)

	id, err := r.Open(context.Background(), approval.OpenRequest{
		PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
	})
	require.NoError(t, err)

	tag, err := testPool.Exec(context.Background(),
		`UPDATE admin_approvals SET expires_at = now() - interval '1 minute' WHERE request_id = $1`,
		id[:])
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected(), "expected exactly one approval row to be force-expired")

	err = r.MarkApproved(context.Background(), id, secondOp)
	require.Error(t, err)
	assertCode(t, err, "DENY_APPROVAL_EXPIRED")
}

// TestRepoMarkApprovedRejectsAlreadyApproved verifies INV-D7: a second
// MarkApproved on an already-approved row returns DENY_APPROVAL_ALREADY_APPROVED.
func TestRepoMarkApprovedRejectsAlreadyApproved(t *testing.T) {
	r := approval.NewPostgresRepo(testPool, nil)
	primary := ulid.Make().String()
	secondOp := ulid.Make().String()
	insertPlayerForApproval(t, primary)
	insertPlayerForApproval(t, secondOp)

	id, err := r.Open(context.Background(), approval.OpenRequest{
		PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
	})
	require.NoError(t, err)

	require.NoError(t, r.MarkApproved(context.Background(), id, secondOp))

	err = r.MarkApproved(context.Background(), id, secondOp)
	require.Error(t, err)
	assertCode(t, err, "DENY_APPROVAL_ALREADY_APPROVED")
}

// TestRepoConcurrentMarkApproved verifies that concurrent MarkApproved calls
// are serialized by the atomic UPDATE: exactly one succeeds, the rest get
// DENY_APPROVAL_ALREADY_APPROVED.
func TestRepoConcurrentMarkApproved(t *testing.T) {
	r := approval.NewPostgresRepo(testPool, nil)
	primary := ulid.Make().String()
	insertPlayerForApproval(t, primary)

	id, err := r.Open(context.Background(), approval.OpenRequest{
		PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
	})
	require.NoError(t, err)

	const N = 50
	secondOps := make([]string, N)
	for i := range secondOps {
		secondOps[i] = ulid.Make().String()
		insertPlayerForApproval(t, secondOps[i])
	}

	var success atomic.Int32
	var alreadyApproved atomic.Int32
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			err := r.MarkApproved(context.Background(), id, secondOps[i])
			if err == nil {
				success.Add(1)
				return
			}
			oopsErr, ok := oops.AsOops(err)
			if ok && oopsErr.Code() == "DENY_APPROVAL_ALREADY_APPROVED" {
				alreadyApproved.Add(1)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), success.Load(), "exactly one MarkApproved should succeed")
	assert.Equal(t, int32(N-1), alreadyApproved.Load(), "all losers should see ALREADY_APPROVED")
}

// TestRepoWaitForApprovalReturnsOnApproval verifies that WaitForApproval
// returns the approved row once MarkApproved is called concurrently.
func TestRepoWaitForApprovalReturnsOnApproval(t *testing.T) {
	r := approval.NewPostgresRepo(testPool, nil)
	primary := ulid.Make().String()
	secondOp := ulid.Make().String()
	insertPlayerForApproval(t, primary)
	insertPlayerForApproval(t, secondOp)

	id, err := r.Open(context.Background(), approval.OpenRequest{
		PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
	})
	require.NoError(t, err)

	deadline := time.Now().Add(10 * time.Second)

	// Approve in a goroutine after a short delay.
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = r.MarkApproved(context.Background(), id, secondOp)
	}()

	got, err := r.WaitForApproval(context.Background(), id, deadline)
	require.NoError(t, err)
	require.NotNil(t, got.ApprovedAt)
	assert.Equal(t, secondOp, got.ApprovedByPlayerID)
}

// TestRepoWaitForApprovalDeadlineExpires verifies that WaitForApproval returns
// APPROVAL_WAIT_DEADLINE when the deadline passes without approval.
func TestRepoWaitForApprovalDeadlineExpires(t *testing.T) {
	r := approval.NewPostgresRepo(testPool, nil)
	primary := ulid.Make().String()
	insertPlayerForApproval(t, primary)

	id, err := r.Open(context.Background(), approval.OpenRequest{
		PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
	})
	require.NoError(t, err)

	// Deadline already in the past.
	deadline := time.Now().Add(-1 * time.Second)
	_, err = r.WaitForApproval(context.Background(), id, deadline)
	require.Error(t, err)
	assertCode(t, err, "APPROVAL_WAIT_DEADLINE")
}

// TestRepoWaitForApprovalSurfacesExpiry locks in CodeRabbit #4: when the
// row's expires_at has passed, WaitForApproval MUST return
// DENY_APPROVAL_EXPIRED immediately rather than polling past the
// server-enforced TTL until the caller's deadline. The previous
// implementation called Get(), which hides expired rows behind
// APPROVAL_NOT_FOUND, so the loop slept past expiry and dropped the
// intended DENY_APPROVAL_EXPIRED signal.
func TestRepoWaitForApprovalSurfacesExpiry(t *testing.T) {
	r := approval.NewPostgresRepo(testPool, nil)
	primary := ulid.Make().String()
	insertPlayerForApproval(t, primary)

	id, err := r.Open(context.Background(), approval.OpenRequest{
		PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
	})
	require.NoError(t, err)

	// Force-expire the row so the next poll observes expires_at < now().
	tag, err := testPool.Exec(context.Background(),
		`UPDATE admin_approvals SET expires_at = now() - interval '1 minute' WHERE request_id = $1`,
		id[:])
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected(), "expected exactly one approval row to be force-expired")

	// Use a generous deadline; the call MUST return well before then.
	deadline := time.Now().Add(30 * time.Second)
	start := time.Now()
	_, err = r.WaitForApproval(context.Background(), id, deadline)
	elapsed := time.Since(start)

	require.Error(t, err)
	assertCode(t, err, "DENY_APPROVAL_EXPIRED")
	// Sanity: the call must short-circuit on expiry, not run to deadline.
	// Allow generous slack (1s) for goroutine scheduling and DB roundtrip.
	assert.Less(t, elapsed, 1*time.Second,
		"WaitForApproval MUST return DENY_APPROVAL_EXPIRED quickly, not poll until deadline")
}

// openAndApprove is a test helper that opens a fresh approval and immediately
// marks it approved by secondOp, returning the resulting Approval.
func openAndApprove(t *testing.T, r *approval.PostgresRepo, primary, secondOp, opKind string, opArgsHash []byte) approval.Approval {
	t.Helper()
	id, err := r.Open(context.Background(), approval.OpenRequest{
		PrimaryPlayerID: primary, OpKind: opKind, OpArgsHash: opArgsHash,
	})
	require.NoError(t, err)
	require.NoError(t, r.MarkApproved(context.Background(), id, secondOp))
	a, err := r.Get(context.Background(), id)
	require.NoError(t, err)
	return a
}

// TestINV_F17_GetByOpArgsHashMatrix_ReturnsApproved is the INV-F17 happy path:
// an approved, non-expired row not authored by excludePlayerID is returned.
func TestINV_F17_GetByOpArgsHashMatrix_ReturnsApproved(t *testing.T) {
	r := approval.NewPostgresRepo(testPool, nil)
	primary := ulid.Make().String()
	secondOp := ulid.Make().String()
	other := ulid.Make().String()
	insertPlayerForApproval(t, primary)
	insertPlayerForApproval(t, secondOp)

	hash := []byte("hash-happy")
	openAndApprove(t, r, primary, secondOp, "rekey", hash)

	got, err := r.GetByOpArgsHash(context.Background(), "rekey", hash, other)
	require.NoError(t, err)
	assert.Equal(t, primary, got.PrimaryPlayerID)
	assert.NotNil(t, got.ApprovedAt)
}

// TestINV_F17_GetByOpArgsHashMatrix_FiltersExpired verifies that GetByOpArgsHash
// returns APPROVAL_NOT_FOUND for rows where expires_at <= now().
func TestINV_F17_GetByOpArgsHashMatrix_FiltersExpired(t *testing.T) {
	r := approval.NewPostgresRepo(testPool, nil)
	primary := ulid.Make().String()
	secondOp := ulid.Make().String()
	other := ulid.Make().String()
	insertPlayerForApproval(t, primary)
	insertPlayerForApproval(t, secondOp)

	hash := []byte("hash-expired")
	a := openAndApprove(t, r, primary, secondOp, "rekey", hash)

	// Force-expire the row.
	_, err := testPool.Exec(context.Background(),
		`UPDATE admin_approvals SET expires_at = now() - interval '1 minute' WHERE request_id = $1`,
		a.RequestID[:])
	require.NoError(t, err)

	_, err = r.GetByOpArgsHash(context.Background(), "rekey", hash, other)
	require.Error(t, err)
	assertCode(t, err, "APPROVAL_NOT_FOUND")
}

// TestINV_F17_GetByOpArgsHashFiltersOwnAuthor verifies that GetByOpArgsHash
// returns APPROVAL_NOT_FOUND when primary_player_id == excludePlayerID.
func TestINV_F17_GetByOpArgsHashFiltersOwnAuthor(t *testing.T) {
	r := approval.NewPostgresRepo(testPool, nil)
	primary := ulid.Make().String()
	secondOp := ulid.Make().String()
	insertPlayerForApproval(t, primary)
	insertPlayerForApproval(t, secondOp)

	hash := []byte("hash-self-author")
	openAndApprove(t, r, primary, secondOp, "rekey", hash)

	// Exclude the primary author — should be filtered.
	_, err := r.GetByOpArgsHash(context.Background(), "rekey", hash, primary)
	require.Error(t, err)
	assertCode(t, err, "APPROVAL_NOT_FOUND")
}

// TestINV_F17_GetByOpArgsHashMatrix_FiltersUnapproved verifies that
// GetByOpArgsHash returns APPROVAL_NOT_FOUND for rows where approved_at IS NULL.
func TestINV_F17_GetByOpArgsHashMatrix_FiltersUnapproved(t *testing.T) {
	r := approval.NewPostgresRepo(testPool, nil)
	primary := ulid.Make().String()
	other := ulid.Make().String()
	insertPlayerForApproval(t, primary)

	hash := []byte("hash-unapproved")
	// Open without approving — approved_at stays NULL.
	_, err := r.Open(context.Background(), approval.OpenRequest{
		PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: hash,
	})
	require.NoError(t, err)

	_, err = r.GetByOpArgsHash(context.Background(), "rekey", hash, other)
	require.Error(t, err)
	assertCode(t, err, "APPROVAL_NOT_FOUND")
}

// TestINV_F17_GetByOpArgsHashMatrix_TiebreakerMostRecent verifies that when
// two approved rows match, GetByOpArgsHash returns the most-recently-approved.
func TestINV_F17_GetByOpArgsHashMatrix_TiebreakerMostRecent(t *testing.T) {
	r := approval.NewPostgresRepo(testPool, nil)
	primary1 := ulid.Make().String()
	primary2 := ulid.Make().String()
	secondOp1 := ulid.Make().String()
	secondOp2 := ulid.Make().String()
	other := ulid.Make().String()
	insertPlayerForApproval(t, primary1)
	insertPlayerForApproval(t, primary2)
	insertPlayerForApproval(t, secondOp1)
	insertPlayerForApproval(t, secondOp2)

	hash := []byte("hash-tiebreaker")

	// Insert first approval and capture its request_id.
	a1 := openAndApprove(t, r, primary1, secondOp1, "rekey", hash)

	// Insert second approval with a later approved_at.
	a2 := openAndApprove(t, r, primary2, secondOp2, "rekey", hash)

	// Force a1.approved_at to be clearly earlier so the ORDER BY is deterministic.
	_, err := testPool.Exec(context.Background(),
		`UPDATE admin_approvals SET approved_at = now() - interval '10 minutes' WHERE request_id = $1`,
		a1.RequestID[:])
	require.NoError(t, err)

	got, err := r.GetByOpArgsHash(context.Background(), "rekey", hash, other)
	require.NoError(t, err)
	// Should return the row approved most recently (a2).
	assert.Equal(t, a2.PrimaryPlayerID, got.PrimaryPlayerID,
		"GetByOpArgsHash must return the most-recently-approved matching row")
}
