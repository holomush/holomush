//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver, mirroring goose's own connection shape
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/pressly/goose/v3"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
	bootstrapsetup "github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/charname/blocklist"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/testsupport/chartest"
	"github.com/holomush/holomush/internal/world"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/test/testutil"
)

// ---------------------------------------------------------------------------
// Schema staging
// ---------------------------------------------------------------------------

// migrationsSourceDir is internal/store/migrations, reached from this package's
// directory. A Go test runs with its working directory set to the package
// directory, so the relative walk is stable.
const migrationsSourceDir = "../../../internal/store/migrations"

// stagedVersion55 is the schema this suite's negative controls run against:
// every migration EXCEPT 000056's UNIQUE index.
//
// It is version 55, not 53. Staging at 53 would be NON-DIAGNOSTIC: after plan
// 02-06 the create path binds normalized_name, name_skeleton and
// name_skeleton_unicode_version, and those columns arrive in 000054 — so a
// create against 53 fails on MISSING COLUMNS, which is evidence about a schema
// mismatch rather than about an absent index, and is indistinguishable from the
// outside from the demonstration it is supposed to be.
const stagedVersion55 = 55

// stageSchemaWithoutUniqueIndex brings a blank database up to version 55 — the
// whole chain except 000056 — and returns its connection string.
//
// # The staging recipe, and the one option that must NOT be used
//
// 000055 is a REGISTERED GO MIGRATION. It has no `.sql` file and
// `//go:embed migrations/*.sql` never sees it; it reaches a provider through
// goose's GLOBAL REGISTRY, via the init() in
// internal/store/migrations/000055_backfill_character_normalized_names.go, which
// runs because internal/store blank-imports that package.
//
// So goose.WithDisableGlobalRegistry(true) is precisely the option that REMOVES
// it. A run staged that way applies schema 54 and still goes red — for the wrong
// reason, both creates failing on missing columns — which is exactly the
// non-diagnostic failure staging at 53 was corrected for. The in-tree fixture
// migrate_gointerleave_integration_test.go DOES pass that option, but it pairs
// it with WithGoMigrations because its Go migrations are SYNTHETIC and must not
// collide with the real corpus. Neither applies here.
//
// Instead: copy the real `.sql` files 000001–000054 into a temp dir, omit
// 000056, and build the provider over that directory with the global registry
// LEFT ENABLED, so the real registered 000055 is collected by version alongside
// them. That is the production shape — a `.sql`-only filesystem plus registered
// Go migrations — reproduced with one file withheld.
func stageSchemaWithoutUniqueIndex(ctx context.Context, t testing.TB) string {
	GinkgoHelper()

	connStr := testutil.RawDatabase(t, testutil.SharedPostgres(t))

	entries, err := os.ReadDir(migrationsSourceDir)
	Expect(err).NotTo(HaveOccurred(), "read %s", migrationsSourceDir)

	dir := t.TempDir()
	copied := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		// The ONE file withheld. Everything else, including every version above
		// it that might exist later, is copied verbatim.
		if strings.HasPrefix(name, "000056_") {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(migrationsSourceDir, name))
		Expect(readErr).NotTo(HaveOccurred(), "read %s", name)
		Expect(os.WriteFile(filepath.Join(dir, name), body, 0o600)).To(Succeed())
		copied++
	}
	Expect(copied).To(BeNumerically(">", 40),
		"the staged corpus must be the real chain, not an empty directory")

	db, err := sql.Open("pgx", connStr)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = db.Close() }) //nolint:errcheck // best-effort cleanup

	// No WithDisableGlobalRegistry: that option is what would delete 000055.
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(dir))
	Expect(err).NotTo(HaveOccurred())

	_, err = provider.Up(ctx)
	Expect(err).NotTo(HaveOccurred(), "the staged chain must apply cleanly")

	// PRECONDITION, asserted BEFORE any spec body runs against this database and
	// deliberately distinct from the specs' own assertions: a mis-staged run must
	// abort as a broken fixture, not produce a failure that reads like the
	// demonstration.
	var sawGoMigration55 bool
	for _, src := range provider.ListSources() {
		if src.Version == stagedVersion55 && src.Type == goose.TypeGo {
			sawGoMigration55 = true
		}
		Expect(src.Version).NotTo(BeNumerically(">=", 56),
			"000056 must be withheld from the staged corpus; staging recipe: copy the real .sql "+
				"files except 000056 into a temp dir and leave the global registry ENABLED")
	}
	Expect(sawGoMigration55).To(BeTrue(),
		"the provider's collected sources must contain the REGISTERED Go migration 55. If this "+
			"fails, goose's global registry was disabled when the provider was built, or "+
			"internal/store was not imported, and the staged schema is 54 rather than 55 — see "+
			"this file's staging doc comment")

	dbVersion, err := provider.GetDBVersion(ctx)
	Expect(err).NotTo(HaveOccurred())
	Expect(dbVersion).To(BeEquivalentTo(stagedVersion55),
		"the staged database must report version 55")

	return connStr
}

