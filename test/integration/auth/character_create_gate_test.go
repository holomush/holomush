// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package auth_test

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
	authpg "github.com/holomush/holomush/internal/auth/postgres"
	bootstrapsetup "github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/charname/blocklist"
	"github.com/holomush/holomush/internal/naming"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/telnet"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/test/testutil"
)

// These specs prove the PRODUCTION create path is gated.
//
// That distinction is the whole point of plan 02-06's Theme-A blocker: before
// this plan, charname.Gate compiled, passed every unit test, and was constructed
// at NO composition root — so a name submitted through CharacterService.Create
// reached no confusable check and no block list. A spec that built its own gate
// and called Check would have been green throughout. These drive
// CharacterService and GuestService instead.

// gateTestEnv is the production create path, assembled the way the composition
// roots assemble it.
type gateTestEnv struct {
	pool       *pgxpool.Pool
	characters *auth.CharacterService
	guests     *auth.GuestService
	startLoc   ulid.ULID
}

// gatePool returns a fresh, fully migrated database pool for one gate spec.
//
// The database is provisioned through suiteT because testutil takes a
// testing.TB and Ginkgo's GinkgoT() does not satisfy that interface; the pool
// itself is closed per-spec via DeferCleanup. This is the same suiteT pattern
// every other database-backed Ginkgo suite in test/integration uses.
func gatePool() *pgxpool.Pool {
	GinkgoHelper()
	shared := testutil.SharedPostgres(suiteT)
	connStr := testutil.FreshDatabase(suiteT, shared)
	pool, err := pgxpool.New(context.Background(), connStr)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(pool.Close)
	return pool
}

// expectErrorCode is the Gomega twin of errutil.AssertErrorCode, which cannot be
// used here: it takes a testing.TB, and GinkgoT() does not satisfy that
// interface. It asserts the TOP-LEVEL oops code via oops.AsOops(err).Code()
// rather than chain-walking with errors.Is — the same strength
// errutil.AssertErrorCode carries, and the strength these specs need, since a
// double-wrapped INTERNAL around NAME_CONFUSABLE must NOT satisfy them.
func expectErrorCode(err error, code string) {
	GinkgoHelper()
	oopsErr, ok := oops.AsOops(err)
	Expect(ok).To(BeTrue(), "expected an oops error, got %T", err)
	Expect(oopsErr.Code()).To(Equal(code))
}

