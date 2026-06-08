// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/holomush/holomush/test/testutil"
)

// newTestStore opens a SceneStore against a fresh database on the shared
// Postgres container. Plugin-specific migrations are applied internally by
// NewSceneStore via storage.RunMigrationsFS.
//
// Uses RawDatabase (not FreshDatabase) because the core baseline migration
// creates a legacy scene_participants table with FK to locations(id), which
// conflicts with the plugin's scene_participants that references scenes(id).
// A blank database avoids the conflict and lets the plugin own its schema.
func newTestStore() *SceneStore {
	GinkgoHelper()

	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 2*time.Minute)
	DeferCleanup(cancelSetup)

	connStr := testutil.RawDatabase(suiteT, sharedPG)

	store, err := NewSceneStore(setupCtx, connStr)
	Expect(err).NotTo(HaveOccurred(), "failed to open scene store")
	DeferCleanup(store.Close)

	return store
}

// mustCreateScene inserts a minimal scene row directly via the store's
// Phase 1 Create method. Used by Phase 3 tests that need a pre-existing
// scene but don't care about the participant/ops event side effects of
// CreateWithOwner.
func mustCreateScene(store *SceneStore, sceneID, ownerID, visibility string) *SceneRow {
	GinkgoHelper()
	row := &SceneRow{
		ID:              sceneID,
		Title:           "Test Scene " + sceneID,
		OwnerID:         ownerID,
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      visibility,
		ContentWarnings: []string{},
		Tags:            []string{},
	}
	Expect(store.Create(context.Background(), row)).NotTo(HaveOccurred())
	return row
}

// mustAddParticipant inserts a scene_participants row with the given role.
// joined_at uses SQL-side NOW() in epoch-nanoseconds to match the clock
// domain of the production INSERT paths (store.go). mustCreateScene uses
// the minimal Create (no owner participant), so callers needing an owner in
// the roster MUST add it explicitly via this helper.
func mustAddParticipant(store *SceneStore, sceneID, characterID, role string) {
	GinkgoHelper()
	_, err := store.pool.Exec(context.Background(), `
		INSERT INTO scene_participants (scene_id, character_id, role, joined_at)
		VALUES ($1, $2, $3, (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)`,
		sceneID, characterID, role)
	Expect(err).NotTo(HaveOccurred())
}

// assertParticipantRowExists asserts that a row exists in scene_participants
// for the given (sceneID, characterID) pair with the expected role.
func assertParticipantRowExists(store *SceneStore, sceneID, characterID, expectedRole string) {
	GinkgoHelper()
	var role string
	err := store.pool.QueryRow(
		context.Background(),
		`SELECT role FROM scene_participants WHERE scene_id = $1 AND character_id = $2`,
		sceneID, characterID,
	).Scan(&role)
	Expect(err).NotTo(HaveOccurred(), "expected participant row for (%s, %s) but query failed", sceneID, characterID)
	Expect(role).To(Equal(expectedRole))
}

// assertParticipantRowAbsent asserts that no row exists in scene_participants
// for the given (sceneID, characterID) pair.
func assertParticipantRowAbsent(store *SceneStore, sceneID, characterID string) {
	GinkgoHelper()
	var role string
	err := store.pool.QueryRow(
		context.Background(),
		`SELECT role FROM scene_participants WHERE scene_id = $1 AND character_id = $2`,
		sceneID, characterID,
	).Scan(&role)
	Expect(err).To(MatchError(pgx.ErrNoRows), "expected participant row for (%s, %s) to be absent", sceneID, characterID)
}