// uniqueIndexExists reports whether 000056's UNIQUE index is present.
func uniqueIndexExists(ctx context.Context, pool *pgxpool.Pool) bool {
	GinkgoHelper()
	var exists bool
	Expect(pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE indexname = 'characters_normalized_name_key')`).
		Scan(&exists)).To(Succeed())
	return exists
}

// ---------------------------------------------------------------------------
// The production create path
// ---------------------------------------------------------------------------

// createPath is auth.CharacterService assembled the way the composition roots
// assemble it, over one pool.
type createPath struct {
	pool     *pgxpool.Pool
	chars    *auth.CharacterService
	startLoc ulid.ULID
}

// newCreatePath wires the PRODUCTION create path against connStr.
//
// It is the production service rather than a hand-rolled INSERT because the
// property under test is what a character CREATE does — the pre-check, the
// serialization guard, the gated writer and the 23505 handler that maps the
// index's answer to CHARACTER_NAME_TAKEN.
func newCreatePath(ctx context.Context, connStr string) *createPath {
	GinkgoHelper()

	pool, err := pgxpool.New(ctx, connStr)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(pool.Close)

	startLoc := ulid.Make()
	_, err = pool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Uniqueness Start', '', 'persistent', 'last:0', (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
	`, startLoc.String())
	Expect(err).NotTo(HaveOccurred())

	snapshot, err := blocklist.Compile(nil)
	Expect(err).NotTo(HaveOccurred())
	gate := &charname.Gate{
		Skeletons: worldpg.NewSkeletonLookup(pool),
		BlockList: snapshot,
	}

	charRepo := worldpg.NewCharacterRepository(pool)
	genesis, err := auth.NewCharacterGenesisService(
		charRepo,
		worldpg.NewTransactor(pool),
		worldpg.NewBindingRepository(pool),
		worldpg.NewOutboxStore(pool),
		worldpg.NewReapingGuard(pool),
	)
	Expect(err).NotTo(HaveOccurred())

	chars, err := auth.NewCharacterService(
		bootstrapsetup.NewCharRepoAdapter(pool, charRepo),
		bootstrapsetup.NewLocRepoAdapter(&startLoc, worldpg.NewLocationRepository(pool)),
		genesis,
		gate,
	)
	Expect(err).NotTo(HaveOccurred())

	return &createPath{pool: pool, chars: chars, startLoc: startLoc}
}

// seedPlayer inserts a player row and returns its id.
func (c *createPath) seedPlayer(ctx context.Context) ulid.ULID {
	GinkgoHelper()
	id := ulid.Make()
	_, err := c.pool.Exec(ctx,
		`INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'hash')`,
		id.String(), "uniq_"+id.String())
	Expect(err).NotTo(HaveOccurred())
	return id
}

// insertRawCharacter inserts a character with its identity columns supplied
// EXPLICITLY, bypassing every application-level check above the database.
//
// This is what lets a spec ask the DATABASE a question. Both the
// ExistsByNormalizedName pre-check and 02-06's advisory-lock skeleton guard sit
// on the create path and are present in BOTH schemas under test here, so a
// duplicate routed through Create never reaches the index and cannot say
// anything about whether it exists.
func (c *createPath) insertRawCharacter(ctx context.Context, display, key string) {
	GinkgoHelper()
	Expect(c.insertRawCharacterErr(ctx, display, key)).NotTo(HaveOccurred())
}

// insertRawCharacterErr is insertRawCharacter's error-returning form, for the
// cases where the write is EXPECTED to be refused.
func (c *createPath) insertRawCharacterErr(ctx context.Context, display, key string) error {
	GinkgoHelper()
	return c.insertRawCharacterWithSkeleton(ctx, display, key, charname.Skeleton(key))
}

