// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/testsupport/chartest"
	"github.com/holomush/holomush/test/testutil"
)

// This spec drives the REAL cobra command tree from NewRootCmd() against a real
// Postgres container, and it lives HERE — in `package main` — rather than in
// test/integration/charname, because `cmd/holomush` is package main and no
// external test package can import it.
//
// That placement is the whole point. A spec that reaches for charname.Gate.Admit
// and CharacterRepository.Rename directly passes while
// `holomush character name set` is unregistered, mis-parsed, or broken in its
// exit-code mapping — every layer between an operator and the write. Driving
// NewRootCmd() with SetArgs exercises all of them.

// characterNameCLIEnv is one spec's fixture: a freshly migrated database, a
// query pool, and the DATABASE_URL the command reads.
type characterNameCLIEnv struct {
	connStr string
	pool    *pgxpool.Pool
	ctx     context.Context //nolint:containedctx // one per spec, torn down in DeferCleanup
}

// newCharacterNameCLIEnv brings up a fresh database with the whole migration
// chain applied — including 000055's backfill and 000056's UNIQUE index — and
// points DATABASE_URL at it.
func newCharacterNameCLIEnv(t testing.TB) *characterNameCLIEnv {
	GinkgoHelper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	DeferCleanup(cancel)

	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)

	pool, err := pgxpool.New(ctx, connStr)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(pool.Close)

	GinkgoT().Setenv("DATABASE_URL", connStr)

	return &characterNameCLIEnv{connStr: connStr, pool: pool, ctx: ctx}
}

