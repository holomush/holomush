// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package channels_test

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	channelv1 "github.com/holomush/holomush/pkg/proto/holomush/channel/v1"
)

// commandText unmarshals a command_response/command_error frame's payload to its
// user-facing text. A plugin's Errorf/OK response is delivered to the acting
// session as a command_error / command_response EVENT (not an RPC failure), so
// user-facing outcomes are asserted against these frames, mirroring the scenes
// e2e (scene_info_read_access_test.go).
func commandText(payload []byte) string {
	var crp core.CommandResponsePayload
	Expect(json.Unmarshal(payload, &crp)).To(Succeed(),
		"command event payload must unmarshal as CommandResponsePayload")
	return crp.Text
}

// channelSayType is the plugin-qualified wire type core-channels emits for a
// spoken channel line (INV-PLUGIN-40).
const channelSayType = "core-channels:channel_say"

// channelIDByName resolves a channel name to its id via the store-backed
// ListChannels RPC (membership-keyed on the caller's character id — no
// command-dispatch actor context required). The caller MUST be a member.
func channelIDByName(ctx context.Context, client channelv1.ChannelServiceClient, characterID, name string) string {
	resp, err := client.ListChannels(ctx, &channelv1.ListChannelsRequest{CharacterId: characterID})
	Expect(err).NotTo(HaveOccurred(), "ListChannels for %s", characterID)
	for _, c := range resp.GetChannels() {
		if c.GetName() == name {
			return c.GetId()
		}
	}
	Fail("channel " + name + " not found among " + characterID + " memberships")
	return ""
}

// historyTexts returns the content of each entry the membership-gated
// QueryChannelHistory RPC yields for the given member. A non-member deny
// surfaces as an error and fails the caller.
func historyTexts(ctx context.Context, client channelv1.ChannelServiceClient, characterID, channelID string) []string {
	resp, err := client.QueryChannelHistory(ctx, &channelv1.QueryChannelHistoryRequest{
		CharacterId: characterID,
		ChannelId:   channelID,
		Limit:       50,
	})
	if err != nil {
		return nil
	}
	texts := make([]string, 0, len(resp.GetEntries()))
	for _, e := range resp.GetEntries() {
		texts = append(texts, e.GetContent())
	}
	return texts
}

// End-to-end live delivery + membership-gated history read-back. A member
// receives another member's channel_say live; a non-member receives nothing and
// is denied history content; the `=` prefix alias routes to core-channels
// (MED-6); a posted line round-trips through channel_log (CHAN-03).
var _ = Describe("core-channels e2e: live delivery + membership-gated history", Ordered, func() {
	var (
		ts           *integrationtest.Server
		ctx          context.Context
		admin        *integrationtest.Session
		poster       *integrationtest.Session
		listener     *integrationtest.Session
		lurker       *integrationtest.Session
		client       channelv1.ChannelServiceClient
		townsquareID string
	)

	BeforeAll(func() {
		ctx = context.Background()
		ts = integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithRealABAC(),
			integrationtest.WithSessionStreamDelivery(),
		)
		admin = ts.ConnectAuthedWithRoles(ctx, "Admin", []string{"admin"})
		Expect(admin.SendCommand(ctx, "channel create Townsquare public")).To(Succeed(),
			"admin (admin role) must be permitted to create a channel (seed-channel-admin-create)")

		poster = ts.ConnectAuthed(ctx, "Poster")
		listener = ts.ConnectAuthed(ctx, "Listener")
		lurker = ts.ConnectAuthed(ctx, "Lurker")

		Expect(poster.SendCommand(ctx, "channel join Townsquare")).To(Succeed())
		Expect(listener.SendCommand(ctx, "channel join Townsquare")).To(Succeed())

		client = ts.ChannelServiceClient()
		townsquareID = channelIDByName(ctx, client, poster.CharacterID.String(), "Townsquare")
		Expect(townsquareID).NotTo(BeEmpty())
	})

	AfterAll(func() {
		if ts != nil {
			ts.Stop()
		}
	})

	It("delivers a channel_say live to a second joined member (T-01-01 member arm, CHAN-02)", func() {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		Expect(poster.SendCommand(ctx, "channel say Townsquare hello-there")).To(Succeed())

		frame := listener.WaitForEvent(cctx, channelSayType)
		Expect(frame).NotTo(BeNil(),
			"a joined member MUST receive another member's channel_say on its live Subscribe stream")
		Expect(frame.GetType()).To(Equal(channelSayType))
	})

	It("does NOT deliver a channel_say to a non-member (T-01-01 prohibition arm)", func() {
		Expect(poster.SendCommand(ctx, "channel say Townsquare not-for-lurker")).To(Succeed())

		// The non-member is subscribed only to its ambient streams (Townsquare is
		// not a default channel and the lurker never joined), so no channel_say
		// may appear in a bounded drain window.
		frames := lurker.DrainEvents(ctx, time.Second)
		for _, f := range frames {
			Expect(f.GetType()).NotTo(Equal(channelSayType),
				"a non-member MUST NOT receive channel content live (INV-CHANNEL-1 live arm)")
		}
	})

	It("routes `=Townsquare ...` through the manifest-seeded `=` prefix alias to core-channels (MED-6)", func() {
		// The `=` prefix alias (plugin.yaml commands.aliases) is seeded by the host
		// alias-seeder under the whole-system tier and consulted via the dispatcher's
		// alias cache, reassembling `=Townsquare equals-routed` to
		// `channel Townsquare equals-routed` — a post to channel Townsquare. The
		// content round-tripping through channel_log proves the alias reached the
		// plugin (not merely the 01-07 parser unit test).
		Expect(poster.SendCommand(ctx, "=Townsquare equals-routed-line")).To(Succeed())

		Eventually(func() []string {
			return historyTexts(ctx, client, poster.CharacterID.String(), townsquareID)
		}, 10*time.Second, 200*time.Millisecond).Should(ContainElement(ContainSubstring("equals-routed-line")),
			"the =alias-routed post MUST reach core-channels and land in channel_log")
	})

	// Verifies: INV-CHANNEL-1
	// Verifies: INV-PRIVACY-7
	//
	// INV-CHANNEL-1: history CONTENT is membership-gated for EVERY channel type
	// (public included) — a non-member's QueryChannelHistory is denied a uniform
	// not-found while a member reads the content back.
	// INV-PRIVACY-7: core-channels is the first history_scope: custom adopter;
	// this exercises its divergent, membership-gated custom-scope history
	// semantics (the placeholder the history-scope spec left open).
	It("reads posted content back to a member and denies a non-member (CHAN-03 emit→audit / INV-CHANNEL-1)", func() {
		// Member: the earlier `channel say hello-there` round-trips emit→audit
		// (host consumer → PluginAuditService.AuditEvent → channel_log) and is
		// read back through the membership-gated QueryChannelHistory (CHAN-03).
		Eventually(func() []string {
			return historyTexts(ctx, client, poster.CharacterID.String(), townsquareID)
		}, 10*time.Second, 200*time.Millisecond).Should(ContainElement(ContainSubstring("hello-there")),
			"a member MUST read back the channel's history content (emit→audit round-trip)")

		// Non-member: history CONTENT is membership-gated for EVERY channel type —
		// Townsquare being public governs visibility/join-eligibility, NOT history
		// access. The deny is a uniform not-found (no hidden-vs-empty oracle).
		_, err := client.QueryChannelHistory(ctx, &channelv1.QueryChannelHistoryRequest{
			CharacterId: lurker.CharacterID.String(),
			ChannelId:   townsquareID,
			Limit:       50,
		})
		Expect(err).To(HaveOccurred(),
			"a non-member MUST NOT read a public channel's history content (INV-CHANNEL-1)")
		Expect(status.Code(err)).To(Equal(codes.NotFound),
			"the non-member history deny MUST present the uniform not-found (T-01-12)")
	})
})