// insertRawCharacterWithSkeleton inserts a character whose skeleton is supplied
// independently of its uniqueness key.
//
// Passing an UNRELATED skeleton is how a spec makes charname.Gate's confusable
// step blind to the row. That step runs BEFORE the uniqueness pre-check
// (gate.go's documented order: normalize, syntax, mixed-script, block list,
// skeleton), so an ordinary duplicate is refused NAME_CONFUSABLE and the
// pre-check's own answer is never reached.
func (c *createPath) insertRawCharacterWithSkeleton(ctx context.Context, display, key, skeleton string) error {
	GinkgoHelper()
	playerID := c.seedPlayer(ctx)
	_, err := c.pool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name, normalized_name, name_skeleton, name_skeleton_unicode_version)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		ulid.Make().String(), playerID.String(), display, key,
		skeleton, chartest.IdentityFor(display).UnicodeVersion)
	return err
}

// rowsWithNormalizedName counts characters holding a uniqueness key.
func (c *createPath) rowsWithNormalizedName(ctx context.Context, key string) int {
	GinkgoHelper()
	var n int
	Expect(c.pool.QueryRow(ctx,
		`SELECT count(*) FROM characters WHERE normalized_name = $1`, key).Scan(&n)).To(Succeed())
	return n
}

// codeOfErr returns an error's top-level oops code, or "" when it has none.
func codeOfErr(err error) string {
	if err == nil {
		return ""
	}
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return ""
	}
	code, ok := oopsErr.Code().(string)
	if !ok {
		return ""
	}
	return code
}

