// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// INV-Y5INX-1 / INV-Y5INX-5: the production CreateScene RPC mints a bare ULID
// scene id — no "scene-" (or any) prefix. This guards against reintroducing a
// type-tag prefix on entity ids (the bare-ULID identity convention).
var _ = Describe("INV-Y5INX-1/5: CreateScene mints a bare ULID", func() {
	It("returns an id with no scene- prefix that parses as a ULID", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		ts := integrationtest.Start(suiteT, integrationtest.WithInTreePlugins(), integrationtest.WithPluginCrypto())
		defer ts.Stop()

		alice := ts.ConnectAuthed(ctx, "Alice")
		loc := ts.NewLocation(ctx)

		// Call the RPC directly (not the helper) so we assert on the raw id the
		// server returns, not the harness-parsed value. Mirrors session.go:464.
		resp, err := ts.SceneServiceClient().CreateScene(ctx, &scenev1.CreateSceneRequest{
			CharacterId: alice.CharacterID.String(),
			Title:       "guard scene",
			LocationId:  loc.String(),
			Visibility:  "open",
		})
		Expect(err).NotTo(HaveOccurred())
		rawID := resp.GetScene().GetId()

		Expect(strings.HasPrefix(rawID, "scene-")).To(BeFalse(),
			"INV-Y5INX-1/5: scene id MUST be a bare ULID, not scene- prefixed")
		_, perr := ulid.Parse(rawID)
		Expect(perr).NotTo(HaveOccurred(), "scene id MUST parse as a bare ULID")
	})
})
