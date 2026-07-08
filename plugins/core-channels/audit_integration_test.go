// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// recordingStream collects QueryHistory sends against a real DB.
type recordingStream struct {
	pluginv1.PluginAuditService_QueryHistoryServer
	ctx   context.Context
	sends []*pluginv1.QueryHistoryResponse
}

func (s *recordingStream) Context() context.Context { return s.ctx }
func (s *recordingStream) Send(resp *pluginv1.QueryHistoryResponse) error {
	s.sends = append(s.sends, resp)
	return nil
}

// insertLogRow persists one channel_log row via the audit store's idempotent
// INSERT, stamping the given event timestamp so joined_at-floor filtering can be
// exercised deterministically.
func insertLogRow(store *ChannelAuditStore, subject string, ts time.Time) []byte {
	GinkgoHelper()
	id := ulid.Make().Bytes()
	err := store.Insert(context.Background(), id[:], subject, "core-channels:channel_say",
		timestamppb.New(ts), "ACTOR_KIND_CHARACTER", nil, []byte(`{"text":"hi"}`), 1, "identity")
	Expect(err).NotTo(HaveOccurred())
	return id[:]
}

var _ = Describe("channel_log audit round-trip (CHAN-02/CHAN-03)", func() {
	var (
		ctx        context.Context
		store      *channelStore
		auditStore *ChannelAuditStore
		srv        *ChannelAuditServer
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = newTestChannelStore()
		auditStore = NewChannelAuditStore(store.Pool())
		srv = &ChannelAuditServer{store: auditStore, memberLookup: store, scrollbackCap: 500}
	})

	Describe("AuditEvent idempotency (T-01-17)", func() {
		It("writes one row on redelivery of the same event id", func() {
			id := ulid.Make().Bytes()
			row := &pluginv1.AuditRow{
				Id:        id[:],
				Subject:   "events.main.channel.01CHAN000000000000000000ID",
				Type:      "core-channels:channel_say",
				Timestamp: timestamppb.New(time.Unix(1_700_000_000, 0).UTC()),
				Codec:     "identity",
				SchemaVer: 1,
				Payload:   []byte(`{"text":"hi"}`),
			}
			_, err := srv.AuditEvent(ctx, &pluginv1.AuditEventRequest{Row: row})
			Expect(err).NotTo(HaveOccurred())
			// Redeliver the identical row (same id) — ON CONFLICT DO NOTHING.
			_, err = srv.AuditEvent(ctx, &pluginv1.AuditEventRequest{Row: row})
			Expect(err).NotTo(HaveOccurred())

			var n int
			Expect(store.pool.QueryRow(ctx, `SELECT count(*) FROM channel_log WHERE id = $1`, id[:]).Scan(&n)).To(Succeed())
			Expect(n).To(Equal(1), "redelivery MUST be a no-op (idempotent INSERT)")
		})
	})

	Describe("QueryHistory membership gate + joined_at floor (CHAN-02/D-07)", func() {
		var (
			channelID   string
			memberID    ulid.ULID
			subject     string
			afterRowID  []byte
			beforeRowID []byte
		)

		BeforeEach(func() {
			row := &channelRow{Name: "History", Type: string(channelTypePublic), OwnerID: "char-owner"}
			Expect(store.CreateChannel(ctx, row)).To(Succeed())
			channelID = row.ID
			subject = "events.main.channel." + channelID

			memberID = ulid.Make()
			Expect(store.JoinChannel(ctx, channelID, memberID.String())).To(Succeed())

			ok, joinedAt, err := store.MembershipForHistory(ctx, channelID, memberID.String())
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())

			// One row before the join (must be filtered out), one after.
			beforeRowID = insertLogRow(auditStore, subject, joinedAt.Add(-time.Hour))
			afterRowID = insertLogRow(auditStore, subject, joinedAt.Add(time.Hour))
			_ = beforeRowID
		})

		It("returns only rows at/after the member's joined_at floor", func() {
			stream := &recordingStream{ctx: ctx}
			err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
				Subject: subject,
				Caller:  &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, Id: memberID.Bytes()[:]},
			}, stream)
			Expect(err).NotTo(HaveOccurred())
			Expect(stream.sends).To(HaveLen(1), "history MUST NOT cross the joined_at floor (D-07)")
			Expect(stream.sends[0].GetRow().GetId()).To(Equal(afterRowID))
		})

		It("denies a non-member with PermissionDenied before any DB read", func() {
			nonMember := ulid.Make()
			stream := &recordingStream{ctx: ctx}
			err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
				Subject: subject,
				Caller:  &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, Id: nonMember.Bytes()[:]},
			}, stream)
			Expect(err).To(HaveOccurred())
			st, _ := status.FromError(err)
			Expect(st.Code()).To(Equal(codes.PermissionDenied))
			Expect(stream.sends).To(BeEmpty())
		})
	})
})