var _ = Describe("Character-name uniqueness is decided by the database (IDENT-09)", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 3*time.Minute)
		DeferCleanup(cancel)
	})

	Describe("Two concurrent claims of one name", func() {
		It("commits exactly one, refuses the other, and leaves exactly one row", func() {
			path := newCreatePath(ctx, testutil.FreshDatabase(suiteT, testutil.SharedPostgres(suiteT)))
			Expect(uniqueIndexExists(ctx, path.pool)).To(BeTrue(),
				"this case runs against the FULL chain, index included")

			playerA := path.seedPlayer(ctx)
			playerB := path.seedPlayer(ctx)

			const contested = "Brenna"
			var (
				wg     sync.WaitGroup
				errs   [2]error
				chars  [2]*world.Character
				owners = [2]ulid.ULID{playerA, playerB}
			)
			start := make(chan struct{})
			for i := range owners {
				wg.Add(1)
				go func(i int) {
					defer GinkgoRecover()
					defer wg.Done()
					<-start
					chars[i], errs[i] = path.chars.Create(ctx, owners[i], contested)
				}(i)
			}
			close(start)
			wg.Wait()

			winners := 0
			for i := range errs {
				if errs[i] == nil {
					winners++
					Expect(chars[i]).NotTo(BeNil())
				}
			}
			Expect(winners).To(Equal(1),
				"exactly one concurrent claim of a name may commit; errors: %v / %v", errs[0], errs[1])

			// The loser's refusal is a NAME-COLLISION refusal, not an incidental
			// failure. Which of the two codes it carries depends on which layer
			// caught it: 02-06's transaction-scoped advisory lock on the skeleton
			// (NAME_CONFUSABLE) or 000056's UNIQUE index surfaced through the
			// 23505 handler (CHARACTER_NAME_TAKEN). Both are correct outcomes;
			// asserting only one would make this spec a test of WHICH guard won
			// the race rather than of the guarantee.
			for i := range errs {
				if errs[i] != nil {
					Expect(codeOfErr(errs[i])).To(BeElementOf("NAME_CONFUSABLE", "CHARACTER_NAME_TAKEN"),
						"the losing claim must be refused as a name collision")
				}
			}

			Expect(path.rowsWithNormalizedName(ctx, strings.ToLower(contested))).To(Equal(1),
				"the database must hold exactly one row for the contested key")
		})
	})

	Describe("The UNIQUE index itself, isolated from every check above it", func() {
		// These two cases are a matched pair: the SAME write, run against two
		// schemas. Together they are the rule-4 demonstration — the write is
		// rejected with the index present and ACCEPTED without it, so the index
		// is proven load-bearing rather than assumed to be.
		//
		// They write by direct SQL rather than through CharacterService.Create,
		// and that is the whole point of the pair. Two application-level checks
		// sit above the index and would both catch an ordinary duplicate before
		// it ever reached the database's own answer: the friendly
		// ExistsByNormalizedName pre-check (§6.1.3 calls it a UX affordance,
		// explicitly NOT the guarantee) and 02-06's transaction-scoped advisory
		// lock on the skeleton. A negative control routed through Create would
		// therefore go red at 000055 for the WRONG reason — refused by the
		// pre-check, which is present in both schemas — and would prove nothing
		// about the index. Going straight at the table is what isolates it.

		It("rejects a duplicate uniqueness key at the database against the full chain", func() {
			path := newCreatePath(ctx, testutil.FreshDatabase(suiteT, testutil.SharedPostgres(suiteT)))
			Expect(uniqueIndexExists(ctx, path.pool)).To(BeTrue())

			path.insertRawCharacter(ctx, "Brenna", "brenna")

			err := path.insertRawCharacterErr(ctx, "Ｂｒｅｎｎａ", "brenna")
			Expect(err).To(HaveOccurred(),
				"the database itself must refuse a second row holding one uniqueness key")
			Expect(err.Error()).To(ContainSubstring("23505"))
			Expect(err.Error()).To(ContainSubstring("characters_normalized_name_key"),
				"the refusal must come from 000056's index, not some other constraint")
			Expect(path.rowsWithNormalizedName(ctx, "brenna")).To(Equal(1))

			// Paired positive control on the SAME fixture: a distinct key still
			// inserts, so the rejection above cannot pass because writes are
			// broken.
			Expect(path.insertRawCharacterErr(ctx, "Cordelia", "cordelia")).NotTo(HaveOccurred())
		})

		It("ACCEPTS the very same duplicate against the schema staged at 000055 (the pre-index state)", func() {
			path := newCreatePath(ctx, stageSchemaWithoutUniqueIndex(ctx, suiteT))
			Expect(uniqueIndexExists(ctx, path.pool)).To(BeFalse(),
				"the staged schema is 000055: every migration EXCEPT the unique index")

			path.insertRawCharacter(ctx, "Brenna", "brenna")

			Expect(path.insertRawCharacterErr(ctx, "Ｂｒｅｎｎａ", "brenna")).NotTo(HaveOccurred(),
				"without 000056 nothing refuses a duplicate uniqueness key — this is the pre-fix state")
			Expect(path.rowsWithNormalizedName(ctx, "brenna")).To(Equal(2),
				"TWO rows share the uniqueness key, which is exactly what 000056 exists to prevent")
		})
	})

	Describe("The create path's friendly pre-check", func() {
		It("surfaces an already-taken name as CHARACTER_NAME_TAKEN", func() {
			// The pre-check and the index return the SAME caller-visible code by
			// design (character_service.go's 23505 handler), so a caller cannot
			// tell which layer answered — and does not need to. This asserts the
			// contract; the pair above asserts which layer is load-bearing.
			path := newCreatePath(ctx, testutil.FreshDatabase(suiteT, testutil.SharedPostgres(suiteT)))
			// The seeded row carries a DECOUPLED skeleton so the gate's
			// confusable step — which runs first — cannot see it, and the
			// pre-check is the layer that answers.
			Expect(path.insertRawCharacterWithSkeleton(
				ctx, "Brenna", "brenna", "zzdecoupledskeleton",
			)).NotTo(HaveOccurred())

			_, err := path.chars.Create(ctx, path.seedPlayer(ctx), "Brenna")
			Expect(err).To(HaveOccurred())
			Expect(codeOfErr(err)).To(Equal("CHARACTER_NAME_TAKEN"))

			created, err := path.chars.Create(ctx, path.seedPlayer(ctx), "Cordelia")
			Expect(err).NotTo(HaveOccurred())
			Expect(created).NotTo(BeNil())
		})
	})
})

// storeImportAnchor exists so this file's dependency on internal/store is
// explicit rather than incidental.
//
// internal/store carries the blank import of internal/store/migrations, and
// that import is the ONLY thing that runs migration 000055's init() and puts it
// into goose's global registry. Without it the staging above collects sources
// 1–54 and silently produces schema 54 — the exact mis-staging the precondition
// assertion exists to catch. Naming the dependency here means an import-tidying
// pass has something to see.
var storeImportAnchor = store.MigrationName
