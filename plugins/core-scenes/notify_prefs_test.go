// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

var _ = Describe("SceneStore notify prefs", func() {
	ctx := context.Background()

	Describe("SetSceneMute and ListMutedScenes", func() {
		It("includes a scene after it is muted and excludes it after unmute", func() {
			store := newTestStore()

			Expect(store.SetSceneMute(ctx, "char-1", "scene-a", true)).NotTo(HaveOccurred())
			muted, err := store.ListMutedScenes(ctx, "char-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(muted).To(ContainElement("scene-a"))

			Expect(store.SetSceneMute(ctx, "char-1", "scene-a", false)).NotTo(HaveOccurred())
			muted, err = store.ListMutedScenes(ctx, "char-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(muted).NotTo(ContainElement("scene-a"))
		})

		It("is idempotent when muting the same scene twice", func() {
			store := newTestStore()

			Expect(store.SetSceneMute(ctx, "char-1", "scene-a", true)).NotTo(HaveOccurred())
			Expect(store.SetSceneMute(ctx, "char-1", "scene-a", true)).NotTo(HaveOccurred())

			muted, err := store.ListMutedScenes(ctx, "char-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(muted).To(ConsistOf("scene-a"))
		})

		It("returns only the querying character's muted scenes", func() {
			store := newTestStore()

			Expect(store.SetSceneMute(ctx, "char-1", "scene-a", true)).NotTo(HaveOccurred())
			Expect(store.SetSceneMute(ctx, "char-2", "scene-b", true)).NotTo(HaveOccurred())

			muted, err := store.ListMutedScenes(ctx, "char-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(muted).To(ConsistOf("scene-a"))
			Expect(muted).NotTo(ContainElement("scene-b"))
		})
	})

	Describe("SetSceneNotifyPref and GetSceneNotifyPref", func() {
		It("defaults to notify enabled with realtime mode when no row exists", func() {
			store := newTestStore()

			enabled, mode, err := store.GetSceneNotifyPref(ctx, "char-new")
			Expect(err).NotTo(HaveOccurred())
			Expect(enabled).To(BeTrue())
			Expect(mode).To(Equal("realtime"))
		})

		It("persists a disabled global notify preference and round-trips it", func() {
			store := newTestStore()

			Expect(store.SetSceneNotifyPref(ctx, "char-1", false)).NotTo(HaveOccurred())
			enabled, mode, err := store.GetSceneNotifyPref(ctx, "char-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(enabled).To(BeFalse())
			Expect(mode).To(Equal("realtime"))
		})

		It("re-enables notifications idempotently after disabling", func() {
			store := newTestStore()

			Expect(store.SetSceneNotifyPref(ctx, "char-1", false)).NotTo(HaveOccurred())
			Expect(store.SetSceneNotifyPref(ctx, "char-1", true)).NotTo(HaveOccurred())

			enabled, _, err := store.GetSceneNotifyPref(ctx, "char-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(enabled).To(BeTrue())
		})

		It("keeps the global notify pref independent of per-scene mutes", func() {
			store := newTestStore()

			// A per-scene mute must not flip the global notify pref off.
			Expect(store.SetSceneMute(ctx, "char-1", "scene-a", true)).NotTo(HaveOccurred())

			enabled, _, err := store.GetSceneNotifyPref(ctx, "char-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(enabled).To(BeTrue())

			muted, err := store.ListMutedScenes(ctx, "char-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(muted).To(ConsistOf("scene-a"))
		})
	})
})
