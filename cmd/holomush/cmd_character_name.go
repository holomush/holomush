// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/charname/blocklist"
	"github.com/holomush/holomush/internal/store"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/internal/world/wmodel"
)

// characterNameEnv is everything the `character name` subcommands need: a pool,
// the SAME admission gate the create path uses, and the gated writer.
//
// # Why this command opens its own pool instead of dialling the admin socket
//
// Every other cmd_*.go operator command reaches the running server over the
// admin UDS/Connect surface. That is the wrong transport HERE, for a reason
// specific to this command: migration 000055 ABORTS STARTUP when it finds a
// pre-existing duplicate (D-22). At the exact moment an operator needs
// `character name duplicates` and `character name set`, the server is not
// running — so a command reachable only through the running server would be
// unreachable precisely when it is needed.
//
// cmd/holomush/migrate.go is the in-tree precedent for a direct-database cobra
// command; this follows it for URL sourcing and error handling. The gate is
// constructed through setup.NewCharacterNameGate, the SAME helper all three
// production composition roots call, so the CLI and the create path reject the
// same names — including the operator's block list.
type characterNameEnv struct {
	pool *pgxpool.Pool
	gate *charname.Gate
	repo characterRenamer

	// db is a database/sql handle on the same database. It exists because
	// BackfillCharacterIdentity takes a database/sql executor — its only other
	// caller is the goose Go migration, which runs on a *sql.Tx — so the
	// duplicates report drives the same shape the migration does rather than a
	// pgx-flavoured approximation of it.
	db *sql.DB
}

// characterRenamer is the narrow write surface this command needs, declared
// consumer-side so a unit test can observe whether a refused name reached the
// writer at all. *worldpostgres.CharacterRepository satisfies it.
//
// Rename's only name parameter is a charname.Admitted, whose sole constructor is
// (*charname.Gate).Admit — so "this command cannot write an unadmitted name" is
// a property of the type, not of a test.
type characterRenamer interface {
	Rename(
		ctx context.Context,
		id ulid.ULID,
		name charname.Admitted,
		expectedVersion int,
		intent wmodel.EnvelopeIntent,
	) (*wmodel.MutationDelta, error)
}

// characterNameEnvFactory builds a characterNameEnv and its teardown. It is a
// seam so unit tests can drive the commands without a database.
type characterNameEnvFactory func(ctx context.Context) (*characterNameEnv, func(), error)

// defaultCharacterNameEnvFactory opens a pool from DATABASE_URL and wires the
// gate against it.
func defaultCharacterNameEnvFactory(ctx context.Context) (*characterNameEnv, func(), error) {
	url, err := getDatabaseURL()
	if err != nil {
		return nil, nil, oops.With("command", "character name").Wrap(err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, nil, oops.Code("CHARACTER_NAME_CLI_DB_FAILED").
			With("command", "character name").Wrap(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		pool.Close()
		return nil, nil, oops.Code("CHARACTER_NAME_CLI_DB_FAILED").
			With("command", "character name").Wrap(err)
	}

	settings, err := store.NewPostgresEventStore(ctx, url)
	if err != nil {
		pool.Close()
		_ = db.Close() //nolint:errcheck // cleanup on an error path; the init error takes precedence
		return nil, nil, oops.Code("CHARACTER_NAME_CLI_DB_FAILED").
			With("command", "character name").Wrap(err)
	}

	teardown := func() {
		settings.Close()
		_ = db.Close() //nolint:errcheck // process is exiting; a close error has nowhere useful to go
		pool.Close()
	}

	// The block list is compiled ONCE here rather than polled: this is a
	// one-shot command, and a gate built with a nil block list is refused by
	// NewCharacterNameGate rather than silently accepting every name.
	blockList := blocklist.NewSubsystem(blocklist.SubsystemConfig{
		Source: func() blocklist.Source { return settings },
		Key:    blocklist.DefaultKey,
	})
	if prepErr := blockList.Prepare(ctx); prepErr != nil {
		teardown()
		return nil, nil, oops.Code("CHARACTER_NAME_CLI_BLOCKLIST_FAILED").
			With("command", "character name").Wrap(prepErr)
	}

	gate, err := setup.NewCharacterNameGate(pool, blockList)
	if err != nil {
		teardown()
		return nil, nil, oops.With("command", "character name").Wrap(err)
	}

	return &characterNameEnv{
		pool: pool,
		gate: gate,
		repo: worldpostgres.NewCharacterRepository(pool),
		db:   db,
	}, teardown, nil
}

// NewCharacterCmd returns the `holomush character` parent command.
//
// It MUST be registered on NewRootCmd. A cobra command constructed here and
// never added to the root compiles, unit-tests green, and returns
// "unknown command" to the operator during the failed deployment that requires
// it — see root_test.go's reachability traversal.
func NewCharacterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "character",
		Short: "Character operator commands (direct database access)",
		Long: `Character operator commands.

These talk to PostgreSQL directly via DATABASE_URL rather than to a running
server, because the case they exist for — a duplicate character name found by
migration 000055 — aborts server startup.`,
	}
	cmd.AddCommand(newCharacterNameCmd(defaultCharacterNameEnvFactory))
	return cmd
}