// assertOpsEventRecorded asserts that EXACTLY one row exists in
// scene_ops_events for the given scene with the given kind. Returns the
// payload JSON for the caller to inspect kind-specific fields.
func assertOpsEventRecorded(store *SceneStore, sceneID string, kind OpsEventKind, expectedActor, expectedTarget string) map[string]any {
	GinkgoHelper()

	// Step 1: assert exactly one matching row exists.
	var count int
	Expect(store.pool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM scene_ops_events WHERE scene_id = $1 AND kind = $2`,
		sceneID, string(kind),
	).Scan(&count)).NotTo(HaveOccurred())
	Expect(count).To(Equal(1), "expected exactly one ops event %s for scene %s, found %d", kind, sceneID, count)

	// Step 2: read the single row and verify actor/target/payload.
	var (
		actor   string
		target  *string
		payload []byte
	)
	err := store.pool.QueryRow(
		context.Background(), `
		SELECT actor_id, target_id, payload FROM scene_ops_events
		WHERE scene_id = $1 AND kind = $2`,
		sceneID, string(kind),
	).Scan(&actor, &target, &payload)
	Expect(err).NotTo(HaveOccurred(), "expected ops event %s for scene %s but query failed", kind, sceneID)
	Expect(actor).To(Equal(expectedActor))
	if expectedTarget == "" {
		Expect(target).To(BeNil())
	} else {
		Expect(target).NotTo(BeNil())
		Expect(*target).To(Equal(expectedTarget))
	}
	var p map[string]any
	Expect(json.Unmarshal(payload, &p)).NotTo(HaveOccurred())
	return p
}

// countOpsEvents returns the number of scene_ops_events rows for a scene,
// optionally filtered by kind. Pass an empty string for kind to count all.
func countOpsEvents(store *SceneStore, sceneID string, kind OpsEventKind) int {
	GinkgoHelper()
	var n int
	var err error
	if kind == "" {
		err = store.pool.QueryRow(
			context.Background(),
			`SELECT COUNT(*) FROM scene_ops_events WHERE scene_id = $1`,
			sceneID,
		).Scan(&n)
	} else {
		err = store.pool.QueryRow(
			context.Background(),
			`SELECT COUNT(*) FROM scene_ops_events WHERE scene_id = $1 AND kind = $2`,
			sceneID, string(kind),
		).Scan(&n)
	}
	Expect(err).NotTo(HaveOccurred())
	return n
}

var _ = Describe("SceneStore", func() {
	Describe("Create", func() {
		It("persists all scene fields", func() {
			store := newTestStore()
			ctx := context.Background()
			locationID := "loc-01"
			row := &SceneRow{
				ID:              "scene-01HXYZ",
				Title:           "A Decades-Crossed Meeting",
				Description:     "Off-grid private meeting",
				LocationID:      &locationID,
				OwnerID:         "char-alice",
				State:           string(SceneStateActive),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{},
				Tags:            []string{"plot", "social"},
			}

			err := store.Create(ctx, row)
			Expect(err).NotTo(HaveOccurred())

			got, err := store.Get(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ID).To(Equal(row.ID))
			Expect(got.Title).To(Equal(row.Title))
			Expect(got.Description).To(Equal(row.Description))
			Expect(got.LocationID).NotTo(BeNil())
			Expect(*got.LocationID).To(Equal(locationID))
			Expect(got.OwnerID).To(Equal(row.OwnerID))
			Expect(got.State).To(Equal(row.State))
			Expect(got.PoseOrder).To(Equal(row.PoseOrder))
			Expect(got.Visibility).To(Equal(row.Visibility))
			Expect(got.Tags).To(ConsistOf(row.Tags))
			Expect(got.CreatedAt).NotTo(BeZero())
		})

		It("rejects duplicate scene ID", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID:              "scene-dup",
				Title:           "Original",
				OwnerID:         "char-bob",
				State:           string(SceneStateActive),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{},
				Tags:            []string{},
			}

			Expect(store.Create(ctx, row)).NotTo(HaveOccurred())
			err := store.Create(ctx, row)
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_CREATE_FAILED")
		})
	})

	Describe("Get", func() {
		It("returns SCENE_NOT_FOUND for missing scene", func() {
			store := newTestStore()
			ctx := context.Background()

			_, err := store.Get(ctx, "scene-does-not-exist")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_NOT_FOUND")
			errutil.AssertErrorContext(suiteT, err, "scene_id", "scene-does-not-exist")
		})
	})

	Describe("End", func() {
		It("transitions active scene to ended", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID:              "scene-end-active",
				Title:           "End from active",
				OwnerID:         "char-alice",
				State:           string(SceneStateActive),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{},
				Tags:            []string{},
			}
			Expect(store.Create(ctx, row)).NotTo(HaveOccurred())

			got, err := store.End(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.State).To(Equal(string(SceneStateEnded)))
			Expect(got.EndedAt).NotTo(BeNil(), "ended_at should be set")

			reread, err := store.Get(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(reread.State).To(Equal(got.State))
		})

		It("transitions paused scene to ended", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID:              "scene-end-paused",
				Title:           "End from paused",
				OwnerID:         "char-alice",
				State:           string(SceneStatePaused),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{},
				Tags:            []string{},
			}
			Expect(store.Create(ctx, row)).NotTo(HaveOccurred())

			got, err := store.End(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.State).To(Equal(string(SceneStateEnded)))
			Expect(got.EndedAt).NotTo(BeNil())
		})

		It("rejects already ended scene", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID:              "scene-end-twice",
				Title:           "Already ended",
				OwnerID:         "char-alice",
				State:           string(SceneStateEnded),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{},
				Tags:            []string{},
			}
			Expect(store.Create(ctx, row)).NotTo(HaveOccurred())

			_, err := store.End(ctx, row.ID)
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_TRANSITION_FORBIDDEN")
		})

		It("returns SCENE_NOT_FOUND for missing scene", func() {
			store := newTestStore()
			ctx := context.Background()
			_, err := store.End(ctx, "scene-does-not-exist")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_NOT_FOUND")
		})
	})

	Describe("Pause", func() {
		It("transitions active scene to paused", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID:              "scene-pause",
				Title:           "Pause from active",
				OwnerID:         "char-alice",
				State:           string(SceneStateActive),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{},
				Tags:            []string{},
			}
			Expect(store.Create(ctx, row)).NotTo(HaveOccurred())

			got, err := store.Pause(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.State).To(Equal(string(SceneStatePaused)))
		})

		It("rejects already paused scene", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID:              "scene-pause-twice",
				Title:           "Already paused",
				OwnerID:         "char-alice",
				State:           string(SceneStatePaused),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{},
				Tags:            []string{},
			}
			Expect(store.Create(ctx, row)).NotTo(HaveOccurred())

			_, err := store.Pause(ctx, row.ID)
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_TRANSITION_FORBIDDEN")
		})
	})

	Describe("Resume", func() {
		It("transitions paused scene to active", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID:              "scene-resume",
				Title:           "Resume from paused",
				OwnerID:         "char-alice",
				State:           string(SceneStatePaused),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{},
				Tags:            []string{},
			}
			Expect(store.Create(ctx, row)).NotTo(HaveOccurred())

			got, err := store.Resume(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.State).To(Equal(string(SceneStateActive)))
		})

		It("rejects already active scene", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID:              "scene-resume-active",
				Title:           "Already active",
				OwnerID:         "char-alice",
				State:           string(SceneStateActive),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{},
				Tags:            []string{},
			}
			Expect(store.Create(ctx, row)).NotTo(HaveOccurred())

			_, err := store.Resume(ctx, row.ID)
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_TRANSITION_FORBIDDEN")
		})
	})

	Describe("Update", func() {
		It("applies title only when only title is specified", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID:              "scene-update-title",
				Title:           "Original",
				Description:     "Original description",
				OwnerID:         "char-alice",
				State:           string(SceneStateActive),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{"violence"},
				Tags:            []string{"plot"},
			}
			Expect(store.Create(ctx, row)).NotTo(HaveOccurred())

			newTitle := "Renamed"
			update := &SceneUpdate{Title: &newTitle}
			_, err := store.Update(ctx, row.ID, update)
			Expect(err).NotTo(HaveOccurred())

			got, err := store.Get(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Title).To(Equal("Renamed"))
			Expect(got.Description).To(Equal("Original description"))
			Expect(got.ContentWarnings).To(ConsistOf("violence"))
			Expect(got.Tags).To(ConsistOf("plot"))
		})

		It("applies multiple fields simultaneously", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID:              "scene-update-many",
				Title:           "Title 1",
				OwnerID:         "char-alice",
				State:           string(SceneStateActive),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{},
				Tags:            []string{},
			}
			Expect(store.Create(ctx, row)).NotTo(HaveOccurred())

			title := "Title 2"
			desc := "New description"
			vis := "private"
			update := &SceneUpdate{
				Title:       &title,
				Description: &desc,
				Visibility:  &vis,
			}
			_, err := store.Update(ctx, row.ID, update)
			Expect(err).NotTo(HaveOccurred())

			got, err := store.Get(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Title).To(Equal("Title 2"))
			Expect(got.Description).To(Equal("New description"))
			Expect(got.Visibility).To(Equal("private"))
		})

		It("respects repeated fields update flag", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID:              "scene-update-repeated",
				Title:           "T",
				OwnerID:         "char-alice",
				State:           string(SceneStateActive),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{"violence"},
				Tags:            []string{"plot", "social"},
			}
			Expect(store.Create(ctx, row)).NotTo(HaveOccurred())

			// Only update content_warnings; leave tags alone.
			update := &SceneUpdate{
				ContentWarnings:       []string{"violence", "death"},
				UpdateContentWarnings: true,
				Tags:                  nil,
				UpdateTags:            false, // explicitly NOT updating
			}
			_, err := store.Update(ctx, row.ID, update)
			Expect(err).NotTo(HaveOccurred())

			got, err := store.Get(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ContentWarnings).To(ConsistOf("violence", "death"))
			Expect(got.Tags).To(ConsistOf("plot", "social"), "tags should be unchanged")
		})

		It("clears repeated field when empty slice is provided with update flag", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID:              "scene-update-clear",
				Title:           "T",
				OwnerID:         "char-alice",
				State:           string(SceneStateActive),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{"violence"},
				Tags:            []string{},
			}
			Expect(store.Create(ctx, row)).NotTo(HaveOccurred())

			update := &SceneUpdate{
				ContentWarnings:       []string{},
				UpdateContentWarnings: true, // explicit clear
			}
			_, err := store.Update(ctx, row.ID, update)
			Expect(err).NotTo(HaveOccurred())

			got, err := store.Get(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ContentWarnings).To(BeEmpty(), "content_warnings should be cleared to empty slice")
		})

		It("rejects update on ended scene", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID:              "scene-update-ended",
				Title:           "Ended",
				OwnerID:         "char-alice",
				State:           string(SceneStateEnded),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{},
				Tags:            []string{},
			}
			Expect(store.Create(ctx, row)).NotTo(HaveOccurred())

			title := "Try to rename"
			update := &SceneUpdate{Title: &title}
			_, err := store.Update(ctx, row.ID, update)
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_TRANSITION_FORBIDDEN")
		})

		It("returns SCENE_NOT_FOUND for missing scene", func() {
			store := newTestStore()
			ctx := context.Background()
			title := "Anything"
			update := &SceneUpdate{Title: &title}
			_, err := store.Update(ctx, "scene-does-not-exist", update)
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_NOT_FOUND")
		})

		It("is a no-op when no fields are specified", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID:              "scene-update-noop",
				Title:           "Unchanged",
				OwnerID:         "char-alice",
				State:           string(SceneStateActive),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{},
				Tags:            []string{},
			}
			Expect(store.Create(ctx, row)).NotTo(HaveOccurred())

			update := &SceneUpdate{}
			_, err := store.Update(ctx, row.ID, update)
			Expect(err).NotTo(HaveOccurred())

			got, err := store.Get(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Title).To(Equal("Unchanged"))
		})
	})

	Describe("recordOpsEventTx", func() {
		It("writes row with expected kind and payload", func() {
			store := newTestStore()
			ctx := context.Background()

			scene := mustCreateScene(store, "scene-ope-1", "char-alice", string(SceneVisibilityOpen))

			tx, err := store.pool.Begin(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer tx.Rollback(ctx) //nolint:errcheck // best-effort cleanup

			err = recordOpsEventTx(ctx, tx, scene.ID, OpsKindMembershipJoin, "char-alice", "char-alice",
				map[string]any{"visibility": "open", "from_invited": false})
			Expect(err).NotTo(HaveOccurred())
			Expect(tx.Commit(ctx)).NotTo(HaveOccurred())

			payload := assertOpsEventRecorded(store, scene.ID, OpsKindMembershipJoin, "char-alice", "char-alice")
			Expect(payload["visibility"]).To(Equal("open"))
			Expect(payload["from_invited"]).To(Equal(false))
		})

		It("rejects unknown ops event kind", func() {
			store := newTestStore()
			ctx := context.Background()
			mustCreateScene(store, "scene-ope-2", "char-alice", string(SceneVisibilityOpen))

			tx, err := store.pool.Begin(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer tx.Rollback(ctx) //nolint:errcheck // best-effort cleanup

			err = recordOpsEventTx(ctx, tx, "scene-ope-2", OpsEventKind("bogus.kind"), "char-alice", "", nil)
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_OPS_EVENT_INVALID_KIND")
		})

		It("accepts nil payload as empty object", func() {
			store := newTestStore()
			ctx := context.Background()
			mustCreateScene(store, "scene-ope-3", "char-alice", string(SceneVisibilityOpen))

			tx, err := store.pool.Begin(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer tx.Rollback(ctx) //nolint:errcheck // best-effort cleanup

			err = recordOpsEventTx(ctx, tx, "scene-ope-3", OpsKindLifecyclePaused, "char-alice", "", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(tx.Commit(ctx)).NotTo(HaveOccurred())

			payload := assertOpsEventRecorded(store, "scene-ope-3", OpsKindLifecyclePaused, "char-alice", "")
			Expect(payload).To(BeEmpty())
		})
	})

	Describe("CreateWithOwner", func() {
		It("inserts scene, owner participant row, and lifecycle.created ops event", func() {
			store := newTestStore()
			ctx := context.Background()

			row := &SceneRow{
				ID:              "scene-cwo-1",
				Title:           "Owned scene",
				OwnerID:         "char-alice",
				State:           string(SceneStateActive),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityPrivate),
				ContentWarnings: []string{},
				Tags:            []string{},
			}

			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			got, err := store.Get(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.OwnerID).To(Equal(row.OwnerID))

			assertParticipantRowExists(store, row.ID, row.OwnerID, "owner")

			payload := assertOpsEventRecorded(store, row.ID, OpsKindLifecycleCreated, row.OwnerID, "")
			Expect(payload["visibility"]).To(Equal("private"))
			Expect(payload["from_template"]).To(Equal(false))
		})

		It("rolls back when scene ID is duplicate", func() {
			store := newTestStore()
			ctx := context.Background()

			row := &SceneRow{
				ID:              "scene-cwo-dup",
				Title:           "First",
				OwnerID:         "char-alice",
				State:           string(SceneStateActive),
				PoseOrder:       string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{},
				Tags:            []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			rowDup := *row
			rowDup.OwnerID = "char-bob"
			err := store.CreateWithOwner(ctx, &rowDup)
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_CREATE_FAILED")

			assertParticipantRowAbsent(store, row.ID, "char-bob")
			Expect(countOpsEvents(store, row.ID, OpsKindLifecycleCreated)).To(Equal(1))
		})
	})

	Describe("GetWithMembership", func() {
		It("returns participants and invitees lists", func() {
			store := newTestStore()
			ctx := context.Background()

			row := &SceneRow{
				ID: "scene-gwm-1", Title: "T", OwnerID: "char-alice",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityPrivate),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			_, err := store.pool.Exec(ctx,
				`INSERT INTO scene_participants (scene_id, character_id, role) VALUES ($1, 'char-bob', 'member')`,
				row.ID)
			Expect(err).NotTo(HaveOccurred())
			_, err = store.pool.Exec(ctx,
				`INSERT INTO scene_participants (scene_id, character_id, role) VALUES ($1, 'char-carol', 'invited')`,
				row.ID)
			Expect(err).NotTo(HaveOccurred())

			got, participants, invitees, err := store.GetWithMembership(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.OwnerID).To(Equal("char-alice"))
			Expect(participants).To(ConsistOf("char-alice", "char-bob"))
			Expect(invitees).To(ConsistOf("char-carol"))
		})

		It("excludes a role='observer' row from both participants and invitees (holomush-5rh.8.4)", func() {
			store := newTestStore()
			ctx := context.Background()

			row := &SceneRow{
				ID: "scene-gwm-obs", Title: "T", OwnerID: "char-alice",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			mustAddParticipant(store, row.ID, "char-watcher", "observer")

			// The real SQL filter (role IN ('owner','member')) is the gate that
			// makes write-scene-as-participant deny observers — pin it at the
			// real-query layer, not just the fake-store layer (resolver_test.go).
			_, participants, invitees, err := store.GetWithMembership(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(participants).To(ConsistOf("char-alice"),
				"observer row MUST NOT appear in participants")
			Expect(invitees).To(BeEmpty(),
				"observer row MUST NOT appear in invitees either")
		})

		It("returns empty lists when scene has no participants", func() {
			store := newTestStore()
			ctx := context.Background()

			mustCreateScene(store, "scene-gwm-empty", "char-alice", string(SceneVisibilityOpen))

			row, participants, invitees, err := store.GetWithMembership(ctx, "scene-gwm-empty")
			Expect(err).NotTo(HaveOccurred())
			Expect(row.OwnerID).To(Equal("char-alice"))
			Expect(participants).To(BeEmpty())
			Expect(invitees).To(BeEmpty())
		})

		It("returns SCENE_NOT_FOUND for missing scene", func() {
			store := newTestStore()
			ctx := context.Background()

			_, _, _, err := store.GetWithMembership(ctx, "scene-gwm-missing")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_NOT_FOUND")
		})
	})

	Describe("AddParticipant", func() {
		It("inserts fresh member row for open scene", func() {
			store := newTestStore()
			ctx := context.Background()

			row := &SceneRow{
				ID: "scene-ap-1", Title: "T", OwnerID: "char-alice",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			got, result, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(OpInserted))
			Expect(got.CharacterID).To(Equal("char-bob"))
			Expect(got.Role).To(Equal("member"))
			assertParticipantRowExists(store, row.ID, "char-bob", "member")
			assertOpsEventRecorded(store, row.ID, OpsKindMembershipJoin, "char-bob", "char-bob")
		})

		It("promotes invited row to member on private scene", func() {
			store := newTestStore()
			ctx := context.Background()

			row := &SceneRow{
				ID: "scene-ap-promote", Title: "T", OwnerID: "char-alice",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityPrivate),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			_, err := store.pool.Exec(ctx,
				`INSERT INTO scene_participants (scene_id, character_id, role) VALUES ($1, 'char-bob', 'invited')`,
				row.ID)
			Expect(err).NotTo(HaveOccurred())

			got, result, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(OpPromoted))
			Expect(got.Role).To(Equal("member"))
			assertParticipantRowExists(store, row.ID, "char-bob", "member")

			payload := assertOpsEventRecorded(store, row.ID, OpsKindMembershipJoin, "char-bob", "char-bob")
			Expect(payload["visibility"]).To(Equal("private"))
			Expect(payload["from_invited"]).To(Equal(true))
		})

		It("returns OpNoChange for existing member without emitting new ops event", func() {
			store := newTestStore()
			ctx := context.Background()

			row := &SceneRow{
				ID: "scene-ap-noop", Title: "T", OwnerID: "char-alice",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			_, result1, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(result1).To(Equal(OpInserted))

			_, result2, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(result2).To(Equal(OpNoChange))

			Expect(countOpsEvents(store, row.ID, OpsKindMembershipJoin)).To(Equal(1))
		})

		It("rejects private scene without invitation", func() {
			store := newTestStore()
			ctx := context.Background()

			row := &SceneRow{
				ID: "scene-ap-priv", Title: "T", OwnerID: "char-alice",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityPrivate),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_JOIN_NOT_INVITED")
			errutil.AssertErrorContext(suiteT, err, "scene_id", row.ID)
			errutil.AssertErrorContext(suiteT, err, "character_id", "char-bob")
		})

		It("rejects ended scene", func() {
			store := newTestStore()
			ctx := context.Background()

			row := &SceneRow{
				ID: "scene-ap-ended", Title: "T", OwnerID: "char-alice",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, err := store.End(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())

			_, _, err = store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_TRANSITION_FORBIDDEN")
			errutil.AssertErrorContext(suiteT, err, "current_state", "ended")
		})

		It("returns SCENE_NOT_FOUND for missing scene", func() {
			store := newTestStore()
			_, _, err := store.AddParticipant(context.Background(), "scene-nope", "char-bob")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_NOT_FOUND")
		})

		It("works on paused scene", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-bnd-pj", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, err := store.Pause(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())

			got, result, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(OpInserted))
			Expect(got.Role).To(Equal("member"))
		})
	})

	Describe("RemoveParticipant", func() {
		It("deletes member row and emits ops event", func() {
			store := newTestStore()
			ctx := context.Background()

			row := &SceneRow{
				ID: "scene-rp-1", Title: "T", OwnerID: "char-alice",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())

			got, err := store.RemoveParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Role).To(Equal("member"))
			assertParticipantRowAbsent(store, row.ID, "char-bob")

			payload := assertOpsEventRecorded(store, row.ID, OpsKindMembershipLeave, "char-bob", "char-bob")
			Expect(payload["prior_role"]).To(Equal("member"))
		})

		It("refuses to remove owner", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-rp-owner", OwnerID: "char-alice",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility: string(SceneVisibilityOpen), Title: "T",
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			_, err := store.RemoveParticipant(ctx, row.ID, "char-alice")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_OWNER_CANNOT_LEAVE")
			assertParticipantRowExists(store, row.ID, "char-alice", "owner")
		})

		It("returns SCENE_PARTICIPANT_NOT_FOUND for missing participant", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-rp-missing", OwnerID: "char-alice",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility: string(SceneVisibilityOpen), Title: "T",
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			_, err := store.RemoveParticipant(ctx, row.ID, "char-ghost")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_PARTICIPANT_NOT_FOUND")
		})
	})

	Describe("InviteParticipant", func() {
		It("inserts invited row and emits ops event", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-inv-1", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityPrivate),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			got, err := store.InviteParticipant(ctx, row.ID, "char-alice", "char-bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Role).To(Equal("invited"))
			assertParticipantRowExists(store, row.ID, "char-bob", "invited")
			assertOpsEventRecorded(store, row.ID, OpsKindMembershipInvite, "char-alice", "char-bob")
		})

		It("is idempotent for existing invitee (no second ops event)", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-inv-2", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityPrivate),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			_, err := store.InviteParticipant(ctx, row.ID, "char-alice", "char-bob")
			Expect(err).NotTo(HaveOccurred())
			_, err = store.InviteParticipant(ctx, row.ID, "char-alice", "char-bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(countOpsEvents(store, row.ID, OpsKindMembershipInvite)).To(Equal(1))
		})

		It("rejects existing member", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-inv-3", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())

			_, err = store.InviteParticipant(ctx, row.ID, "char-alice", "char-bob")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_INVITE_TARGET_ALREADY_MEMBER")
		})

		It("rejects owner as invite target", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-bnd-io", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityPrivate),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			_, err := store.InviteParticipant(ctx, row.ID, "char-alice", "char-alice")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_INVITE_TARGET_ALREADY_MEMBER")
			errutil.AssertErrorContext(suiteT, err, "current_role", "owner")
		})
	})

	Describe("KickParticipant", func() {
		It("removes member row and emits ops event", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-kp-1", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())

			got, err := store.KickParticipant(ctx, row.ID, "char-alice", "char-bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Role).To(Equal("member"))
			assertParticipantRowAbsent(store, row.ID, "char-bob")

			payload := assertOpsEventRecorded(store, row.ID, OpsKindMembershipKick, "char-alice", "char-bob")
			Expect(payload["prior_role"]).To(Equal("member"))
		})

		It("removes invited row and payload reflects prior role", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-kp-inv", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityPrivate),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, err := store.InviteParticipant(ctx, row.ID, "char-alice", "char-bob")
			Expect(err).NotTo(HaveOccurred())

			got, err := store.KickParticipant(ctx, row.ID, "char-alice", "char-bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Role).To(Equal("invited"))
			assertParticipantRowAbsent(store, row.ID, "char-bob")

			payload := assertOpsEventRecorded(store, row.ID, OpsKindMembershipKick, "char-alice", "char-bob")
			Expect(payload["prior_role"]).To(Equal("invited"))
		})

		It("refuses to kick owner", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-kp-owner", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			_, err := store.KickParticipant(ctx, row.ID, "char-alice", "char-alice")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_KICK_FORBIDDEN")
			assertParticipantRowExists(store, row.ID, "char-alice", "owner")
		})

		It("works on paused scene", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-bnd-pk", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())
			_, err = store.Pause(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())

			_, err = store.KickParticipant(ctx, row.ID, "char-alice", "char-bob")
			Expect(err).NotTo(HaveOccurred())
			assertParticipantRowAbsent(store, row.ID, "char-bob")
		})
	})

	Describe("TransferOwnership", func() {
		It("updates participants and scenes row atomically", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-to-1", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())

			Expect(store.TransferOwnership(ctx, row.ID, "char-alice", "char-bob")).NotTo(HaveOccurred())

			assertParticipantRowExists(store, row.ID, "char-alice", "member")
			assertParticipantRowExists(store, row.ID, "char-bob", "owner")
			got, err := store.Get(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.OwnerID).To(Equal("char-bob"))

			payload := assertOpsEventRecorded(store, row.ID, OpsKindMembershipOwnershipTransferred, "char-alice", "char-bob")
			Expect(payload["from"]).To(Equal("char-alice"))
		})

		It("rejects non-member target", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-to-nm", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			err := store.TransferOwnership(ctx, row.ID, "char-alice", "char-bob")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_TRANSFER_TARGET_NOT_MEMBER")
		})

		It("rejects non-owner caller", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-to-no", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())
			_, _, err = store.AddParticipant(ctx, row.ID, "char-carol")
			Expect(err).NotTo(HaveOccurred())

			err = store.TransferOwnership(ctx, row.ID, "char-bob", "char-carol")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_NOT_OWNER")
		})

		It("is a no-op when target equals current owner", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-to-self", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			Expect(store.TransferOwnership(ctx, row.ID, "char-alice", "char-alice")).NotTo(HaveOccurred())
			assertParticipantRowExists(store, row.ID, "char-alice", "owner")
			Expect(countOpsEvents(store, row.ID, OpsKindMembershipOwnershipTransferred)).To(Equal(0))
		})

		It("works on paused scene", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-bnd-pt", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())
			_, err = store.Pause(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())

			Expect(store.TransferOwnership(ctx, row.ID, "char-alice", "char-bob")).NotTo(HaveOccurred())
			assertParticipantRowExists(store, row.ID, "char-bob", "owner")
			got, err := store.Get(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.OwnerID).To(Equal("char-bob"))
			Expect(got.State).To(Equal("paused"), "transfer must not affect scene state")
		})
	})

	Describe("ListParticipants", func() {
		It("returns all roles ordered by joined_at", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-lp-1", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())

			got, err := store.ListParticipants(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(2))
			Expect(got[0].CharacterID).To(Equal("char-alice"))
			Expect(got[0].Role).To(Equal("owner"))
			Expect(got[1].CharacterID).To(Equal("char-bob"))
			Expect(got[1].Role).To(Equal("member"))
		})

		It("returns empty slice for missing scene (no error)", func() {
			store := newTestStore()
			ctx := context.Background()

			got, err := store.ListParticipants(ctx, "scene-does-not-exist")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})
	})

	Describe("GetParticipant", func() {
		It("returns row when participant is present", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-gp-1", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			got, err := store.GetParticipant(ctx, row.ID, "char-alice")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Role).To(Equal("owner"))
		})

		It("returns SCENE_PARTICIPANT_NOT_FOUND for missing participant", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-gp-missing", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			_, err := store.GetParticipant(ctx, row.ID, "char-ghost")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_PARTICIPANT_NOT_FOUND")
		})
	})

	Describe("lifecycle ops events", func() {
		It("End emits lifecycle.ended ops event in same transaction", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-end-ope", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			_, err := store.End(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())

			payload := assertOpsEventRecorded(store, row.ID, OpsKindLifecycleEnded, row.OwnerID, "")
			Expect(payload["prior_state"]).To(Equal("active"))
		})

		It("Pause emits lifecycle.paused ops event", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-pause-ope", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			_, err := store.Pause(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			assertOpsEventRecorded(store, row.ID, OpsKindLifecyclePaused, row.OwnerID, "")
		})

		It("Resume emits lifecycle.resumed ops event", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-resume-ope", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, err := store.Pause(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())

			_, err = store.Resume(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			assertOpsEventRecorded(store, row.ID, OpsKindLifecycleResumed, row.OwnerID, "")
		})

		It("Update emits settings.updated ops event with mask paths", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-upd-ope", OwnerID: "char-alice", Title: "Old",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			newTitle := "New"
			_, err := store.Update(ctx, row.ID, &SceneUpdate{Title: &newTitle})
			Expect(err).NotTo(HaveOccurred())

			payload := assertOpsEventRecorded(store, row.ID, OpsKindSettingsUpdated, row.OwnerID, "")
			paths, ok := payload["paths"].([]any)
			Expect(ok).To(BeTrue())
			Expect(paths).To(ContainElement("title"))
		})
	})

	Describe("access policy invariants", func() {
		It("owner can read own scene via participant policy", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-locks-1", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			_, participants, _, err := store.GetWithMembership(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(participants).To(ContainElement("char-alice"), "owner must be in participants list")
		})

		It("member appears in participants list for resume-scene-as-participant policy", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-locks-resume", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())
			_, err = store.Pause(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())

			_, participants, _, err := store.GetWithMembership(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(participants).To(ContainElement("char-bob"),
				"member must be in participants list for resume-scene-as-participant policy")
		})

		It("kicked character immediately disappears from participants", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-locks-kick", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())

			_, before, _, err := store.GetWithMembership(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(before).To(ContainElement("char-bob"))

			_, err = store.KickParticipant(ctx, row.ID, "char-alice", "char-bob")
			Expect(err).NotTo(HaveOccurred())

			_, after, _, err := store.GetWithMembership(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(after).NotTo(ContainElement("char-bob"),
				"kicked character must immediately disappear from participants list")
		})

		It("invitee can join private scene", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-locks-pj", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityPrivate),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, err := store.InviteParticipant(ctx, row.ID, "char-alice", "char-bob")
			Expect(err).NotTo(HaveOccurred())

			got, result, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(OpPromoted))
			Expect(got.Role).To(Equal("member"))
		})

		It("non-invitee cannot join private scene", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-locks-pj-no", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityPrivate),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_JOIN_NOT_INVITED")
		})

		It("owner cannot leave own scene", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-locks-ol", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			_, err := store.RemoveParticipant(ctx, row.ID, "char-alice")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "SCENE_OWNER_CANNOT_LEAVE")
		})

		It("owner can transfer to member, and previous owner becomes member", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-locks-xfer", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())

			Expect(store.TransferOwnership(ctx, row.ID, "char-alice", "char-bob")).NotTo(HaveOccurred())

			assertParticipantRowExists(store, row.ID, "char-alice", "member")
			assertParticipantRowExists(store, row.ID, "char-bob", "owner")
			got, err := store.Get(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.OwnerID).To(Equal("char-bob"))

			// Now char-alice (no longer owner) CAN leave.
			_, err = store.RemoveParticipant(ctx, row.ID, "char-alice")
			Expect(err).NotTo(HaveOccurred())
			assertParticipantRowAbsent(store, row.ID, "char-alice")
		})
	})

	Describe("database schema invariants", func() {
		It("scenes.owner_id denorm always matches participant owner row", func() {
			store := newTestStore()
			ctx := context.Background()

			assertInvariant := func(sceneID string) {
				GinkgoHelper()
				var (
					denormOwnerID    string
					participantOwner string
				)
				Expect(store.pool.QueryRow(
					ctx,
					`SELECT owner_id FROM scenes WHERE id = $1`, sceneID,
				).Scan(&denormOwnerID)).NotTo(HaveOccurred())
				Expect(store.pool.QueryRow(
					ctx,
					`SELECT character_id FROM scene_participants WHERE scene_id = $1 AND role = 'owner'`,
					sceneID,
				).Scan(&participantOwner)).NotTo(HaveOccurred())
				Expect(denormOwnerID).To(Equal(participantOwner),
					"scenes.owner_id must always match the participant row with role='owner'")
			}

			row := &SceneRow{
				ID: "scene-inv-denorm", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}

			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			assertInvariant(row.ID)

			_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())
			assertInvariant(row.ID)

			Expect(store.TransferOwnership(ctx, row.ID, "char-alice", "char-bob")).NotTo(HaveOccurred())
			assertInvariant(row.ID)

			_, err = store.RemoveParticipant(ctx, row.ID, "char-alice")
			Expect(err).NotTo(HaveOccurred())
			assertInvariant(row.ID)
		})

		It("each membership mutation produces exactly one ops event", func() {
			store := newTestStore()
			ctx := context.Background()

			row := &SceneRow{
				ID: "scene-inv-count", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityPrivate),
				ContentWarnings: []string{}, Tags: []string{},
			}

			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			Expect(countOpsEvents(store, row.ID, "")).To(Equal(1), "after create")

			_, err := store.InviteParticipant(ctx, row.ID, "char-alice", "char-bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(countOpsEvents(store, row.ID, "")).To(Equal(2), "after invite")

			_, err = store.InviteParticipant(ctx, row.ID, "char-alice", "char-bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(countOpsEvents(store, row.ID, "")).To(Equal(2), "after redundant invite")

			_, _, err = store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(countOpsEvents(store, row.ID, "")).To(Equal(3), "after join")

			_, _, err = store.AddParticipant(ctx, row.ID, "char-bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(countOpsEvents(store, row.ID, "")).To(Equal(3), "after redundant join")

			_, err = store.KickParticipant(ctx, row.ID, "char-alice", "char-bob")
			Expect(err).NotTo(HaveOccurred())
			Expect(countOpsEvents(store, row.ID, "")).To(Equal(4), "after kick")

			_, err = store.Pause(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(countOpsEvents(store, row.ID, "")).To(Equal(5), "after pause")
			_, err = store.Resume(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(countOpsEvents(store, row.ID, "")).To(Equal(6), "after resume")

			_, err = store.End(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(countOpsEvents(store, row.ID, "")).To(Equal(7), "after end")
		})

		It("participant primary key prevents double insertion", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-inv-pk", OwnerID: "char-alice", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			_, err := store.pool.Exec(
				ctx,
				`INSERT INTO scene_participants (scene_id, character_id, role) VALUES ($1, $2, 'member')`,
				row.ID, "char-alice",
			)
			Expect(err).To(HaveOccurred(), "duplicate (scene_id, character_id) must violate PK")

			var count int
			Expect(store.pool.QueryRow(
				ctx,
				`SELECT COUNT(*) FROM scene_participants WHERE scene_id = $1 AND character_id = $2`,
				row.ID, "char-alice",
			).Scan(&count)).NotTo(HaveOccurred())
			Expect(count).To(Equal(1))
		})

		It("ops events cannot be updated or deleted by application code (static check)", func() {
			// This is a meta-test: assert that the codebase contains no UPDATE or
			// DELETE statements against scene_ops_events. Append-only is enforced
			// by convention (the recordOpsEventTx helper is the only writer); this
			// test catches accidental future violations.
			goFiles, err := filepath.Glob("*.go")
			Expect(err).NotTo(HaveOccurred())
			Expect(goFiles).NotTo(BeEmpty(), "expected at least one .go file in package directory")

			whitespace := regexp.MustCompile(`\s+`)
			for _, fname := range goFiles {
				if strings.HasSuffix(fname, "_test.go") {
					continue
				}
				data, err := os.ReadFile(fname)
				Expect(err).NotTo(HaveOccurred(), "failed to read %s", fname)
				content := whitespace.ReplaceAllString(strings.ToLower(string(data)), " ")
				Expect(content).NotTo(ContainSubstring("update scene_ops_events"),
					"%s contains UPDATE on scene_ops_events — events must be immutable", fname)
				Expect(content).NotTo(ContainSubstring("delete from scene_ops_events"),
					"%s contains DELETE on scene_ops_events — events must be immutable", fname)
			}
		})
	})

	Describe("IsParticipant", func() {
		It("returns true for owner", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-ip-owner", OwnerID: "char-owner-1", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			ok, err := store.IsParticipant(ctx, row.ID, "char-owner-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue(), "owner MUST pass the INV-SCENE-60 gate")
		})

		It("returns true for member", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-ip-member", OwnerID: "char-owner-2", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, _, err := store.AddParticipant(ctx, row.ID, "char-member-2")
			Expect(err).NotTo(HaveOccurred())

			ok, err := store.IsParticipant(ctx, row.ID, "char-member-2")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue(), "member MUST pass the INV-SCENE-60 gate")
		})

		It("returns false for invited role (NOT a participant for INV-SCENE-60 gate)", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-ip-invited", OwnerID: "char-owner-3", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityPrivate),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, err := store.InviteParticipant(ctx, row.ID, "char-owner-3", "char-invitee-3")
			Expect(err).NotTo(HaveOccurred())

			ok, err := store.IsParticipant(ctx, row.ID, "char-invitee-3")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse(), "invited role MUST NOT count as participant for INV-SCENE-60 gate")
		})

		It("returns false for character not in scene", func() {
			store := newTestStore()
			ctx := context.Background()
			row := &SceneRow{
				ID: "scene-ip-outsider", OwnerID: "char-owner-4", Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

			ok, err := store.IsParticipant(ctx, row.ID, "char-outsider-4")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse(), "non-participant MUST return false with nil error")
		})
	})

	Describe("ListParticipantsWithPoseMeta", func() {
		It("returns zero TotalPoseCount and nil pose fields when no poses have been recorded", func() {
			store := newTestStore()
			ctx := context.Background()

			owner := "char-lpm-owner-1"
			member := "char-lpm-member-1"
			sceneID := "scene-lpm-no-poses"
			row := &SceneRow{
				ID: sceneID, OwnerID: owner, Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, _, err := store.AddParticipant(ctx, sceneID, member)
			Expect(err).NotTo(HaveOccurred())

			result, err := store.ListParticipantsWithPoseMeta(ctx, sceneID)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.TotalPoseCount).To(BeZero(), "total_pose_count MUST be 0 when no poses recorded")
			Expect(result.Participants).To(HaveLen(2))

			for _, p := range result.Participants {
				Expect(p.LastPoseAt).To(BeNil(), "LastPoseAt MUST be nil when participant has never posed")
				Expect(p.LastPoseSeq).To(BeNil(), "LastPoseSeq MUST be nil when participant has never posed")
			}
		})

		It("excludes invited role from result", func() {
			store := newTestStore()
			ctx := context.Background()

			owner := "char-lpm-owner-2"
			member := "char-lpm-member-2"
			invitee := "char-lpm-invitee-2"
			sceneID := "scene-lpm-excludes-invited"
			row := &SceneRow{
				ID: sceneID, OwnerID: owner, Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityPrivate),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			// Private scene: invite member first so AddParticipant can promote.
			_, err := store.InviteParticipant(ctx, sceneID, owner, member)
			Expect(err).NotTo(HaveOccurred())
			_, _, err = store.AddParticipant(ctx, sceneID, member)
			Expect(err).NotTo(HaveOccurred())
			// invitee stays in invited role (never promoted).
			_, err = store.InviteParticipant(ctx, sceneID, owner, invitee)
			Expect(err).NotTo(HaveOccurred())

			result, err := store.ListParticipantsWithPoseMeta(ctx, sceneID)
			Expect(err).NotTo(HaveOccurred())
			// Only owner + member — invited MUST be excluded per INV-SCENE-60.
			Expect(result.Participants).To(HaveLen(2))
			charIDs := make([]string, 0, 2)
			for _, p := range result.Participants {
				charIDs = append(charIDs, p.CharacterID)
			}
			Expect(charIDs).To(ConsistOf(owner, member))
			Expect(charIDs).NotTo(ContainElement(invitee), "invited role MUST be excluded per INV-SCENE-60")
		})

		It("reflects updated pose metadata after direct column write", func() {
			store := newTestStore()
			ctx := context.Background()

			owner := "char-lpm-owner-3"
			member := "char-lpm-member-3"
			sceneID := "scene-lpm-pose-meta"
			row := &SceneRow{
				ID: sceneID, OwnerID: owner, Title: "T",
				State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
				Visibility:      string(SceneVisibilityOpen),
				ContentWarnings: []string{}, Tags: []string{},
			}
			Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
			_, _, err := store.AddParticipant(ctx, sceneID, member)
			Expect(err).NotTo(HaveOccurred())

			// Simulate what SceneAuditStore would do: write pose metadata directly.
			poseTime := time.Now().UTC().Truncate(time.Millisecond)
			poseSeq := int32(3)
			_, err = store.pool.Exec(
				ctx,
				`UPDATE scenes SET total_pose_count = 3 WHERE id = $1`,
				sceneID,
			)
			Expect(err).NotTo(HaveOccurred())
			_, err = store.pool.Exec(
				ctx,
				`UPDATE scene_participants SET last_pose_at = $1, last_pose_seq = $2
				 WHERE scene_id = $3 AND character_id = $4`,
				pgnanos.From(poseTime), poseSeq, sceneID, member,
			)
			Expect(err).NotTo(HaveOccurred())

			result, err := store.ListParticipantsWithPoseMeta(ctx, sceneID)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.TotalPoseCount).To(Equal(uint32(3)), "TotalPoseCount MUST reflect updated total_pose_count")

			// Find the member in the result.
			var memberMeta *ParticipantWithPoseMeta
			for i := range result.Participants {
				if result.Participants[i].CharacterID == member {
					memberMeta = &result.Participants[i]
					break
				}
			}
			Expect(memberMeta).NotTo(BeNil(), "member MUST appear in result")
			Expect(memberMeta.LastPoseAt).NotTo(BeNil(), "LastPoseAt MUST be set after pose write")
			Expect(memberMeta.LastPoseAt.Time().UTC().Truncate(time.Millisecond)).To(Equal(poseTime))
			Expect(memberMeta.LastPoseSeq).NotTo(BeNil(), "LastPoseSeq MUST be set after pose write")
			Expect(*memberMeta.LastPoseSeq).To(Equal(poseSeq))
		})
	})

	Describe("IsMember", func() {
		DescribeTable(
			"returns expected result by role and scene state",
			func(sceneID string, visible SceneVisibility, setupFn func(*SceneStore), probe string, want bool, wantMsg string) {
				store := newTestStore()
				ctx := context.Background()

				if visible != "" {
					row := &SceneRow{
						ID: sceneID, OwnerID: "char-alice", Title: "T",
						State:           string(SceneStateActive),
						PoseOrder:       string(PoseOrderModeFree),
						Visibility:      string(visible),
						ContentWarnings: []string{}, Tags: []string{},
					}
					Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
				}
				if setupFn != nil {
					setupFn(store)
				}

				got, err := store.IsMember(ctx, sceneID, probe)
				Expect(err).NotTo(HaveOccurred(), wantMsg)
				if wantMsg != "" {
					Expect(got).To(Equal(want), wantMsg)
				} else {
					Expect(got).To(Equal(want))
				}
			},
			Entry(
				"returns true for owner",
				"scene-isM-1", SceneVisibilityOpen,
				nil,
				"char-alice", true, "owner MUST be reported as member",
			),
			Entry(
				"returns true for joined member",
				"scene-isM-2", SceneVisibilityOpen,
				func(store *SceneStore) {
					_, _, err := store.AddParticipant(context.Background(), "scene-isM-2", "char-bob")
					Expect(err).NotTo(HaveOccurred())
				},
				"char-bob", true, "",
			),
			Entry(
				"returns false for invited-only",
				"scene-isM-3", SceneVisibilityPrivate,
				func(store *SceneStore) {
					_, err := store.InviteParticipant(context.Background(), "scene-isM-3", "char-alice", "char-bob")
					Expect(err).NotTo(HaveOccurred())
				},
				"char-bob", false, "invited-only rows MUST return false — invitation grants join, not read",
			),
			Entry(
				"returns false for non-participant",
				"scene-isM-4", SceneVisibilityOpen,
				nil,
				"char-stranger", false, "",
			),
			Entry(
				"returns false for missing scene",
				"scene-isM-missing", SceneVisibility(""), // sentinel: skip scene creation
				nil,
				"char-alice", false, "missing scene MUST be nil error per spec §5.4 (info-hiding)",
			),
		)
	})

	Describe("published_scenes / published_scene_votes FK cascade", func() {
		It("cascades vote deletion when the parent published_scene is deleted", func() {
			store := newTestStore()
			ctx := context.Background()

			pubID := "ps-cascade-test-01"
			sceneID := "scene-cascade-test-01"

			// Insert a published_scenes row with all NOT NULL columns.
			// initiated_at is BIGINT epoch-nanoseconds (INV-STORE-1).
			_, err := store.pool.Exec(
				ctx, `
				INSERT INTO published_scenes
					(id, scene_id, attempt_number, status, initiated_by, initiated_at,
					 vote_window, cooloff_window, max_attempts_snapshot)
				VALUES
					($1, $2, 1, 'COLLECTING', 'char-initiator',
					 (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
					 '7 days'::interval, '30 minutes'::interval, 3)`,
				pubID, sceneID,
			)
			Expect(err).NotTo(HaveOccurred())

			// Insert two vote rows referencing the published_scene.
			_, err = store.pool.Exec(
				ctx, `
				INSERT INTO published_scene_votes (published_scene_id, character_id)
				VALUES ($1, 'char-voter-a'), ($1, 'char-voter-b')`,
				pubID,
			)
			Expect(err).NotTo(HaveOccurred())

			// Confirm both votes exist.
			var count int
			Expect(store.pool.QueryRow(
				ctx,
				`SELECT COUNT(*) FROM published_scene_votes WHERE published_scene_id = $1`,
				pubID,
			).Scan(&count)).NotTo(HaveOccurred())
			Expect(count).To(Equal(2))

			// Delete the parent published_scene row.
			_, err = store.pool.Exec(
				ctx,
				`DELETE FROM published_scenes WHERE id = $1`, pubID,
			)
			Expect(err).NotTo(HaveOccurred())

			// Assert the cascade fired: no vote rows remain.
			Expect(store.pool.QueryRow(
				ctx,
				`SELECT COUNT(*) FROM published_scene_votes WHERE published_scene_id = $1`,
				pubID,
			).Scan(&count)).NotTo(HaveOccurred())
			Expect(count).To(Equal(0))
		})
	})
})

var _ = Describe("Publish store — attempt + roster lifecycle", func() {
	It("creates a published_scenes row with COLLECTING status and frozen roster", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		sceneID := ulid.Make().String()
		ownerID := ulid.Make().String()
		memberID := ulid.Make().String()
		invitedID := ulid.Make().String()
		mustCreateScene(store, sceneID, ownerID, string(SceneVisibilityOpen))
		// mustCreateScene uses the minimal Create (no owner participant), so
		// add owner explicitly alongside member + invited.
		mustAddParticipant(store, sceneID, ownerID, "owner")
		mustAddParticipant(store, sceneID, memberID, "member")
		mustAddParticipant(store, sceneID, invitedID, "invited")

		pub, err := store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
			SceneID:       sceneID,
			AttemptNumber: 1,
			InitiatedBy:   ownerID,
			VoteWindow:    7 * 24 * time.Hour,
			CoolOffWindow: 30 * time.Minute,
			MaxAttempts:   3,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(pub.Status).To(Equal(StatusCollecting))
		Expect(pub.AttemptNumber).To(Equal(1))

		voters, err := store.ListPublishVoters(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(voters).To(HaveLen(2), "roster must include owner+member, NOT invited (INV-SCENE-28)")
		voterIDs := make(map[string]bool, len(voters))
		for _, v := range voters {
			voterIDs[v.CharacterID] = true
			Expect(v.Vote).To(BeNil(), "fresh roster row MUST start with vote=nil (pending)")
		}
		Expect(voterIDs[ownerID]).To(BeTrue())
		Expect(voterIDs[memberID]).To(BeTrue())
		Expect(voterIDs[invitedID]).To(BeFalse(), "invited role MUST be excluded — INV-SCENE-28")
	})

	It("rejects a duplicate active attempt for the same scene", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		sceneID := ulid.Make().String()
		ownerID := ulid.Make().String()
		mustCreateScene(store, sceneID, ownerID, string(SceneVisibilityOpen))
		mustAddParticipant(store, sceneID, ownerID, "owner")

		_, err := store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
			SceneID: sceneID, AttemptNumber: 1, InitiatedBy: ownerID,
			VoteWindow: 7 * 24 * time.Hour, CoolOffWindow: 30 * time.Minute, MaxAttempts: 3,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
			SceneID: sceneID, AttemptNumber: 2, InitiatedBy: ownerID,
			VoteWindow: 7 * 24 * time.Hour, CoolOffWindow: 30 * time.Minute, MaxAttempts: 3,
		})
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "SCENE_PUBLISH_ALREADY_ACTIVE")
	})

	It("fails closed when the scene has no eligible (owner/member) voters", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		sceneID := ulid.Make().String()
		ownerID := ulid.Make().String()
		invitedID := ulid.Make().String()
		mustCreateScene(store, sceneID, ownerID, string(SceneVisibilityOpen))
		// Only an invited participant — no owner/member to seed the roster.
		mustAddParticipant(store, sceneID, invitedID, "invited")

		_, err := store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
			SceneID: sceneID, AttemptNumber: 1, InitiatedBy: ownerID,
			VoteWindow: 7 * 24 * time.Hour, CoolOffWindow: 30 * time.Minute, MaxAttempts: 3,
		})
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "SCENE_PUBLISH_NO_ELIGIBLE_VOTERS")

		// The failed attempt's row must not survive (transaction rolled back).
		var n int
		Expect(store.pool.QueryRow(
			ctx,
			`SELECT count(*) FROM published_scenes WHERE scene_id = $1`, sceneID,
		).Scan(&n)).NotTo(HaveOccurred())
		Expect(n).To(Equal(0), "rolled-back attempt MUST leave no published_scenes row")
	})
})

// setupAttemptWithOneVoter creates a scene with a single owner participant
// and one COLLECTING publish attempt; the roster therefore has exactly one
// voter (the owner). Returns the attempt and the owner/voter character ID.
func setupAttemptWithOneVoter(ctx context.Context, store *SceneStore) (*PublishedScene, string) {
	GinkgoHelper()
	sceneID := ulid.Make().String()
	ownerID := ulid.Make().String()
	mustCreateScene(store, sceneID, ownerID, string(SceneVisibilityOpen))
	mustAddParticipant(store, sceneID, ownerID, "owner")
	pub, err := store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
		SceneID: sceneID, AttemptNumber: 1, InitiatedBy: ownerID,
		VoteWindow: 7 * 24 * time.Hour, CoolOffWindow: 30 * time.Minute, MaxAttempts: 3,
	})
	Expect(err).NotTo(HaveOccurred())
	return pub, ownerID
}

// setupAttemptWith3Voters creates a scene with owner + two members and one
// COLLECTING attempt; the roster therefore has exactly three voters.
func setupAttemptWith3Voters(ctx context.Context, store *SceneStore) *PublishedScene {
	GinkgoHelper()
	sceneID := ulid.Make().String()
	ownerID := ulid.Make().String()
	mustCreateScene(store, sceneID, ownerID, string(SceneVisibilityOpen))
	mustAddParticipant(store, sceneID, ownerID, "owner")
	mustAddParticipant(store, sceneID, ulid.Make().String(), "member")
	mustAddParticipant(store, sceneID, ulid.Make().String(), "member")
	pub, err := store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
		SceneID: sceneID, AttemptNumber: 1, InitiatedBy: ownerID,
		VoteWindow: 7 * 24 * time.Hour, CoolOffWindow: 30 * time.Minute, MaxAttempts: 3,
	})
	Expect(err).NotTo(HaveOccurred())
	return pub
}

var _ = Describe("Publish store — vote operations", func() {
	It("returns is_change=false on first cast", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub, voter := setupAttemptWithOneVoter(ctx, store)

		result, err := store.CastVote(ctx, pub.ID, voter, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.IsChange).To(BeFalse(), "first cast is not a change")
		Expect(result.Vote).To(BeTrue())
	})

	It("returns is_change=true when a vote is flipped", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub, voter := setupAttemptWithOneVoter(ctx, store)

		_, err := store.CastVote(ctx, pub.ID, voter, true)
		Expect(err).NotTo(HaveOccurred())
		result, err := store.CastVote(ctx, pub.ID, voter, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.IsChange).To(BeTrue())
		Expect(result.Vote).To(BeFalse())
	})

	It("returns is_change=false when re-affirming the same value", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub, voter := setupAttemptWithOneVoter(ctx, store)

		_, err := store.CastVote(ctx, pub.ID, voter, true)
		Expect(err).NotTo(HaveOccurred())
		result, err := store.CastVote(ctx, pub.ID, voter, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.IsChange).To(BeFalse())
	})

	It("preserves voted_at on first cast but advances last_changed_at on a later cast", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub, voter := setupAttemptWithOneVoter(ctx, store)

		_, err := store.CastVote(ctx, pub.ID, voter, true)
		Expect(err).NotTo(HaveOccurred())
		first, err := store.ListPublishVoters(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(first).To(HaveLen(1))
		Expect(first[0].VotedAt).NotTo(BeNil())
		Expect(first[0].LastChangedAt).NotTo(BeNil())
		votedAt := first[0].VotedAt.Time()

		time.Sleep(2 * time.Millisecond)
		_, err = store.CastVote(ctx, pub.ID, voter, false)
		Expect(err).NotTo(HaveOccurred())
		second, err := store.ListPublishVoters(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(second[0].VotedAt.Time()).To(BeTemporally("==", votedAt), "voted_at MUST be preserved across casts")
		Expect(second[0].LastChangedAt.Time()).To(BeTemporally(">", votedAt), "last_changed_at MUST advance")
	})

	It("rejects a vote from a non-roster character", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub, _ := setupAttemptWithOneVoter(ctx, store)

		_, err := store.CastVote(ctx, pub.ID, ulid.Make().String(), true)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "SCENE_PUBLISH_NOT_A_VOTER")
	})

	It("tallies correct yes/no/pending counts", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub := setupAttemptWith3Voters(ctx, store)
		voters, err := store.ListPublishVoters(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(voters).To(HaveLen(3))

		_, err = store.CastVote(ctx, pub.ID, voters[0].CharacterID, true)
		Expect(err).NotTo(HaveOccurred())
		_, err = store.CastVote(ctx, pub.ID, voters[1].CharacterID, false)
		Expect(err).NotTo(HaveOccurred())
		// voters[2] left pending.

		tally, err := store.TallyVotes(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(tally.Yes).To(Equal(1))
		Expect(tally.No).To(Equal(1))
		Expect(tally.Pending).To(Equal(1))
	})

	// holomush-wn612: CastVote locks the attempt row FOR UPDATE and re-validates
	// non-terminal status inside the tx, so a vote cannot land on an attempt that
	// resolved between the handler's status check and the vote write. This is the
	// terminal boundary of INV-SCENE-29 (votes are valid only during COLLECTING /
	// COOLOFF). A roster member is established BEFORE the terminal transition to
	// prove the rejection is the status guard, not the non-voter guard.
	It("rejects a vote on an ATTEMPT_FAILED attempt with SCENE_PUBLISH_INVALID_STATE", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub, voter := setupAttemptWithOneVoter(ctx, store)

		reason := FailureAnyNo
		Expect(store.TransitionStatus(ctx, pub.ID, TransitionInput{
			To: StatusAttemptFailed, FailureReason: &reason, Resolved: true,
		})).NotTo(HaveOccurred())

		_, err := store.CastVote(ctx, pub.ID, voter, true)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "SCENE_PUBLISH_INVALID_STATE")
	})

	It("rejects a vote on a PUBLISHED attempt with SCENE_PUBLISH_INVALID_STATE", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub, voter := setupAttemptWithOneVoter(ctx, store)

		// COLLECTING → COOLOFF → PUBLISHED (the only legal path to PUBLISHED).
		now := time.Now()
		Expect(store.TransitionStatus(ctx, pub.ID, TransitionInput{
			To: StatusCoolOff, SetCoolOffAt: &now,
		})).NotTo(HaveOccurred())
		Expect(store.TransitionStatus(ctx, pub.ID, TransitionInput{
			To: StatusPublished, Resolved: true,
		})).NotTo(HaveOccurred())

		_, err := store.CastVote(ctx, pub.ID, voter, true)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "SCENE_PUBLISH_INVALID_STATE")
	})

	It("rejects a vote on a nonexistent attempt with SCENE_PUBLISH_NOT_FOUND", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()

		// No attempt row exists, so the FOR UPDATE lock SELECT finds no row —
		// distinct from the NOT_A_VOTER path, which presupposes a live attempt.
		_, err := store.CastVote(ctx, ulid.Make().String(), ulid.Make().String(), true)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "SCENE_PUBLISH_NOT_FOUND")
	})
})

var _ = Describe("Publish store — status reads + transitions", func() {
	It("GetPublishedSceneHeader returns the attempt without content entries", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub, ownerID := setupAttemptWithOneVoter(ctx, store)

		got, err := store.GetPublishedSceneHeader(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
		Expect(got.ID).To(Equal(pub.ID))
		Expect(got.SceneID).To(Equal(pub.SceneID))
		Expect(got.Status).To(Equal(StatusCollecting))
		Expect(got.InitiatedBy).To(Equal(ownerID))
		Expect(got.AttemptNumber).To(Equal(1))
		Expect(got.VoteWindow).To(Equal(7*24*time.Hour), "INTERVAL must round-trip to the original duration")
		Expect(got.CoolOffWindow).To(Equal(30 * time.Minute))
		Expect(got.MaxAttemptsSnapshot).To(Equal(3))
		Expect(got.CoolOffStartedAt).To(BeNil())
		Expect(got.ContentEntries).To(BeNil(), "header read MUST NOT carry content (INV-SCENE-32)")
	})

	It("GetPublishedSceneHeader returns nil for a nonexistent id", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		got, err := store.GetPublishedSceneHeader(ctx, ulid.Make().String())
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeNil())
	})

	It("GetPublishedSceneContent returns nil for a non-PUBLISHED attempt", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub, _ := setupAttemptWithOneVoter(ctx, store)

		entries, err := store.GetPublishedSceneContent(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(BeNil(), "COLLECTING attempt has no content_entries yet")
	})

	It("TransitionStatus COLLECTING→COOLOFF sets cooloff_started_at", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub, _ := setupAttemptWithOneVoter(ctx, store)

		now := time.Now()
		Expect(store.TransitionStatus(ctx, pub.ID, TransitionInput{
			To: StatusCoolOff, SetCoolOffAt: &now,
		})).NotTo(HaveOccurred())

		got, err := store.GetPublishedSceneHeader(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Status).To(Equal(StatusCoolOff))
		Expect(got.CoolOffStartedAt).NotTo(BeNil())
	})

	It("TransitionStatus COOLOFF→COLLECTING clears cooloff_started_at", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub, _ := setupAttemptWithOneVoter(ctx, store)
		now := time.Now()
		Expect(store.TransitionStatus(ctx, pub.ID, TransitionInput{To: StatusCoolOff, SetCoolOffAt: &now})).NotTo(HaveOccurred())

		Expect(store.TransitionStatus(ctx, pub.ID, TransitionInput{
			To: StatusCollecting, ClearCoolOff: true,
		})).NotTo(HaveOccurred())

		got, err := store.GetPublishedSceneHeader(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Status).To(Equal(StatusCollecting))
		Expect(got.CoolOffStartedAt).To(BeNil(), "flip-back MUST clear cooloff_started_at")
	})

	It("TransitionStatus to ATTEMPT_FAILED sets resolved_at and failure_reason", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub, _ := setupAttemptWithOneVoter(ctx, store)

		reason := FailureAnyNo
		Expect(store.TransitionStatus(ctx, pub.ID, TransitionInput{
			To: StatusAttemptFailed, Resolved: true, FailureReason: &reason,
		})).NotTo(HaveOccurred())

		got, err := store.GetPublishedSceneHeader(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Status).To(Equal(StatusAttemptFailed))
		Expect(got.ResolvedAt).NotTo(BeNil())
		Expect(got.FailureReason).NotTo(BeNil())
		Expect(*got.FailureReason).To(Equal(FailureAnyNo))
	})

	It("TransitionStatus rejects an illegal transition with SCENE_PUBLISH_INVALID_TRANSITION", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub, _ := setupAttemptWithOneVoter(ctx, store)

		// COLLECTING → PUBLISHED is illegal (PUBLISHED is reachable only from COOLOFF).
		err := store.TransitionStatus(ctx, pub.ID, TransitionInput{To: StatusPublished, Resolved: true})
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "SCENE_PUBLISH_INVALID_TRANSITION")

		// Status is unchanged.
		got, err := store.GetPublishedSceneHeader(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Status).To(Equal(StatusCollecting))
	})

	It("LockForSnapshot returns a COOLOFF row under FOR UPDATE and rejects non-COOLOFF", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub, _ := setupAttemptWithOneVoter(ctx, store)

		// COLLECTING attempt: lock must reject with INVALID_STATE.
		tx1, err := store.pool.Begin(ctx)
		Expect(err).NotTo(HaveOccurred())
		_, err = store.LockForSnapshot(ctx, tx1, pub.ID)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "SCENE_PUBLISH_INVALID_STATE")
		Expect(tx1.Rollback(ctx)).NotTo(HaveOccurred())

		// Move to COOLOFF, then lock succeeds and returns the row.
		now := time.Now()
		Expect(store.TransitionStatus(ctx, pub.ID, TransitionInput{To: StatusCoolOff, SetCoolOffAt: &now})).NotTo(HaveOccurred())
		tx2, err := store.pool.Begin(ctx)
		Expect(err).NotTo(HaveOccurred())
		locked, err := store.LockForSnapshot(ctx, tx2, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(locked.ID).To(Equal(pub.ID))
		Expect(locked.Status).To(Equal(StatusCoolOff))
		Expect(tx2.Rollback(ctx)).NotTo(HaveOccurred())
	})

	It("CountAttempts returns total / active / published counts per scene", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub, _ := setupAttemptWithOneVoter(ctx, store)

		counts, err := store.CountAttempts(ctx, pub.SceneID)
		Expect(err).NotTo(HaveOccurred())
		Expect(counts.Total).To(Equal(1))
		Expect(counts.Active).To(Equal(1), "a COLLECTING attempt is active")
		Expect(counts.Published).To(Equal(0))
	})

	It("TransitionStatus COOLOFF→PUBLISHED sets published_at and resolved_at, and counts as published", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		pub, _ := setupAttemptWithOneVoter(ctx, store)

		now := time.Now()
		Expect(store.TransitionStatus(ctx, pub.ID, TransitionInput{To: StatusCoolOff, SetCoolOffAt: &now})).NotTo(HaveOccurred())
		Expect(store.TransitionStatus(ctx, pub.ID, TransitionInput{To: StatusPublished, Resolved: true})).NotTo(HaveOccurred())

		got, err := store.GetPublishedSceneHeader(ctx, pub.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Status).To(Equal(StatusPublished))
		Expect(got.PublishedAt).NotTo(BeNil(), "PUBLISHED transition MUST set published_at")
		Expect(got.ResolvedAt).NotTo(BeNil(), "PUBLISHED is terminal — resolved_at MUST be set")

		counts, err := store.CountAttempts(ctx, pub.SceneID)
		Expect(err).NotTo(HaveOccurred())
		Expect(counts.Published).To(Equal(1))
		Expect(counts.Active).To(Equal(0), "a PUBLISHED attempt is no longer active")
	})
})

var _ = Describe("Publish store — ReadSceneLogForSnapshot", func() {
	It("returns only pose/say/emit IC content in chronological order, excluding OOC, ops, and other scenes", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store := newTestStore()
		subject := "events.main.scene.scene-snaplog-1.ic"

		// scene_log.timestamp is BIGINT epoch-nanos (INV-STORE-1, migration 000007).
		// Ordering is by id ASC, so make the ids deterministically increasing:
		// a strictly-monotonic time component with zero entropy guarantees
		// insertion order == ORDER BY id ASC by construction, without depending
		// on ulid.Make()'s wall-clock + global monotonic entropy source.
		var seq uint64
		insertLog := func(subj, eventType string) {
			seq++
			var id ulid.ULID
			Expect(id.SetTime(seq)).NotTo(HaveOccurred())
			_, err := store.pool.Exec(ctx, `
				INSERT INTO scene_log (id, subject, type, timestamp, actor_kind, payload, schema_ver, codec)
				VALUES ($1, $2, $3, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, 'character', $4, 1, 'identity')`,
				id.Bytes(), subj, eventType, []byte(eventType))
			Expect(err).NotTo(HaveOccurred())
		}
		insertLog(subject, "core-scenes:scene_pose")
		insertLog(subject, "core-scenes:scene_ooc") // OOC — excluded
		insertLog(subject, "core-scenes:scene_say")
		insertLog(subject, "core-scenes:scene_leave_ic") // ops/notice — excluded
		insertLog(subject, "core-scenes:scene_emit")
		insertLog("events.main.scene.other.ic", "core-scenes:scene_pose") // different scene — excluded by subject

		tx, err := store.pool.Begin(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = tx.Rollback(ctx) }()

		logs, err := store.ReadSceneLogForSnapshot(ctx, tx, subject)
		Expect(err).NotTo(HaveOccurred())
		Expect(logs).To(HaveLen(3), "only pose/say/emit for this subject; OOC + ops + other-scene excluded")
		Expect(logs[0].Type).To(Equal("core-scenes:scene_pose"))
		Expect(logs[1].Type).To(Equal("core-scenes:scene_say"))
		Expect(logs[2].Type).To(Equal("core-scenes:scene_emit"), "chronological order preserved via ULID id ASC")
		Expect(logs[0].Codec).To(Equal("identity"))
		Expect(logs[0].DEKRef).To(BeNil(), "identity-codec rows have NULL dek_ref")
	})
})

// ── ListBoard store integration tests (iokti.12) ─────────────────────────────

var _ = Describe("SceneStore.ListBoard", func() {
	var (
		store   *SceneStore
		ctx     context.Context
		ownerID string
	)

	BeforeEach(func() {
		store = newTestStore()
		ctx = context.Background()
		ownerID = ulid.Make().String()
	})

	// mustCreateSceneWithState seeds a scene with arbitrary state, visibility
	// and tags via the store's Create path (minimal, no participant side-effects).
	mustCreateSceneWithState := func(id, state, visibility string, tags []string) *SceneRow {
		GinkgoHelper()
		row := &SceneRow{
			ID:              id,
			Title:           "Scene " + id,
			OwnerID:         ownerID,
			State:           state,
			PoseOrder:       string(PoseOrderModeFree),
			Visibility:      visibility,
			ContentWarnings: []string{},
			Tags:            tags,
		}
		Expect(store.Create(ctx, row)).NotTo(HaveOccurred())
		return row
	}

	It("returns only open+active/paused scenes with matching tag, excluding private and ended", func() {
		plotTagged := mustCreateSceneWithState(ulid.Make().String(), "active", "open", []string{"plot"})
		// Excluded: private visibility (even if active + tagged).
		mustCreateSceneWithState(ulid.Make().String(), "active", "private", []string{"plot"})
		// Excluded: ended state (even if open + tagged).
		mustCreateSceneWithState(ulid.Make().String(), "ended", "open", []string{"plot"})
		// Excluded: open + active but no "plot" tag.
		mustCreateSceneWithState(ulid.Make().String(), "active", "open", []string{"social"})

		rows, err := store.ListBoard(ctx, BoardQuery{Tags: []string{"plot"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].ID).To(Equal(plotTagged.ID))
	})

	It("includes both active and paused scenes when no tag filter is applied", func() {
		active := mustCreateSceneWithState(ulid.Make().String(), "active", "open", []string{})
		paused := mustCreateSceneWithState(ulid.Make().String(), "paused", "open", []string{})
		// Excluded: ended.
		mustCreateSceneWithState(ulid.Make().String(), "ended", "open", []string{})

		rows, err := store.ListBoard(ctx, BoardQuery{})
		Expect(err).NotTo(HaveOccurred())
		ids := make([]string, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		Expect(ids).To(ContainElements(active.ID, paused.ID))
		for _, r := range rows {
			Expect(r.State).To(BeElementOf("active", "paused"))
		}
	})

	It("excludes scenes that have some but not all requested tags", func() {
		// Both tags: should be included.
		both := mustCreateSceneWithState(ulid.Make().String(), "active", "open", []string{"plot", "action"})
		// Only one tag: excluded by the @> containment requirement.
		mustCreateSceneWithState(ulid.Make().String(), "active", "open", []string{"plot"})

		rows, err := store.ListBoard(ctx, BoardQuery{Tags: []string{"plot", "action"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].ID).To(Equal(both.ID))
	})

	It("respects Limit and Offset for pagination", func() {
		// Seed three open+active scenes.
		for i := 0; i < 3; i++ {
			mustCreateSceneWithState(ulid.Make().String(), "active", "open", []string{})
		}

		// First page: limit 2.
		page1, err := store.ListBoard(ctx, BoardQuery{Limit: 2, Offset: 0})
		Expect(err).NotTo(HaveOccurred())
		Expect(page1).To(HaveLen(2))

		// Second page: offset 2, should return the remaining 1.
		page2, err := store.ListBoard(ctx, BoardQuery{Limit: 2, Offset: 2})
		Expect(err).NotTo(HaveOccurred())
		Expect(page2).To(HaveLen(1))

		// No scene appears on both pages.
		page1IDs := map[string]bool{}
		for _, r := range page1 {
			page1IDs[r.ID] = true
		}
		for _, r := range page2 {
			Expect(page1IDs[r.ID]).To(BeFalse(), "scene %s appears in both pages", r.ID)
		}
	})

	It("normalises zero Limit to defaultBoardLimit and does not panic on empty result", func() {
		rows, err := store.ListBoard(ctx, BoardQuery{Limit: 0})
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(BeEmpty(), "no scenes seeded, so empty result is expected")
	})
})

// ── ListBoard CW exclusion integration tests (iokti.13) ─────────────────────

var _ = Describe("SceneStore.ListBoard CW exclusion", func() {
	var (
		store   *SceneStore
		ctx     context.Context
		ownerID string
	)

	BeforeEach(func() {
		store = newTestStore()
		ctx = context.Background()
		ownerID = ulid.Make().String()
	})

	// mustCreateBoardScene seeds an open+active scene with the given CW tags.
	mustCreateBoardScene := func(id string, cws []string) *SceneRow {
		GinkgoHelper()
		row := &SceneRow{
			ID:              id,
			Title:           "Board Scene " + id,
			OwnerID:         ownerID,
			State:           string(SceneStateActive),
			PoseOrder:       string(PoseOrderModeFree),
			Visibility:      "open",
			ContentWarnings: cws,
			Tags:            []string{},
		}
		Expect(store.Create(ctx, row)).NotTo(HaveOccurred())
		return row
	}

	It("excludes a scene whose content_warnings overlap the blocked set", func() {
		blocked := mustCreateBoardScene(ulid.Make().String(), []string{"death", "violence"})
		safe := mustCreateBoardScene(ulid.Make().String(), []string{"romance"})
		noTags := mustCreateBoardScene(ulid.Make().String(), []string{})

		rows, err := store.ListBoard(ctx, BoardQuery{BlockedCW: []string{"death"}})
		Expect(err).NotTo(HaveOccurred())

		ids := make([]string, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		// scene with "death" is excluded (overlap); others are kept.
		Expect(ids).NotTo(ContainElement(blocked.ID),
			"scene with blocked CW 'death' must be excluded")
		Expect(ids).To(ContainElement(safe.ID))
		Expect(ids).To(ContainElement(noTags.ID))
	})

	It("keeps all scenes when BlockedCW is empty", func() {
		a := mustCreateBoardScene(ulid.Make().String(), []string{"death"})
		b := mustCreateBoardScene(ulid.Make().String(), []string{"romance"})

		rows, err := store.ListBoard(ctx, BoardQuery{BlockedCW: []string{}})
		Expect(err).NotTo(HaveOccurred())

		ids := make([]string, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		Expect(ids).To(ContainElement(a.ID))
		Expect(ids).To(ContainElement(b.ID))
	})

	It("keeps all scenes when BlockedCW is nil", func() {
		a := mustCreateBoardScene(ulid.Make().String(), []string{"violence"})
		b := mustCreateBoardScene(ulid.Make().String(), []string{})

		rows, err := store.ListBoard(ctx, BoardQuery{BlockedCW: nil})
		Expect(err).NotTo(HaveOccurred())

		ids := make([]string, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		Expect(ids).To(ContainElement(a.ID))
		Expect(ids).To(ContainElement(b.ID))
	})

	It("excludes a scene that shares any one CW from a multi-element block set", func() {
		// Scene has both death and romance; block set contains only romance.
		blocked := mustCreateBoardScene(ulid.Make().String(), []string{"death", "romance"})
		// Scene has only violence — no overlap with the block set.
		safe := mustCreateBoardScene(ulid.Make().String(), []string{"violence"})

		rows, err := store.ListBoard(ctx, BoardQuery{BlockedCW: []string{"romance"}})
		Expect(err).NotTo(HaveOccurred())

		ids := make([]string, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		Expect(ids).NotTo(ContainElement(blocked.ID),
			"scene sharing any blocked CW must be excluded")
		Expect(ids).To(ContainElement(safe.ID))
	})

	It("keeps content_warnings on returned scenes (INV-SCENE-56: CW labels not stripped)", func() {
		safe := mustCreateBoardScene(ulid.Make().String(), []string{"romance", "violence"})

		// Block "death" — safe scene has no overlap, so it is returned.
		rows, err := store.ListBoard(ctx, BoardQuery{BlockedCW: []string{"death"}})
		Expect(err).NotTo(HaveOccurred())

		var found *SceneRow
		for _, r := range rows {
			if r.ID == safe.ID {
				found = r
				break
			}
		}
		Expect(found).NotTo(BeNil(), "safe scene must appear in board results")
		Expect(found.ContentWarnings).To(ConsistOf("romance", "violence"),
			"INV-SCENE-56: content_warnings must not be stripped from returned rows")
	})
})

var _ = Describe("AddObserver", func() {
	It("inserts observer row for open active scene and GetParticipant returns it", func() {
		store := newTestStore()
		ctx := context.Background()

		row := &SceneRow{
			ID: "scene-ao-1", Title: "T", OwnerID: "char-alice",
			State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
			Visibility:      string(SceneVisibilityOpen),
			ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

		got, result, err := store.AddObserver(ctx, row.ID, "char-watcher")
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ObserverAdded))
		Expect(got.CharacterID).To(Equal("char-watcher"))
		Expect(got.Role).To(Equal("observer"))

		p, err := store.GetParticipant(ctx, row.ID, "char-watcher")
		Expect(err).NotTo(HaveOccurred())
		Expect(p.Role).To(Equal("observer"))
	})

	It("returns ObserverSceneNotOpen for a private scene", func() {
		store := newTestStore()
		ctx := context.Background()

		row := &SceneRow{
			ID: "scene-ao-priv", Title: "T", OwnerID: "char-alice",
			State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
			Visibility:      string(SceneVisibilityPrivate),
			ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())

		_, result, err := store.AddObserver(ctx, row.ID, "char-watcher")
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ObserverSceneNotOpen))
	})

	It("returns ObserverSceneNotActive for an ended scene", func() {
		store := newTestStore()
		ctx := context.Background()

		row := &SceneRow{
			ID: "scene-ao-ended", Title: "T", OwnerID: "char-alice",
			State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
			Visibility:      string(SceneVisibilityOpen),
			ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
		_, err := store.End(ctx, row.ID)
		Expect(err).NotTo(HaveOccurred())

		_, result, err := store.AddObserver(ctx, row.ID, "char-watcher")
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ObserverSceneNotActive))
	})

	It("returns ObserverAlreadyParticipant for an existing member without changing the row", func() {
		store := newTestStore()
		ctx := context.Background()

		row := &SceneRow{
			ID: "scene-ao-mem", Title: "T", OwnerID: "char-alice",
			State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
			Visibility:      string(SceneVisibilityOpen),
			ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
		_, _, err := store.AddParticipant(ctx, row.ID, "char-bob")
		Expect(err).NotTo(HaveOccurred())

		_, result, err := store.AddObserver(ctx, row.ID, "char-bob")
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ObserverAlreadyParticipant))

		// Row must be unchanged (still member, not observer).
		p, err := store.GetParticipant(ctx, row.ID, "char-bob")
		Expect(err).NotTo(HaveOccurred())
		Expect(p.Role).To(Equal("member"))
	})

	It("scene load returns observer under Observers field and not in Participants", func() {
		store := newTestStore()
		ctx := context.Background()

		row := &SceneRow{
			ID: "scene-ao-load", Title: "T", OwnerID: "char-alice",
			State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
			Visibility:      string(SceneVisibilityOpen),
			ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
		_, result, err := store.AddObserver(ctx, row.ID, "char-watcher")
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ObserverAdded))

		_, participants, invitees, observers, err := store.GetWithMembershipAndObservers(ctx, row.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(participants).To(ConsistOf("char-alice")) // owner only
		Expect(invitees).To(BeEmpty())
		Expect(observers).To(ConsistOf("char-watcher"))
	})

	It("upgrades observer to member via AddParticipant returning ParticipantUpgraded", func() {
		store := newTestStore()
		ctx := context.Background()

		row := &SceneRow{
			ID: "scene-ao-upgrade", Title: "T", OwnerID: "char-alice",
			State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
			Visibility:      string(SceneVisibilityOpen),
			ContentWarnings: []string{}, Tags: []string{},
		}
		Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
		_, aoResult, err := store.AddObserver(ctx, row.ID, "char-watcher")
		Expect(err).NotTo(HaveOccurred())
		Expect(aoResult).To(Equal(ObserverAdded))

		got, apResult, err := store.AddParticipant(ctx, row.ID, "char-watcher")
		Expect(err).NotTo(HaveOccurred())
		Expect(apResult).To(Equal(ParticipantUpgraded))
		Expect(got.Role).To(Equal("member"))

		p, err := store.GetParticipant(ctx, row.ID, "char-watcher")
		Expect(err).NotTo(HaveOccurred())
		Expect(p.Role).To(Equal("member"))
	})

	It("returns ObserverSceneNotFound for a nonexistent scene ID", func() {
		store := newTestStore()
		ctx := context.Background()

		_, result, err := store.AddObserver(ctx, "scene-does-not-exist", "char-watcher")
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ObserverSceneNotFound))
	})
})

// ── ListCharacterScenes integration tests ─────────────────────────────────────

var _ = Describe("SceneStore.ListCharacterScenes", func() {
	// gameID used for IC subject construction — must match the prefix passed
	// to ListCharacterScenes. Tests use "test-game" directly.
	const testGameID = "test-game"
	const icPrefix = "events." + testGameID + ".scene."

	// seedSceneLogRow inserts a minimal identity-codec scene_log row under
	// the given IC subject so the activity aggregates have something to count.
	seedLogRow := func(store *SceneStore, subject string, tsNS int64) {
		GinkgoHelper()
		id := newPoseULID()
		_, err := store.Pool().Exec(
			context.Background(), `
			INSERT INTO scene_log (id, subject, type, timestamp, actor_kind, actor_id, payload, schema_ver, codec)
			VALUES ($1, $2, 'core-scenes:scene_pose', $3, 'character', $4, $5, 1, 'identity')`,
			id, subject, tsNS, []byte("char-alice"), []byte(`{}`),
		)
		Expect(err).NotTo(HaveOccurred())
	}

	It("returns both member and observer scenes ordered by last_activity_ms DESC", func() {
		store := newTestStore()
		ctx := context.Background()

		memberScene := mustCreateScene(store, "lcs-scene-member-01", "char-owner", string(SceneVisibilityOpen))
		observerScene := mustCreateScene(store, "lcs-scene-observer-01", "char-owner", string(SceneVisibilityOpen))

		// char-alice is a member on scene 1, observer on scene 2.
		mustAddParticipant(store, memberScene.ID, "char-alice", "member")
		mustAddParticipant(store, observerScene.ID, "char-alice", "observer")

		// Seed scene_log rows: observer scene is newer (higher epoch-ms → higher last_activity).
		olderNS := time.Now().Add(-2 * time.Hour).UnixNano()
		newerNS := time.Now().Add(-1 * time.Hour).UnixNano()
		seedLogRow(store, icPrefix+memberScene.ID+".ic", olderNS)
		seedLogRow(store, icPrefix+observerScene.ID+".ic", newerNS)

		results, err := store.ListCharacterScenes(ctx, "char-alice", icPrefix)
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(HaveLen(2))

		// Observer scene is newer → appears first.
		Expect(results[0].Scene.ID).To(Equal(observerScene.ID))
		Expect(results[0].Role).To(Equal("observer"))
		Expect(results[0].LastActivityMS).To(BeNumerically(">", 0))
		Expect(results[0].EntryCount).To(BeNumerically("==", 1))

		Expect(results[1].Scene.ID).To(Equal(memberScene.ID))
		Expect(results[1].Role).To(Equal("member"))
		Expect(results[1].LastActivityMS).To(BeNumerically(">", 0))
		Expect(results[1].LastActivityMS).To(BeNumerically("<", results[0].LastActivityMS))
	})

	It("excludes archived scenes", func() {
		store := newTestStore()
		ctx := context.Background()

		activeScene := mustCreateScene(store, "lcs-archived-active-01", "char-owner", string(SceneVisibilityOpen))
		archivedScene := mustCreateScene(store, "lcs-archived-scene-01", "char-owner", string(SceneVisibilityOpen))

		mustAddParticipant(store, activeScene.ID, "char-bob", "member")
		mustAddParticipant(store, archivedScene.ID, "char-bob", "member")

		// Archive scene by setting archived_at to a non-null epoch-ns.
		_, err := store.Pool().Exec(
			ctx,
			`UPDATE scenes SET archived_at = $1 WHERE id = $2`,
			time.Now().UnixNano(), archivedScene.ID,
		)
		Expect(err).NotTo(HaveOccurred())

		results, err := store.ListCharacterScenes(ctx, "char-bob", icPrefix)
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(HaveLen(1))
		Expect(results[0].Scene.ID).To(Equal(activeScene.ID))
	})

	It("returns entry_count zero when no scene_log rows exist", func() {
		store := newTestStore()
		ctx := context.Background()

		emptyScene := mustCreateScene(store, "lcs-empty-log-01", "char-owner", string(SceneVisibilityOpen))
		mustAddParticipant(store, emptyScene.ID, "char-carol", "member")

		results, err := store.ListCharacterScenes(ctx, "char-carol", icPrefix)
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(HaveLen(1))
		Expect(results[0].EntryCount).To(BeNumerically("==", 0))
		Expect(results[0].LastActivityMS).To(BeNumerically("==", 0))
	})

	It("returns empty slice when character has no participant rows", func() {
		store := newTestStore()
		ctx := context.Background()

		results, err := store.ListCharacterScenes(ctx, "char-nobody", icPrefix)
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(BeEmpty())
	})
})

// ── ListPublishedScenes integration tests ─────────────────────────────────────

var _ = Describe("SceneStore.ListPublishedScenes", func() {
	// seedPublishedScene inserts a scene + a PUBLISHED published_scenes row
	// with the given published_at epoch-ns so the list query has rows.
	seedPublishedScene := func(store *SceneStore, sceneID, pubID string, tags []string, publishedAtNS int64) {
		GinkgoHelper()
		ctx := context.Background()
		row := &SceneRow{
			ID: sceneID, Title: "Pub " + sceneID, OwnerID: "char-pub-owner",
			State:           string(SceneStateEnded),
			PoseOrder:       string(PoseOrderModeFree),
			Visibility:      string(SceneVisibilityOpen),
			ContentWarnings: []string{},
			Tags:            tags,
		}
		Expect(store.Create(ctx, row)).NotTo(HaveOccurred())

		// Insert directly — CreatePublishAttempt starts in COLLECTING; we
		// need PUBLISHED status and a specific published_at for ordering tests.
		_, err := store.Pool().Exec(
			ctx, `
			INSERT INTO published_scenes
			  (id, scene_id, attempt_number, status, initiated_by, initiated_at,
			   vote_window, cooloff_window, max_attempts_snapshot,
			   title_snapshot, participants_snapshot, published_at, content_entries)
			VALUES ($1, $2, 1, 'PUBLISHED', 'char-pub-owner',
			        $3, '7 days', '30 minutes', 3,
			        $4, '[]', $3, '[]')`,
			pubID, sceneID, publishedAtNS, "Pub "+sceneID,
		)
		Expect(err).NotTo(HaveOccurred())
	}

	It("returns only PUBLISHED rows ordered by published_at DESC", func() {
		store := newTestStore()

		older := time.Now().Add(-2 * time.Hour).UnixNano()
		newer := time.Now().Add(-1 * time.Hour).UnixNano()
		seedPublishedScene(store, "lps-scene-a", "lps-pub-a", []string{}, older)
		seedPublishedScene(store, "lps-scene-b", "lps-pub-b", []string{}, newer)

		// Also insert a non-PUBLISHED row to verify it is excluded.
		_, err := store.Pool().Exec(
			context.Background(), `
			INSERT INTO published_scenes
			  (id, scene_id, attempt_number, status, initiated_by, initiated_at,
			   vote_window, cooloff_window, max_attempts_snapshot)
			VALUES ('lps-collecting', 'lps-scene-a', 2, 'COLLECTING', 'char-pub-owner',
			        $1, '7 days', '30 minutes', 3)`,
			time.Now().UnixNano(),
		)
		Expect(err).NotTo(HaveOccurred())

		results, err := store.ListPublishedScenes(context.Background(), ListPublishedScenesQuery{Limit: 10})
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(HaveLen(2))
		// Newest first.
		Expect(results[0].ID).To(Equal("lps-pub-b"))
		Expect(results[1].ID).To(Equal("lps-pub-a"))
	})

	It("filters by tags when tags are specified", func() {
		store := newTestStore()

		seedPublishedScene(store, "lps-tag-scene-a", "lps-tag-pub-a", []string{"drama", "romance"}, time.Now().UnixNano())
		seedPublishedScene(store, "lps-tag-scene-b", "lps-tag-pub-b", []string{"drama"}, time.Now().UnixNano())
		seedPublishedScene(store, "lps-tag-scene-c", "lps-tag-pub-c", []string{"comedy"}, time.Now().UnixNano())

		results, err := store.ListPublishedScenes(context.Background(), ListPublishedScenesQuery{
			Limit: 10,
			Tags:  []string{"drama"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(HaveLen(2))

		ids := []string{results[0].ID, results[1].ID}
		Expect(ids).To(ContainElement("lps-tag-pub-a"))
		Expect(ids).To(ContainElement("lps-tag-pub-b"))
	})

	It("respects limit and offset pagination", func() {
		store := newTestStore()

		for i := range 5 {
			ts := time.Now().Add(time.Duration(-i) * time.Hour).UnixNano()
			seedPublishedScene(
				store,
				"lps-page-scene-"+string(rune('a'+i)),
				"lps-page-pub-"+string(rune('a'+i)),
				[]string{}, ts,
			)
		}

		first, err := store.ListPublishedScenes(context.Background(), ListPublishedScenesQuery{Limit: 2, Offset: 0})
		Expect(err).NotTo(HaveOccurred())
		Expect(first).To(HaveLen(2))

		second, err := store.ListPublishedScenes(context.Background(), ListPublishedScenesQuery{Limit: 2, Offset: 2})
		Expect(err).NotTo(HaveOccurred())
		Expect(second).To(HaveLen(2))

		// No overlap.
		Expect(first[0].ID).NotTo(Equal(second[0].ID))
		Expect(first[1].ID).NotTo(Equal(second[1].ID))
	})
})
