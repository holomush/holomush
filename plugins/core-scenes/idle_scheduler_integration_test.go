// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// idleScheduler integration tests (Plan 02-06, D-06).
//
// These exercise ListScenesIdlePastThreshold + idleScheduler.sweep against a
// real Postgres store: the threshold boundary (inclusive), the per-scene
// idle_timeout_secs override beating the game-wide default, paused-exclusion,
// and the active→paused transition with no re-transition (INV-SCENE-71).
//
// The scheduler's `now` clock is injected so tests drive deterministic time
// relative to each scene's created_at (the idle basis when the scene has no IC
// log yet); sweep(ctx) is called directly.
package main

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// newIdleScene creates an active, IC-log-free scene and returns its id plus its
// created_at in epoch-nanoseconds (the idle basis). idleTimeoutSecs, when
// non-nil, sets the per-scene idle_timeout_secs override column.
func newIdleScene(ctx context.Context, store *SceneStore, id, owner string, idleTimeoutSecs *int) int64 {
	GinkgoHelper()
	row := &SceneRow{
		ID: id, Title: "Idle Scene", OwnerID: owner,
		State: string(SceneStateActive), PoseOrder: string(PoseOrderModeFree),
		Visibility: string(SceneVisibilityOpen), IdleTimeoutSecs: idleTimeoutSecs,
		ContentWarnings: []string{}, Tags: []string{},
	}
	Expect(store.CreateWithOwner(ctx, row)).NotTo(HaveOccurred())
	got, err := store.Get(ctx, id)
	Expect(err).NotTo(HaveOccurred())
	return got.CreatedAt.Time().UnixNano()
}

func intPtr(v int) *int { return &v }

var _ = Describe("SceneStore.ListScenesIdlePastThreshold", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		store  *SceneStore
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)
		store = newTestStore()
	})
	AfterEach(func() { cancel() })

	It("returns an active scene at exactly the idle threshold (inclusive boundary)", func() {
		const timeoutSecs = 60
		createdNs := newIdleScene(ctx, store, "01IDLE_BOUNDARY_AT0000000A", "01IDLE_BOUNDARYOWNER000A", nil)
		thresholdNs := createdNs + int64(timeoutSecs)*int64(time.Second)

		idle, err := store.ListScenesIdlePastThreshold(ctx, thresholdNs, timeoutSecs)
		Expect(err).NotTo(HaveOccurred())
		ids := idleIDs(idle)
		Expect(ids).To(ContainElement("01IDLE_BOUNDARY_AT0000000A"),
			"a scene at exactly last_activity + idle_timeout is idle (inclusive)")
	})

	It("does not return an active scene one nanosecond before its threshold", func() {
		const timeoutSecs = 60
		createdNs := newIdleScene(ctx, store, "01IDLE_BOUNDARY_BEFORE00A", "01IDLE_BOUNDARYOWNER000A", nil)
		justBeforeNs := createdNs + int64(timeoutSecs)*int64(time.Second) - 1

		idle, err := store.ListScenesIdlePastThreshold(ctx, justBeforeNs, timeoutSecs)
		Expect(err).NotTo(HaveOccurred())
		Expect(idleIDs(idle)).NotTo(ContainElement("01IDLE_BOUNDARY_BEFORE00A"),
			"a scene not yet past threshold must not be returned")
	})

	It("honors a per-scene idle_timeout_secs override beating the game default", func() {
		const (
			defaultSecs  = 100000 // huge default → scene would NOT be idle under it
			overrideSecs = 10     // small per-scene override → scene IS idle
		)
		createdNs := newIdleScene(ctx, store, "01IDLE_OVERRIDE0000000000A", "01IDLE_OVERRIDEOWNER0000A", intPtr(overrideSecs))
		nowNs := createdNs + int64(overrideSecs)*int64(time.Second) + int64(time.Millisecond)

		idle, err := store.ListScenesIdlePastThreshold(ctx, nowNs, defaultSecs)
		Expect(err).NotTo(HaveOccurred())
		Expect(idleIDs(idle)).To(ContainElement("01IDLE_OVERRIDE0000000000A"),
			"the per-scene idle_timeout_secs override (COALESCE) beats the injected default")
	})

	It("never returns a paused scene", func() {
		const timeoutSecs = 1
		createdNs := newIdleScene(ctx, store, "01IDLE_PAUSED00000000000A", "01IDLE_PAUSEDOWNER00000A", nil)
		_, err := store.Pause(ctx, "01IDLE_PAUSED00000000000A")
		Expect(err).NotTo(HaveOccurred())
		wellPastNs := createdNs + int64(timeoutSecs)*int64(time.Second) + int64(time.Hour)

		idle, err := store.ListScenesIdlePastThreshold(ctx, wellPastNs, timeoutSecs)
		Expect(err).NotTo(HaveOccurred())
		Expect(idleIDs(idle)).NotTo(ContainElement("01IDLE_PAUSED00000000000A"),
			"a paused scene is excluded from the idle sweep (no re-transition)")
	})
})

var _ = Describe("idleScheduler.sweep", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		store  *SceneStore
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)
		store = newTestStore()
	})
	AfterEach(func() { cancel() })

	// Verifies: INV-SCENE-71
	It("transitions an active idle scene to paused and does not re-transition it", func() {
		const timeoutSecs = 30
		createdNs := newIdleScene(ctx, store, "01IDLE_SWEEP000000000000A", "01IDLE_SWEEPOWNER000000A", nil)
		pastNs := createdNs + int64(timeoutSecs)*int64(time.Second) + int64(time.Millisecond)

		sched := &idleScheduler{
			store:                  store,
			interval:               time.Minute, // unused in direct sweep calls
			defaultIdleTimeoutSecs: timeoutSecs,
			now:                    func() time.Time { return time.Unix(0, pastNs) },
		}

		Expect(sched.sweep(ctx)).To(Succeed())
		got, err := store.Get(ctx, "01IDLE_SWEEP000000000000A")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.State).To(Equal(string(SceneStatePaused)),
			"INV-SCENE-71: an active scene idle past its threshold transitions to paused")

		// A second sweep must not re-transition the now-paused scene: the query
		// excludes paused rows, so the scene stays paused.
		Expect(sched.sweep(ctx)).To(Succeed())
		got, err = store.Get(ctx, "01IDLE_SWEEP000000000000A")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.State).To(Equal(string(SceneStatePaused)),
			"INV-SCENE-71: a paused scene is never re-transitioned by the sweep")
	})

	It("leaves an active scene still within its idle window untouched", func() {
		const timeoutSecs = 3600
		createdNs := newIdleScene(ctx, store, "01IDLE_SWEEP_NOTYET00000A", "01IDLE_SWEEPOWNER000000A", nil)
		withinNs := createdNs + int64(time.Second) // 1s into a 1h window

		sched := &idleScheduler{
			store:                  store,
			interval:               time.Minute,
			defaultIdleTimeoutSecs: timeoutSecs,
			now:                    func() time.Time { return time.Unix(0, withinNs) },
		}

		Expect(sched.sweep(ctx)).To(Succeed())
		got, err := store.Get(ctx, "01IDLE_SWEEP_NOTYET00000A")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.State).To(Equal(string(SceneStateActive)),
			"a scene not past its idle threshold must remain active")
	})
})

// idleIDs projects the scene ids from an idle-sweep result.
func idleIDs(scenes []idleScene) []string {
	ids := make([]string, 0, len(scenes))
	for _, s := range scenes {
		ids = append(ids, s.ID)
	}
	return ids
}
