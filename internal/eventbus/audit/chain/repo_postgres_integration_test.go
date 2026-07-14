// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package chain_test

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/test/testutil"
)

// newIntegrationPool opens a pgxpool against a fresh migrated test database
// and registers cleanup. Each test gets its own isolated database so rows
// from one test cannot interfere with another.
func newIntegrationPool() *pgxpool.Pool {
	shared := testutil.SharedPostgres(suiteT)
	connStr := testutil.FreshDatabase(suiteT, shared)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, connStr)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(pool.Close)
	return pool
}

// insertAuditEntry inserts a minimal events_audit row with the given js_seq,
// subject, and JSON payload. The envelope column carries the raw payload bytes
// (identity codec: no proto wrapping for these synthetic test rows).
func insertAuditEntry(pool *pgxpool.Pool, jsSeq int64, subject, payloadJSON string) {
	// Use a deterministic fake event ID derived from jsSeq. It is NOT a ULID, so
	// event_ms (the 000052 partition key) is derived from the row's OWN store-time
	// (now), landing it in the current-month partition.
	fakeID := make([]byte, 16)
	fakeID[0] = byte(jsSeq >> 8)
	fakeID[1] = byte(jsSeq)
	ts := pgnanos.From(time.Now())
	_, err := pool.Exec(context.Background(), `
		INSERT INTO events_audit
			(id, subject, type, timestamp, actor_kind, actor_id,
			 envelope, schema_ver, codec, js_seq, rendering, event_ms)
		VALUES
			($1, $2, 'test.chain.event', $5, 'system', NULL,
			 $3, 1, 'identity', $4, '{}'::jsonb, $6)
	`, fakeID, subject, []byte(payloadJSON), jsSeq, ts, ts)
	Expect(err).NotTo(HaveOccurred())
}

var _ = Describe("Repo_ChainInitialized_RoundTrip", func() {
	It("verifies the full read-mark-reread lifecycle of the chain-initialized signal in bootstrap_metadata", func() {
		pool := newIntegrationPool()
		repo := chain.NewPostgresRepo(pool)
		ctx := context.Background()

		// No row yet — must return false.
		initialized, err := repo.ChainInitialized(ctx, "test.chain", "scope1")
		Expect(err).NotTo(HaveOccurred())
		Expect(initialized).To(BeFalse(), "fresh (chain_name, scope_key) must not be initialized")

		// Mark initialized.
		Expect(repo.MarkChainInitialized(ctx, "test.chain", "scope1")).To(Succeed())

		// Now it must be true.
		initialized, err = repo.ChainInitialized(ctx, "test.chain", "scope1")
		Expect(err).NotTo(HaveOccurred())
		Expect(initialized).To(BeTrue(), "after MarkChainInitialized the signal must be present")

		// Idempotent re-mark must not return an error.
		Expect(repo.MarkChainInitialized(ctx, "test.chain", "scope1")).To(Succeed(),
			"MarkChainInitialized must be idempotent (ON CONFLICT DO NOTHING)")

		// Distinct (chain_name, scope_key) pairs are independent.
		initialized2, err := repo.ChainInitialized(ctx, "test.chain", "scope2")
		Expect(err).NotTo(HaveOccurred())
		Expect(initialized2).To(BeFalse(), "a different scope must not inherit the initialized flag")
	})
})

var _ = Describe("Repo_LoadEntriesByScope_OrdersByJSSeq", func() {
	It("returns entries for the given subject in ascending js_seq order and excludes other scopes", func() {
		pool := newIntegrationPool()
		repo := chain.NewPostgresRepo(pool)
		ctx := context.Background()

		subjectPrefix := "events.g1.system.example"

		// Insert three rows for scopeA in reverse seq order.
		insertAuditEntry(pool, 100, subjectPrefix+".scopeA", `{"scope":"scopeA","label":"third"}`)
		insertAuditEntry(pool, 50, subjectPrefix+".scopeA", `{"scope":"scopeA","label":"first"}`)
		insertAuditEntry(pool, 75, subjectPrefix+".scopeA", `{"scope":"scopeA","label":"second"}`)

		// Row for a different scope — must not appear in scopeA results.
		insertAuditEntry(pool, 60, subjectPrefix+".scopeB", `{"scope":"scopeB"}`)

		entries, err := repo.LoadEntriesByScope(ctx, subjectPrefix, "scopeA")
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(3), "exactly three rows for scopeA must be returned")

		// Must be ordered by js_seq ASC.
		Expect(entries[0].JSSeq).To(Equal(int64(50)), "first entry must have the lowest js_seq")
		Expect(entries[1].JSSeq).To(Equal(int64(75)), "second entry must be next by js_seq")
		Expect(entries[2].JSSeq).To(Equal(int64(100)), "third entry must have the highest js_seq")

		// Subject field on each entry must match.
		for _, e := range entries {
			Expect(e.Subject).To(Equal(subjectPrefix + ".scopeA"))
		}

		// Payload field must be non-nil.
		Expect(entries[0].Payload).NotTo(BeEmpty())
	})
})

var _ = Describe("Repo_DiscoverScopes_DistinctFromSubject", func() {
	It("returns one scope per distinct subject suffix, deduplicating multiple rows under the same subject", func() {
		pool := newIntegrationPool()
		repo := chain.NewPostgresRepo(pool)
		ctx := context.Background()

		subjectPrefix := "events.g2.system.example"

		// Two rows for scope "a", one for scope "b".
		insertAuditEntry(pool, 10, subjectPrefix+".a", `{"scope":"a"}`)
		insertAuditEntry(pool, 11, subjectPrefix+".a", `{"scope":"a"}`)
		insertAuditEntry(pool, 12, subjectPrefix+".b", `{"scope":"b"}`)

		// Row with a different prefix — must not appear.
		insertAuditEntry(pool, 13, "events.g2.system.other.c", `{"scope":"c"}`)

		scopes, err := repo.DiscoverScopes(ctx, subjectPrefix)
		Expect(err).NotTo(HaveOccurred())
		Expect(scopes).To(ConsistOf("a", "b"),
			"DiscoverScopes must return distinct scope suffixes for the given subjectPrefix")
	})
})
