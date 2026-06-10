//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package crypto_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// Verifies: INV-CRYPTO-121
var _ = Describe("Scene DEK genesis-on-first-focus", func() {
	It("first EnsureParticipant on a never-posed scene genesises the DEK and seeds the reader", func() {
		ctx := context.Background()
		h := buildCommsHarness(suiteT) // provides h.dekMgr backed by a real Postgres pool

		// A scene that has never been posed to: no active DEK row exists.
		sceneCtx := dek.ContextID{Type: "scene", ID: "01FRESHSCENEFOCUS1"}
		reader := dek.Participant{
			PlayerID:    "01PLAYERFOCUS00001",
			CharacterID: "01CHARFOCUS000001",
			AddedVia:    "sceneaccess.SetSceneFocus",
		}

		// The production focus seed path. Before the fix this returned ErrNoRows.
		Expect(h.dekMgr.EnsureParticipant(ctx, sceneCtx, reader)).To(Succeed())

		// The scene DEK now exists with the reader as a participant.
		key, err := h.dekMgr.GetOrCreate(ctx, sceneCtx, []dek.Participant{})
		Expect(err).NotTo(HaveOccurred())
		parts, err := h.dekMgr.Participants(ctx, key.ID, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(parts).To(HaveLen(1))
		Expect(parts[0].CharacterID).To(Equal(reader.CharacterID))
	})
})
