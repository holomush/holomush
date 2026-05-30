// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package crypto_test — Phase 3d Task 11: INV-49 envelope round-trip
// targeted proof.
//
// INV-49: events_audit.envelope is byte-equal to the bus envelope across
// emit → audit projection → cold-read for both character (plugin emit on
// behalf of a character binding) and plugin (Actor.ID-bearing ULID)
// actors. This test is the end-to-end lock for the regression Decision 5
// fixed: AAD divergence between encrypt-time (publisher reads envelope
// fields) and cold-decrypt-time (cold reader reconstructs Actor from row
// columns vs envelope unmarshal).
//
// The unit-level coverage lives in
// internal/eventbus/history/cold_postgres_test.go::TestColdPostgresUnmarshalsEnvelope.
// This file complements that by exercising the full bus → projection →
// PG SELECT chain so any drift between publisher.Publish and audit.persist
// is caught.
package crypto_test

import (
	"context"
	"strconv"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	"github.com/holomush/holomush/test/testutil"
)

var _ = Describe("INV-49 envelope round-trip", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		env    *e2eEnv
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		env = setupE2EEnv(ctx, suiteT)
	})

	AfterEach(func() {
		env.Teardown()
		cancel()
	})

	It("byte-equal envelope across emit → audit projection → cold-read for character-binding actor", func() {
		sceneID := "01HINV49CHARBIND00000000"
		participantID := eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    "01PLAYER49AA0000000000000",
			CharacterID: "01CHAR49AA0000000000000",
			BindingID:   "01BIND49AA0000000000000",
		}
		plaintext := `{"text":"inv49 character"}`
		emitSensitivePluginEvent(
			ctx, suiteT, env,
			"scene."+sceneID, plaintext,
			[]dek.Participant{{
				PlayerID:    participantID.PlayerID,
				CharacterID: participantID.CharacterID,
				BindingID:   participantID.BindingID,
				JoinedAt:    time.Now().UTC(),
				AddedVia:    "test_setup",
			}},
			"test-plugin",
		)

		// Capture the bus envelope (the bytes the publisher actually wrote
		// to JetStream) before the projection ingests them.
		translated := "events.main.scene." + sceneID
		msg := testutil.WaitForOneJetStreamMsg(suiteT, env.bus, translated, testutil.DefaultWait)
		busEnvelope := msg.Data()

		// Drain the projection so the events_audit row is committed.
		env.hostSub.AwaitDrained(suiteT, 10*time.Second)

		// Read the envelope back from PG.
		natsMsgID := msg.Headers().Get("Nats-Msg-Id")
		require.NotEmpty(suiteT, natsMsgID)
		idBytes := testutil.MustParseULID(suiteT, natsMsgID).Bytes()
		row := testutil.QueryEventsAuditByID(suiteT, env.pool, idBytes)

		// INV-49: byte-equal envelope.
		Expect(row.Envelope).To(Equal(busEnvelope),
			"INV-49: events_audit.envelope MUST be byte-equal to the bus envelope")

		// Re-unmarshal the row and verify proto.Equal with the bus envelope's
		// proto form. This is a stronger semantic check on top of byte
		// equality — it surfaces any encoder-level drift (e.g. map key order
		// changes that produce different bytes but equal protos).
		var fromBus eventbusv1.Event
		require.NoError(suiteT, proto.Unmarshal(busEnvelope, &fromBus))
		var fromAudit eventbusv1.Event
		require.NoError(suiteT, proto.Unmarshal(row.Envelope, &fromAudit))
		Expect(proto.Equal(&fromBus, &fromAudit)).To(BeTrue(),
			"INV-49: bus and audit envelope must be proto-equal")
	})

	It("byte-equal envelope for plugin actor with Actor.ID (post-w9ml regression lock)", func() {
		sceneID := "01HINV49PLUGIN0000000000"
		participantID := eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    "01PLAYER49BB0000000000000",
			CharacterID: "01CHAR49BB0000000000000",
			BindingID:   "01BIND49BB0000000000000",
		}
		plaintext := `{"text":"inv49 plugin"}`
		translated := "events.main.scene." + sceneID
		pluginActorID := ulid.MustNew(ulid.Timestamp(time.Now()), nil)
		publishSensitiveWithPluginActor(
			ctx, suiteT, env,
			translated,
			"test-plugin:whisper",
			plaintext,
			pluginActorID,
			[]dek.Participant{{
				PlayerID:    participantID.PlayerID,
				CharacterID: participantID.CharacterID,
				BindingID:   participantID.BindingID,
				JoinedAt:    time.Now().UTC(),
				AddedVia:    "test_setup",
			}},
		)

		msg := testutil.WaitForOneJetStreamMsg(suiteT, env.bus, translated, testutil.DefaultWait)
		busEnvelope := msg.Data()
		env.hostSub.AwaitDrained(suiteT, 10*time.Second)

		natsMsgID := msg.Headers().Get("Nats-Msg-Id")
		require.NotEmpty(suiteT, natsMsgID)
		idBytes := testutil.MustParseULID(suiteT, natsMsgID).Bytes()
		row := testutil.QueryEventsAuditByID(suiteT, env.pool, idBytes)

		Expect(row.Envelope).To(Equal(busEnvelope),
			"INV-49 (plugin actor): events_audit.envelope MUST equal bus envelope")

		// Post-w9ml regression lock: re-unmarshal and confirm Actor.ID
		// (ULID bytes) survived the round-trip.
		var pbEnvelope eventbusv1.Event
		require.NoError(suiteT, proto.Unmarshal(row.Envelope, &pbEnvelope))
		Expect(pbEnvelope.GetActor().GetId()).To(Equal(pluginActorID.Bytes()),
			"envelope.Actor.id must survive emit→audit round-trip")

		// AAD bytes built at encrypt time MUST be reproducible from the
		// audit row alone — that's the deeper INV-49 invariant Decision 5
		// motivated. If an attacker (or a buggy projection) could alter the
		// envelope during persist, AAD reconstruction at cold-decrypt time
		// would fail tag verification and the plaintext would be unrecoverable.
		require.NotNil(suiteT, row.DekRef)
		require.NotNil(suiteT, row.DekVersion)
		aadBytes, err := aad.Build(&pbEnvelope, row.Codec, uint64(*row.DekRef), uint32(*row.DekVersion)) //nolint:gosec
		require.NoError(suiteT, err)
		Expect(aadBytes).NotTo(BeEmpty())
	})

	It("cold-read decrypts correctly for both actor kinds via dispatcher chain", func() {
		// Parametrize over the two actor shapes (post-w9ml: both ULID-based).
		type rowCase struct {
			label           string
			sceneID         string
			usePluginActor  bool
			pluginActorULID ulid.ULID
		}
		cases := []rowCase{
			{label: "character-binding (plugin emit)", sceneID: "01HINV49COLD00CHAR000000", usePluginActor: false},
			{label: "plugin-actor with ULID", sceneID: "01HINV49COLD00PLUGIN0000", usePluginActor: true, pluginActorULID: ulid.MustNew(ulid.Timestamp(time.Now()), nil)},
		}
		for i, tc := range cases {
			tc := tc
			By("case " + strconv.Itoa(i) + ": " + tc.label)

			plaintext := `{"text":"inv49 cold ` + tc.label + `"}`
			participantID := eventbus.SessionIdentity{
				Kind:        eventbus.IdentityKindCharacter,
				PlayerID:    "01PLAYER" + strconv.Itoa(i) + "0000000000000",
				CharacterID: "01CHAR" + strconv.Itoa(i) + "00000000000000",
				BindingID:   "01BIND" + strconv.Itoa(i) + "00000000000000",
			}
			translated := "events.main.scene." + tc.sceneID
			if tc.usePluginActor {
				publishSensitiveWithPluginActor(
					ctx, suiteT, env,
					translated,
					"test-plugin:whisper",
					plaintext,
					tc.pluginActorULID,
					[]dek.Participant{{
						PlayerID:    participantID.PlayerID,
						CharacterID: participantID.CharacterID,
						BindingID:   participantID.BindingID,
						JoinedAt:    time.Now().UTC(),
						AddedVia:    "test_setup",
					}},
				)
			} else {
				emitSensitivePluginEvent(
					ctx, suiteT, env,
					"scene."+tc.sceneID, plaintext,
					[]dek.Participant{{
						PlayerID:    participantID.PlayerID,
						CharacterID: participantID.CharacterID,
						BindingID:   participantID.BindingID,
						JoinedAt:    time.Now().UTC(),
						AddedVia:    "test_setup",
					}},
					"test-plugin",
				)
			}
			env.hostSub.AwaitDrained(suiteT, 10*time.Second)

			reader := buildColdReader(env)
			stream, err := reader.QueryHistory(ctx, eventbus.HistoryQuery{
				Subject:   eventbus.Subject(translated),
				Direction: eventbus.DirectionForward,
				PageSize:  10,
				Identity:  participantID,
			})
			Expect(err).NotTo(HaveOccurred())
			recvCtx, recvCancel := context.WithTimeout(ctx, testutil.DefaultWait)
			ev, err := stream.Next(recvCtx)
			recvCancel()
			_ = stream.Close()
			Expect(err).NotTo(HaveOccurred())
			Expect(ev.MetadataOnly).To(BeFalse(), "participant must receive plaintext for "+tc.label)
			Expect(string(ev.Payload)).To(Equal(plaintext))
			if tc.usePluginActor {
				Expect(ev.Actor.ID).To(Equal(tc.pluginActorULID),
					"plugin Actor.ID must round-trip through cold path")
			}
		}
	})
})