// newGateTestEnv builds a CharacterService and a GuestService over one pool,
// sharing ONE charname.Gate — the same shape bootstrapsetup and cmd/holomush
// build.
//
// namerFor optionally replaces the production gemstone/element namer. It is a
// factory rather than a value because the namer needs the start location, which
// this function mints. Omit it to get the production namer.
func newGateTestEnv(
	ctx context.Context,
	blockPatterns []string,
	namerFor ...func(startLoc ulid.ULID) auth.GuestNamer,
) *gateTestEnv {
	GinkgoHelper()
	Expect(len(namerFor)).To(BeNumerically("<=", 1), "at most one namer factory")
	pool := gatePool()

	// A verifiable corpus. 000001_baseline seeds a bootstrap character with no
	// name_skeleton, and the gate correctly refuses to adjudicate against a
	// corpus it cannot verify (D-30) — so without this every spec below would
	// fail NAME_SKELETON_UNVERIFIABLE and prove nothing about the gate. The
	// warmup Admit is the proof the repair worked.
	backfillSuiteSkeletons(ctx, pool)
	_, warmupErr := (&charname.Gate{Skeletons: worldpg.NewSkeletonLookup(pool)}).
		Admit(ctx, "Corpus Warmup")
	Expect(warmupErr).NotTo(HaveOccurred(), "the repaired corpus must be adjudicable")

	charRepo := worldpg.NewCharacterRepository(pool)
	locRepo := worldpg.NewLocationRepository(pool)

	startLoc := ulid.Make()
	_, err := pool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Gate Test Start', '', 'persistent', 'last:0', (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
	`, startLoc.String())
	Expect(err).NotTo(HaveOccurred())

	// The block list arrives as a compiled Snapshot rather than the production
	// Cache: both satisfy charname.BlockList, and the reload behaviour the Cache
	// exists for is 02-05's to assert, not this spec's.
	snapshot, err := blocklist.Compile(blockPatterns)
	Expect(err).NotTo(HaveOccurred())
	gate := &charname.Gate{
		Skeletons: worldpg.NewSkeletonLookup(pool),
		BlockList: snapshot,
	}

	transactor := worldpg.NewTransactor(pool)
	bindingRepo := worldpg.NewBindingRepository(pool)
	genesis, err := auth.NewCharacterGenesisService(
		charRepo, transactor, bindingRepo,
		worldpg.NewOutboxStore(pool), worldpg.NewReapingGuard(pool),
	)
	Expect(err).NotTo(HaveOccurred())

	characters, err := auth.NewCharacterService(
		bootstrapsetup.NewCharRepoAdapter(pool, charRepo),
		bootstrapsetup.NewLocRepoAdapter(&startLoc, locRepo),
		genesis,
		gate,
	)
	Expect(err).NotTo(HaveOccurred())

	playerRepo := authpg.NewPlayerRepository(pool)
	reaping, err := auth.NewCharacterReapingService(
		charRepo, charRepo,
		worldpg.NewPropertyRepository(pool),
		bindingRepo, transactor,
		worldpg.NewOutboxStore(pool),
		playerRepo, playerRepo,
	)
	Expect(err).NotTo(HaveOccurred())

	var namer auth.GuestNamer = telnet.NewGuestAuthenticator(naming.NewGemstoneElementTheme(), startLoc)
	if len(namerFor) == 1 {
		namer = namerFor[0](startLoc)
	}

	guests, err := auth.NewGuestService(
		namer,
		playerRepo,
		&gateGuestCharRepo{pool: pool},
		store.NewPostgresPlayerSessionStore(pool),
		genesis,
		reaping,
		gate,
	)
	Expect(err).NotTo(HaveOccurred())

	return &gateTestEnv{pool: pool, characters: characters, guests: guests, startLoc: startLoc}
}

// gateGuestCharRepo is the auth.GuestCharacterRepository the guest path reads
// through. It carries the SAME predicate every other site carries; the
// transitional NULL branch was removed with migration 000056.
type gateGuestCharRepo struct{ pool *pgxpool.Pool }

func (r *gateGuestCharRepo) ExistsByNormalizedName(ctx context.Context, key string, excluding *ulid.ULID) (bool, error) {
	var excludingArg *string
	if excluding != nil {
		s := excluding.String()
		excludingArg = &s
	}
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM characters
		WHERE normalized_name = $1
		  AND ($2::text IS NULL OR id::text <> $2)
	)`, key, excludingArg).Scan(&exists)
	return exists, err
}

func (e *gateTestEnv) seedPlayer(ctx context.Context) ulid.ULID {
	GinkgoHelper()
	playerID := ulid.Make()
	_, err := e.pool.Exec(ctx,
		`INSERT INTO players (id, username, password_hash) VALUES ($1, $2, '')`,
		playerID.String(), "gate_"+playerID.String())
	Expect(err).NotTo(HaveOccurred())
	return playerID
}

// scriptedGuestNamer hands out a FIXED sequence of guest names and records
// every name the service asked for and released.
//
// The production gemstone/element namer picks at random, which makes any
// retry-loop assertion built on it vacuous: if the first draw happens to be
// admissible the loop never runs, and a spec asserting only "the name that came
// back is admissible" passes without exercising the thing it names. Scripting
// the sequence makes the retry mandatory, and recording the requests is what
// proves it actually happened.
//
// Running out of names is deliberately an ERROR rather than a wrap-around: it
// turns "the service asked for more names than the script allows" into a loud
// GUEST_NAME_GENERATE_FAILED instead of a silent extra lap.
type scriptedGuestNamer struct {
	names    []string
	startLoc ulid.ULID

	mu        sync.Mutex
	requested []string
	released  []string
}

func (n *scriptedGuestNamer) GenerateName() (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.requested) >= len(n.names) {
		return "", oops.Errorf("scriptedGuestNamer: asked for name %d, script holds only %d",
			len(n.requested)+1, len(n.names))
	}
	name := n.names[len(n.requested)]
	n.requested = append(n.requested, name)
	return name, nil
}

func (n *scriptedGuestNamer) ReleaseGuest(name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.released = append(n.released, name)
}

func (n *scriptedGuestNamer) StartLocation() ulid.ULID { return n.startLoc }

func (n *scriptedGuestNamer) requestedNames() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.requested...)
}

func (n *scriptedGuestNamer) releasedNames() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.released...)
}

// compile-time assertions: the spec's stubs really are what the services take.
var (
	_ auth.GuestCharacterRepository = (*gateGuestCharRepo)(nil)
	_ auth.GuestNamer               = (*scriptedGuestNamer)(nil)
)