// End-to-end error uniformity + admin override on a private channel.
var _ = Describe("core-channels e2e: error uniformity + admin override (INV-CHANNEL-2 / T-01-12)", Ordered, func() {
	var (
		ts     *integrationtest.Server
		ctx    context.Context
		owner  *integrationtest.Session
		admin2 *integrationtest.Session
		target *integrationtest.Session
	)

	BeforeAll(func() {
		ctx = context.Background()
		ts = integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithRealABAC(),
			integrationtest.WithSessionStreamDelivery(),
		)
		owner = ts.ConnectAuthedWithRoles(ctx, "Owner", []string{"admin"})
		Expect(owner.SendCommand(ctx, "channel create Secret private")).To(Succeed())
		admin2 = ts.ConnectAuthedWithRoles(ctx, "Overseer", []string{"admin"})
		target = ts.ConnectAuthed(ctx, "Target")
	})

	AfterAll(func() {
		if ts != nil {
			ts.Stop()
		}
	})

	// Verifies: INV-CHANNEL-2
	//
	// A hidden (private, non-invitee) channel and a truly-absent channel present
	// the IDENTICAL uniform not-found — no absent-vs-hidden existence oracle.
	It("presents a hidden (private, non-invitee) channel and an absent channel as the SAME uniform not-found (INV-CHANNEL-2)", func() {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		// A user-facing refusal is delivered as a command_error EVENT (the
		// dispatch itself succeeds), so the outcome is read off the acting
		// session's stream, not the SendCommand return.
		Expect(target.SendCommand(ctx, "channel join Secret")).To(Succeed(),
			"the join command dispatches; the refusal arrives as a command_error event")
		hiddenText := commandText(target.WaitForEvent(cctx, string(core.EventTypeCommandError)).GetPayload())

		Expect(target.SendCommand(ctx, "channel join Nonexistentchannel")).To(Succeed())
		absentText := commandText(target.WaitForEvent(cctx, string(core.EventTypeCommandError)).GetPayload())

		Expect(hiddenText).To(ContainSubstring("No such channel."),
			"a non-invitee joining a PRIVATE channel MUST get the uniform not-found")
		Expect(absentText).To(ContainSubstring("No such channel."),
			"joining an ABSENT channel MUST get the uniform not-found")
		Expect(hiddenText).To(Equal(absentText),
			"hidden and absent MUST be INDISTINGUISHABLE (no existence oracle, T-01-12)")
	})

	It("lets a non-owner admin invite into a channel it does not own (admin override, D-06) and the invitee then joins", func() {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		// admin2 is neither the owner nor a member of Secret; only the admin
		// override (admin-override-channel) grants the invite authority. Success
		// is the command_response "Invited." (a denial would be a command_error).
		Expect(admin2.SendCommand(ctx, "channel invite Secret "+target.CharacterID.String())).To(Succeed())
		inviteText := commandText(admin2.WaitForEvent(cctx, string(core.EventTypeCommandResponse)).GetPayload())
		Expect(inviteText).To(ContainSubstring("Invited"),
			"a non-owner admin MUST be able to invite (admin override, D-06)")

		// InviteToChannel admits the target as a member, so the target's own join
		// now resolves (read gate passes for a member) rather than the uniform
		// not-found it received before the invite.
		Expect(target.SendCommand(ctx, "channel join Secret")).To(Succeed())
		joinText := commandText(target.WaitForEvent(cctx, string(core.EventTypeCommandResponse)).GetPayload())
		Expect(joinText).To(ContainSubstring("Joined"),
			"an invited character MUST be admitted to the private channel")
	})
})