// seedCharacter inserts a player and character by direct SQL, supplying the
// identity columns 000056 requires.
func (e *characterNameCLIEnv) seedCharacter(name string) ulid.ULID {
	GinkgoHelper()
	playerID := ulid.Make()
	_, err := e.pool.Exec(e.ctx,
		`INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'hash')`,
		playerID.String(), "cli_"+playerID.String())
	Expect(err).NotTo(HaveOccurred())

	charID := ulid.Make()
	_, err = e.pool.Exec(e.ctx, `
		INSERT INTO characters (id, player_id, name, normalized_name, name_skeleton, name_skeleton_unicode_version)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		append([]any{charID.String(), playerID.String(), name}, chartest.Columns(name)...)...)
	Expect(err).NotTo(HaveOccurred())
	return charID
}

// storedName reads a character's display name straight from the table.
func (e *characterNameCLIEnv) storedName(id ulid.ULID) string {
	GinkgoHelper()
	var name string
	Expect(e.pool.QueryRow(e.ctx,
		`SELECT name FROM characters WHERE id = $1`, id.String()).Scan(&name)).To(Succeed())
	return name
}

// outboxRowCount reports how many outbox envelopes name the given aggregate.
func (e *characterNameCLIEnv) outboxRowCount(id ulid.ULID) int {
	GinkgoHelper()
	var n int
	Expect(e.pool.QueryRow(e.ctx,
		`SELECT count(*) FROM outbox WHERE aggregate_id = $1`, id.String()).Scan(&n)).To(Succeed())
	return n
}

// runHolomush drives the production command tree and returns its error plus
// everything it printed.
func runHolomush(args ...string) (string, error) {
	GinkgoHelper()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

var _ = Describe("holomush character name (operator resolution CLI)", func() {
	Describe("character name set", func() {
		It("renames a character through the real command tree and leaves an outbox envelope", func() {
			env := newCharacterNameCLIEnv(adminAuthSuiteT)
			charID := env.seedCharacter("Cocoa")
			before := env.outboxRowCount(charID)

			out, err := runHolomush("character", "name", "set", charID.String(), "Brenna")
			Expect(err).NotTo(HaveOccurred(), "output: %s", out)
			Expect(out).To(ContainSubstring("renamed"))
			Expect(env.storedName(charID)).To(Equal("Brenna"))

			// INV-WORLD-4's envelope-atomicity property for this THIRD
			// out-of-world writer: the CLI discards Rename's returned delta, so
			// the only thing that can produce a feed entry is Rename emitting
			// the envelope inside its own transaction.
			Expect(env.outboxRowCount(charID)).To(Equal(before+1),
				"the operator rename must commit an outbox envelope alongside the row")
		})

		It("refuses a replacement the gate rejects, exits non-zero, and writes nothing", func() {
			env := newCharacterNameCLIEnv(adminAuthSuiteT)
			charID := env.seedCharacter("Cocoa")

			// A digit-bearing replacement: refused by Gate.Admit alone, because
			// Gate.Check subsumes world.ValidateCharacterName. No second
			// validation call exists in the command.
			_, err := runHolomush("character", "name", "set", charID.String(), "Brenna2")
			Expect(err).To(HaveOccurred())
			Expect(env.storedName(charID)).To(Equal("Cocoa"), "a refused name must not be written")
			Expect(env.outboxRowCount(charID)).To(Equal(0), "a refused name must emit no envelope")

			// Paired positive control on the SAME fixture, so the refusal above
			// cannot pass because the command is broken.
			out, err := runHolomush("character", "name", "set", charID.String(), "Brenna")
			Expect(err).NotTo(HaveOccurred(), "output: %s", out)
			Expect(env.storedName(charID)).To(Equal("Brenna"))
		})

		It("refuses a replacement that collides with another character's skeleton", func() {
			env := newCharacterNameCLIEnv(adminAuthSuiteT)
			env.seedCharacter("Cocoa")
			other := env.seedCharacter("Brenna")

			_, err := runHolomush("character", "name", "set", other.String(), "Cocoa")
			Expect(err).To(HaveOccurred(), "an existing name must not be seatable on a second character")
			Expect(env.storedName(other)).To(Equal("Brenna"))
		})
	})

	Describe("character name duplicates", func() {
		It("reports a clean corpus and writes nothing", func() {
			env := newCharacterNameCLIEnv(adminAuthSuiteT)
			charID := env.seedCharacter("Cocoa")

			out, err := runHolomush("character", "name", "duplicates")
			Expect(err).NotTo(HaveOccurred(), "output: %s", out)
			Expect(out).To(ContainSubstring("No character-name collisions found"))
			Expect(env.storedName(charID)).To(Equal("Cocoa"))
		})

		It("rolls its detection transaction back rather than half-applying migration 000055", func() {
			env := newCharacterNameCLIEnv(adminAuthSuiteT)

			// Stage a row in the PRE-backfill shape the detection is written
			// for: 000056 constrains normalized_name, so the constraint is
			// relaxed for the duration of this spec exactly as the migration
			// chain leaves it between 000055 and 000056.
			_, err := env.pool.Exec(env.ctx, `DROP INDEX IF EXISTS characters_normalized_name_key`)
			Expect(err).NotTo(HaveOccurred())
			_, err = env.pool.Exec(env.ctx,
				`ALTER TABLE characters ALTER COLUMN normalized_name DROP NOT NULL`)
			Expect(err).NotTo(HaveOccurred())

			playerID := ulid.Make()
			_, err = env.pool.Exec(env.ctx,
				`INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'hash')`,
				playerID.String(), "cli_"+playerID.String())
			Expect(err).NotTo(HaveOccurred())
			rawID := ulid.Make()
			_, err = env.pool.Exec(env.ctx,
				`INSERT INTO characters (id, player_id, name) VALUES ($1, $2, 'Cordelia')`,
				rawID.String(), playerID.String())
			Expect(err).NotTo(HaveOccurred())

			out, cliErr := runHolomush("character", "name", "duplicates")
			Expect(cliErr).NotTo(HaveOccurred(), "output: %s", out)

			// The report is read-only: the row it scanned is STILL unbackfilled.
			// Committing here would half-apply 000055 outside a migration.
			var stillNull bool
			Expect(env.pool.QueryRow(env.ctx,
				`SELECT normalized_name IS NULL FROM characters WHERE id = $1`,
				rawID.String()).Scan(&stillNull)).To(Succeed())
			Expect(stillNull).To(BeTrue(),
				"the duplicates report must roll its transaction back, leaving the corpus untouched")
		})
	})
})