var _ = Describe("The production character-create path admits names through charname.Gate", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("rejects a confusable name submitted through CharacterService.Create", func() {
		env := newGateTestEnv(ctx, nil)
		playerID := env.seedPlayer(ctx)

		// Paired positive control FIRST, on the same fixture: an ordinary name
		// succeeds, so the refusal below cannot pass because creation is broken.
		seeded, err := env.characters.Create(ctx, playerID, "cocoa")
		Expect(err).NotTo(HaveOccurred())
		Expect(seeded).NotTo(BeNil())
		Expect(seeded.Name).To(Equal("cocoa"), "§6.1.5: the player's own capitalization is preserved")

		// A whole-script Cyrillic homoglyph of the seeded name.
		_, err = env.characters.Create(ctx, env.seedPlayer(ctx), "сосоа")
		Expect(err).To(HaveOccurred())
		expectErrorCode(err, "NAME_CONFUSABLE")
	})

	// Proves plan 02-05's matcher actually reaches the gate the create path uses.
	It("rejects a block-listed name and still admits one the pattern does not match", func() {
		env := newGateTestEnv(ctx, []string{`^admin$`})
		playerID := env.seedPlayer(ctx)

		_, err := env.characters.Create(ctx, playerID, "Admin")
		Expect(err).To(HaveOccurred(),
			"the block list matches the case-folded KEY, so 'Admin' must be refused")
		expectErrorCode(err, "NAME_BLOCKED")

		// Paired success on the same fixture.
		ok, err := env.characters.Create(ctx, playerID, "administrative assistant")
		Expect(err).NotTo(HaveOccurred(), "a name the pattern does not match must still be creatable")
		Expect(ok).NotTo(BeNil())
	})

	// Proves the gate's NAME_INVALID_SYNTAX is mapped, not leaked: the
	// caller-visible contract for a digit-bearing name is unchanged.
	It("maps a syntactic refusal to the pre-existing CHARACTER_INVALID_NAME code", func() {
		env := newGateTestEnv(ctx, nil)

		_, err := env.characters.Create(ctx, env.seedPlayer(ctx), "Alaric2")
		Expect(err).To(HaveOccurred())
		expectErrorCode(err, "CHARACTER_INVALID_NAME")
	})

	// The guest twin. The guest path never ran the name pipeline at all before
	// this plan (T-02-31), so a gate installed only in CharacterService would
	// have left the automatic, high-volume path wide open.
	It("retries guest provisioning rather than persisting a block-listed generated name", func() {
		// The namer is SCRIPTED: a blocked candidate first, an admissible one
		// second, so the retry loop is forced to run.
		//
		// Driving this from the production gemstone/element namer made the spec
		// vacuous. It picks at random, `^[aeiou]` blocks only vowel-initial
		// names, and roughly 21 of 26 first letters are consonants — so most
		// runs were admitted on attempt ONE, never entered the retry loop, and
		// still satisfied "the name that came back is not vowel-initial". A
		// green run proved nothing about retrying.
		blocked, admissible := "Amber_Ember", "Jade_River"
		namer := &scriptedGuestNamer{names: []string{blocked, admissible}}
		env := newGateTestEnv(ctx, []string{`^[aeiou]`}, func(startLoc ulid.ULID) auth.GuestNamer {
			namer.startLoc = startLoc
			return namer
		})

		result, err := env.guests.CreateGuest(ctx)
		Expect(err).NotTo(HaveOccurred(), "the retry loop must find an admissible name")
		Expect(result).NotTo(BeNil())
		Expect(result.Character).NotTo(BeNil())

		// The name that landed is the SECOND candidate — the namer's underscores
		// become spaces on the way to the display form.
		Expect(result.Character.Name).To(Equal("Jade River"))

		// It does NOT match the configured pattern.
		normalized, nErr := charname.Normalize(result.Character.Name)
		Expect(nErr).NotTo(HaveOccurred())
		Expect("aeiou").NotTo(ContainSubstring(string(normalized.Key[0])),
			"a generated name matching a configured block pattern must be retried, never persisted")

		// And the stored row carries its identity columns, because it went through
		// the gated writer.
		var key, skeleton, version string
		Expect(env.pool.QueryRow(ctx, `
			SELECT normalized_name, name_skeleton, name_skeleton_unicode_version
			FROM characters WHERE id = $1
		`, result.Character.ID.String()).Scan(&key, &skeleton, &version)).To(Succeed())
		Expect(key).To(Equal(normalized.Key))
		Expect(skeleton).NotTo(BeEmpty())
		Expect(version).To(Equal(charname.UnicodeVersion))

		// The blocked candidate never reached the database — the "never
		// persisted" half of this spec's name.
		var blockedRows int
		Expect(env.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM characters WHERE normalized_name = $1`, "amber ember").
			Scan(&blockedRows)).To(Succeed())
		Expect(blockedRows).To(BeZero(), "the blocked candidate must never be persisted")

		// THE load-bearing assertion. Everything above is also satisfied by a
		// service that never retried and simply drew an admissible name first —
		// which is exactly how this spec used to pass while proving nothing.
		// Asserting the service asked for BOTH names, in order, is what pins the
		// retry itself.
		Expect(namer.requestedNames()).To(Equal([]string{blocked, admissible}),
			"the service must request the blocked candidate, have it refused, then request another")
		Expect(namer.releasedNames()).To(ContainElement(blocked),
			"a refused candidate must be released back to the namer")
	})

	// Pairs the refusal with a populated config that succeeds, on the same fixture.
	It("refuses to construct a gate with a nil block-list subsystem", func() {
		pool := gatePool()

		gate, err := bootstrapsetup.NewCharacterNameGate(pool, nil)
		Expect(err).To(HaveOccurred(), "a production root must not build a gate with no block list")
		Expect(gate).To(BeNil())
		expectErrorCode(err, "CHARACTER_NAME_GATE_MISCONFIGURED")

		gate, err = bootstrapsetup.NewCharacterNameGate(pool, blocklist.NewSubsystem(blocklist.SubsystemConfig{
			Source: func() blocklist.Source { return nil },
		}))
		Expect(err).NotTo(HaveOccurred())
		Expect(gate).NotTo(BeNil())
		Expect(gate.BlockList).NotTo(BeNil(), "the gate must carry the subsystem's live matcher")
		Expect(gate.Skeletons).NotTo(BeNil())
	})

	// The durable guard for the regression PR #4941's E2E run caught.
	//
	// # The defect
	//
	// An EXACT duplicate has an identical skeleton, so charname.Gate.Check step 5
	// intercepted it and returned NAME_CONFUSABLE — which internal/grpc's
	// sanitizeAuthError had no case for, so the web client rendered the generic
	// "request failed" where it had always shown "already taken". The friendly
	// uniqueness pre-check that exists precisely for this case sat AFTER the gate
	// and was unreachable for it.
	//
	// # Why the three are asserted on ONE fixture
	//
	// A fix that simply reported "already taken" for every skeleton collision
	// would satisfy the first two and be WRONG: a name confusable with a
	// DIFFERENT player's character would then assert something untrue and
	// disclose more about the corpus than §6.1.2 intends. The third is what
	// forbids that shape — so all three share one seeded corpus and one spec.
	It("reports an exact duplicate as taken, and a merely confusable name as confusable", func() {
		env := newGateTestEnv(ctx, nil)

		seeded, err := env.characters.Create(ctx, env.seedPlayer(ctx), "cocoa")
		Expect(err).NotTo(HaveOccurred())
		Expect(seeded).NotTo(BeNil())

		By("an exact duplicate is CHARACTER_NAME_TAKEN")
		_, err = env.characters.Create(ctx, env.seedPlayer(ctx), "cocoa")
		Expect(err).To(HaveOccurred())
		expectErrorCode(err, "CHARACTER_NAME_TAKEN")

		By("a case variant of an existing name is CHARACTER_NAME_TAKEN too")
		// Same §6.1.1 uniqueness key, different display form: still the same
		// name, so still "already taken" rather than "too similar".
		_, err = env.characters.Create(ctx, env.seedPlayer(ctx), "COCOA")
		Expect(err).To(HaveOccurred())
		expectErrorCode(err, "CHARACTER_NAME_TAKEN")

		By("a DIFFERENT but confusable name is still NAME_CONFUSABLE")
		// A whole-script Cyrillic homoglyph: a genuinely different name whose
		// normalized key differs, so claiming it was "already taken" would be
		// false. This control is what stops the fix collapsing the two verdicts.
		_, err = env.characters.Create(ctx, env.seedPlayer(ctx), "сосоа")
		Expect(err).To(HaveOccurred())
		expectErrorCode(err, "NAME_CONFUSABLE")
	})
})