// newCharacterNameCmd builds the `character name` group around an injectable
// environment factory.
func newCharacterNameCmd(factory characterNameEnvFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "name",
		Short: "Inspect and resolve character-name collisions",
	}
	cmd.AddCommand(newCharacterNameDuplicatesCmd(factory))
	cmd.AddCommand(newCharacterNameSetCmd(factory))
	return cmd
}

// newCharacterNameDuplicatesCmd reports every collision migration 000055 would
// halt on, WITHOUT applying the backfill.
//
// It runs the detection inside a transaction and rolls it back, so an operator
// can read the report before the deployment window rather than out of a failed
// migration inside one.
func newCharacterNameDuplicatesCmd(factory characterNameEnvFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "duplicates",
		Short: "Report pre-existing character-name collisions (read-only)",
		Long: `Report every character-name collision migration 000055 halts on.

Runs the same detection the migration runs, inside a transaction that is rolled
back, so nothing is written. Two kinds are reported:

  NORMALIZED-NAME  two rows sharing a uniqueness key; migration 000056's UNIQUE
                   index would reject them.
  SKELETON         two rows that LOOK alike (UTS #39 confusables) but whose
                   normalized names DIFFER, so 000056's index passes over them
                   entirely. This scan is the only thing that sees them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			env, closeEnv, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeEnv()

			sets, err := reportCharacterNameDuplicates(ctx, env.db)
			if err != nil {
				return err
			}
			printDuplicateReport(cmd.OutOrStdout(), sets)
			return nil
		},
	}
}

// reportCharacterNameDuplicates runs the backfill's detection in a transaction
// it always ROLLS BACK.
//
// The rollback is the whole point: BackfillCharacterIdentity populates the
// derived columns as a side effect of finding collisions, and a read-only
// operator report must not half-apply migration 000055.
func reportCharacterNameDuplicates(
	ctx context.Context, db *sql.DB,
) ([]worldpostgres.IdentityCollisionSet, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, oops.Code("CHARACTER_NAME_DUPLICATES_FAILED").
			With("operation", "begin read-only transaction").Wrap(err)
	}
	// Always roll back — including on the success path.
	defer func() { _ = tx.Rollback() }() //nolint:errcheck // rollback is the intended outcome on every path

	sets, err := worldpostgres.BackfillCharacterIdentity(ctx, tx)
	if err != nil {
		return nil, oops.Code("CHARACTER_NAME_DUPLICATES_FAILED").Wrap(err)
	}
	return sets, nil
}

// printDuplicateReport renders the collision sets with the fields D-17 requires
// an operator to have in order to decide which character keeps its name.
func printDuplicateReport(w io.Writer, sets []worldpostgres.IdentityCollisionSet) {
	// Rendered into a buffer and written ONCE, so the single write is the only
	// error-returning call and a partially-printed collision report is not a
	// reachable state.
	var b strings.Builder
	if len(sets) == 0 {
		b.WriteString("No character-name collisions found. Migration 000055 will not halt.\n")
	} else {
		fmt.Fprintf(&b, "%d character-name collision set(s) found.\n", len(sets))
		b.WriteString("Migration 000055 will HALT until every set is resolved with:\n")
		b.WriteString("  holomush character name set <character-id> <new-name>\n")
		for _, set := range sets {
			fmt.Fprintf(&b, "\n%s collision on %q (%d characters):\n",
				describeCollisionKind(set.Kind), set.Key, len(set.Members))
			for _, m := range set.Members {
				fmt.Fprintf(&b, "  id=%s name=%q player_id=%s created_at=%d\n",
					m.ID, m.Name, m.PlayerID, m.CreatedAt)
			}
		}
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		slog.Warn("failed to write the character-name duplicate report", "error", err)
	}
}

// describeCollisionKind names what a collision set actually means, because the
// two kinds call for different operator judgement.
func describeCollisionKind(kind worldpostgres.IdentityCollisionKind) string {
	switch kind {
	case worldpostgres.CollisionNormalizedName:
		return "NORMALIZED-NAME"
	case worldpostgres.CollisionSkeleton:
		return "SKELETON (confusable — migration 000056's index would NOT catch this)"
	default:
		return "UNKNOWN-KIND"
	}
}

// newCharacterNameSetCmd renames one character to an operator-supplied name.
//
// # Gate.Admit is the WHOLE check, and there is no second validation call
//
// charname.Gate.Check runs world.ValidateCharacterName on the normalized
// display form, so the token this mints attests the syntactic rules — length
// bounds, letters and spaces only, no leading, trailing or doubled spaces — as
// well as normalization, mixed script, the block list and skeleton
// non-collision. Adding a second validation call here would be redundant and
// would create a second place for the two to disagree.
//
// # It writes no SQL of its own
//
// cmd/ is inside the world-SQL fence's Go scan and has no allow-marker path,
// and D-22 is explicit that resolution MUST NOT be a hand-written SQL runbook:
// a direct UPDATE bypasses both the §6.1.1 pipeline and the block list and can
// seat a name the index about to be created would reject. Both constraints
// point at the same implementation — route the write through the gated
// CharacterRepository.Rename, whose envelope is emitted in the same
// transaction.
func newCharacterNameSetCmd(factory characterNameEnvFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "set <character-id> <new-name>",
		Short: "Rename a character through the full name-admission pipeline",
		Long: `Rename a character to an operator-supplied name.

The replacement passes the same charname.Gate the create path uses — §6.1.1
normalization, the syntactic rules, mixed-script detection, the operator block
list and UTS #39 skeleton non-collision — before it is written. A name the gate
refuses is never written and the command exits non-zero.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			characterID, err := ulid.Parse(args[0])
			if err != nil {
				return oops.Code("CHARACTER_ID_INVALID").
					With("character_id", args[0]).Wrap(err)
			}

			env, closeEnv, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeEnv()

			// The gate is the only validity check, and it excludes the
			// character's OWN row so a case-variant rename is not read as a
			// self-collision (01-SPEC.md:702-706, B-18).
			//
			// The gate's OWN refusal code is surfaced unchanged rather than
			// remapped to a CLI-level one. Two reasons: the operator needs to
			// know WHICH rule refused (a block-list hit and a confusable
			// collision call for different next actions), and oops resolves the
			// DEEPEST code in a chain (#4902), so a re-code here would be a
			// wrapper that silently does not take effect.
			admitted, err := env.gate.Admit(ctx, args[1], charname.ExcludingCharacter(characterID))
			if err != nil {
				return oops.With("character_id", characterID.String()).
					With("submitted_name", args[1]).Wrap(err)
			}

			if _, err := env.repo.Rename(ctx, characterID, admitted, 0, characterRenameIntent(characterID)); err != nil {
				return oops.With("character_id", characterID.String()).Wrap(err)
			}

			if _, werr := fmt.Fprintf(cmd.OutOrStdout(), "renamed %s to %q\n",
				characterID, admitted.Display()); werr != nil {
				// The rename is committed; a failed confirmation write must not
				// be reported as a failed rename.
				slog.WarnContext(ctx, "rename succeeded but its confirmation could not be written",
					"character_id", characterID.String(), "error", werr)
			}
			return nil
		},
	}
}

// characterRenameIntent builds the world-feed envelope intent for an operator
// rename.
//
// INV-WORLD-4 requires every out-of-world writer to emit its envelope
// atomically with the row it writes; CharacterRepository.Rename does that
// inside its own transaction, so this command cannot produce a name change the
// world feed never saw. The actor is "operator" rather than "system" so an
// audit reader can tell a console rename from an automated one.
func characterRenameIntent(characterID ulid.ULID) wmodel.EnvelopeIntent {
	return wmodel.NewEnvelopeIntent(wmodel.IntentParams{
		GameID:        "main",
		Kind:          "character_updated",
		SchemaVersion: 1,
		Actor:         "operator",
		AggregateType: wmodel.AggregateCharacter,
		AggregateID:   characterID,
		Payload:       []byte(`{"renamed":true}`),
	})
}
