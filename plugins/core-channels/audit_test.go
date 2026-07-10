// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// fakeChannelAuditStream satisfies pluginv1.PluginAuditService_QueryHistoryServer
// and records every Send.
type fakeChannelAuditStream struct {
	grpc.ServerStream
	ctx     context.Context
	sends   []*pluginv1.QueryHistoryResponse
	sendErr error
}

func (s *fakeChannelAuditStream) Context() context.Context     { return s.ctx }
func (s *fakeChannelAuditStream) SendHeader(metadata.MD) error { return nil }
func (s *fakeChannelAuditStream) SetHeader(metadata.MD) error  { return nil }
func (s *fakeChannelAuditStream) SetTrailer(metadata.MD)       {}
func (s *fakeChannelAuditStream) Send(resp *pluginv1.QueryHistoryResponse) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.sends = append(s.sends, resp)
	return nil
}

// fakeChannelAuditStore satisfies BOTH channelAuditLogStore and
// channelMembershipAuthLookup so one instance can serve as both audit-server
// dependencies. It records the args passed to queryLog so the joined_at floor
// and scrollback cap can be asserted at unit level.
type fakeChannelAuditStore struct {
	rows           []channelLogRow
	queryLogCalled bool
	queryLogErr    error
	lastNotBefore  *timestamppb.Timestamp
	lastPageSize   int

	// membership map keyed channelID|characterID; value is the joined_at floor.
	members map[string]time.Time
}

func (s *fakeChannelAuditStore) Insert(
	_ context.Context,
	_ []byte,
	_, _ string,
	_ *timestamppb.Timestamp,
	_ string,
	_, _ []byte,
	_ int,
	_ string,
) error {
	return nil
}

func (s *fakeChannelAuditStore) queryLog(
	_ context.Context,
	_ string,
	_, _ []byte,
	notBefore, _ *timestamppb.Timestamp,
	_ bool,
	pageSize int,
) ([]channelLogRow, error) {
	s.queryLogCalled = true
	s.lastNotBefore = notBefore
	s.lastPageSize = pageSize
	if s.queryLogErr != nil {
		return nil, s.queryLogErr
	}
	return s.rows, nil
}

func (s *fakeChannelAuditStore) MembershipForHistory(_ context.Context, channelID, characterID string) (bool, time.Time, error) {
	if s.members == nil {
		return false, time.Time{}, nil
	}
	joinedAt, ok := s.members[channelID+"|"+characterID]
	return ok, joinedAt, nil
}

func chanULIDBytes(t *testing.T, s string) []byte {
	t.Helper()
	id, err := ulid.Parse(s)
	require.NoError(t, err)
	b := id.Bytes()
	return b[:]
}

const (
	testAuditChannelID = "01ABC000000000000000000000"
	testAuditCharID    = "01CHAR00000000000000000000"
)

func testChannelSubject() string {
	return "events.main.channel." + testAuditChannelID
}

func TestQueryHistoryRejectsNilCaller(t *testing.T) {
	srv := &ChannelAuditServer{store: &fakeChannelAuditStore{}, memberLookup: &fakeChannelAuditStore{}, scrollbackCap: 500}
	stream := &fakeChannelAuditStream{ctx: context.Background()}
	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{Subject: testChannelSubject(), Caller: nil}, stream)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.PermissionDenied, st.Code())
}

func TestQueryHistoryRejectsNonCharacterKinds(t *testing.T) {
	for _, k := range []eventbusv1.ActorKind{
		eventbusv1.ActorKind_ACTOR_KIND_UNSPECIFIED,
		eventbusv1.ActorKind_ACTOR_KIND_PLAYER,
		eventbusv1.ActorKind_ACTOR_KIND_SYSTEM,
		eventbusv1.ActorKind_ACTOR_KIND_PLUGIN,
	} {
		t.Run(k.String(), func(t *testing.T) {
			srv := &ChannelAuditServer{store: &fakeChannelAuditStore{}, memberLookup: &fakeChannelAuditStore{}, scrollbackCap: 500}
			stream := &fakeChannelAuditStream{ctx: context.Background()}
			err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
				Subject: testChannelSubject(),
				Caller:  &eventbusv1.Actor{Kind: k, Id: chanULIDBytes(t, testAuditCharID)},
			}, stream)
			require.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.PermissionDenied, st.Code())
		})
	}
}

func TestQueryHistoryRejectsCharacterCallerWithZeroID(t *testing.T) {
	srv := &ChannelAuditServer{store: &fakeChannelAuditStore{}, memberLookup: &fakeChannelAuditStore{}, scrollbackCap: 500}
	stream := &fakeChannelAuditStream{ctx: context.Background()}
	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: testChannelSubject(),
		Caller:  &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, Id: nil},
	}, stream)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.PermissionDenied, st.Code())
}

func TestQueryHistoryDeniesNonMemberWithoutHittingLogStore(t *testing.T) {
	logStore := &fakeChannelAuditStore{}
	memberStore := &fakeChannelAuditStore{members: map[string]time.Time{}}
	srv := &ChannelAuditServer{store: logStore, memberLookup: memberStore, scrollbackCap: 500}
	stream := &fakeChannelAuditStream{ctx: context.Background()}

	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: testChannelSubject(),
		Caller:  &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, Id: chanULIDBytes(t, testAuditCharID)},
	}, stream)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.PermissionDenied, st.Code())
	assert.False(t, logStore.queryLogCalled, "auth is step 1 — store MUST NOT be queried on a non-member read")
}

func TestQueryHistoryAllowsMemberAndReturnsRows(t *testing.T) {
	logStore := &fakeChannelAuditStore{rows: []channelLogRow{{
		id:        chanULIDBytes(t, "01EVENT0000000000000000000"),
		subject:   testChannelSubject(),
		eventType: "core-channels:channel_say",
		timestamp: pgnanos.From(time.Unix(100, 0)),
	}}}
	memberStore := &fakeChannelAuditStore{members: map[string]time.Time{testAuditChannelID + "|" + testAuditCharID: time.Unix(50, 0)}}
	srv := &ChannelAuditServer{store: logStore, memberLookup: memberStore, scrollbackCap: 500}
	stream := &fakeChannelAuditStream{ctx: context.Background()}

	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: testChannelSubject(),
		Caller:  &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, Id: chanULIDBytes(t, testAuditCharID)},
	}, stream)
	require.NoError(t, err)
	require.Len(t, stream.sends, 1)
	assert.Equal(t, "core-channels:channel_say", stream.sends[0].GetRow().GetType())
}

func TestQueryHistoryAppliesJoinedAtFloor(t *testing.T) {
	joinedAt := time.Unix(1_700_000_000, 0).UTC()
	logStore := &fakeChannelAuditStore{}
	memberStore := &fakeChannelAuditStore{members: map[string]time.Time{testAuditChannelID + "|" + testAuditCharID: joinedAt}}
	srv := &ChannelAuditServer{store: logStore, memberLookup: memberStore, scrollbackCap: 500}
	stream := &fakeChannelAuditStream{ctx: context.Background()}

	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: testChannelSubject(),
		Caller:  &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, Id: chanULIDBytes(t, testAuditCharID)},
	}, stream)
	require.NoError(t, err)
	require.NotNil(t, logStore.lastNotBefore, "history read MUST apply the joined_at floor")
	assert.WithinDuration(t, joinedAt, logStore.lastNotBefore.AsTime(), time.Second,
		"the queryLog floor MUST equal the member's joined_at (D-07)")
}

func TestQueryHistoryClampsPageSizeToScrollbackCap(t *testing.T) {
	logStore := &fakeChannelAuditStore{}
	memberStore := &fakeChannelAuditStore{members: map[string]time.Time{testAuditChannelID + "|" + testAuditCharID: time.Unix(1, 0)}}
	srv := &ChannelAuditServer{store: logStore, memberLookup: memberStore, scrollbackCap: 500}
	stream := &fakeChannelAuditStream{ctx: context.Background()}

	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject:  testChannelSubject(),
		PageSize: 100000,
		Caller:   &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, Id: chanULIDBytes(t, testAuditCharID)},
	}, stream)
	require.NoError(t, err)
	assert.Equal(t, 500, logStore.lastPageSize, "page size MUST clamp to the scrollback cap (D-07)")
}

func TestQueryHistoryFailsClosedWhenMemberLookupNotConfigured(t *testing.T) {
	srv := &ChannelAuditServer{store: &fakeChannelAuditStore{}, memberLookup: nil, scrollbackCap: 500}
	stream := &fakeChannelAuditStream{ctx: context.Background()}
	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: testChannelSubject(),
		Caller:  &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, Id: chanULIDBytes(t, testAuditCharID)},
	}, stream)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code(), "nil memberLookup MUST fail closed with Internal, not panic")
}

func TestQueryHistoryRejectsMalformedSubject(t *testing.T) {
	for _, subj := range []string{
		"",
		"events.main.scene." + testAuditChannelID + ".ic",
		"not.events.prefix.channel.01",
		"events.main.channel.*",
		"events.main.channel.>",
		"events.main.channel",
		"events.main.channel.",
		"events..channel." + testAuditChannelID,
	} {
		t.Run(subj, func(t *testing.T) {
			srv := &ChannelAuditServer{store: &fakeChannelAuditStore{}, memberLookup: &fakeChannelAuditStore{members: map[string]time.Time{}}, scrollbackCap: 500}
			stream := &fakeChannelAuditStream{ctx: context.Background()}
			err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
				Subject: subj,
				Caller:  &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, Id: chanULIDBytes(t, testAuditCharID)},
			}, stream)
			require.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.InvalidArgument, st.Code())
		})
	}
}

func TestParseChannelSubjectExtractsID(t *testing.T) {
	id, err := parseChannelSubject("events.main.channel." + testAuditChannelID)
	require.NoError(t, err)
	assert.Equal(t, testAuditChannelID, id)
}

// ── AuditEvent validation ───────────────────────────────────────────────────

func validChannelAuditRow(t *testing.T) *pluginv1.AuditRow {
	t.Helper()
	id := ulid.Make().Bytes()
	return &pluginv1.AuditRow{
		Id:        id[:],
		Subject:   testChannelSubject(),
		Type:      "core-channels:channel_say",
		Timestamp: timestamppb.New(time.Unix(1_700_000_000, 0).UTC()),
		Codec:     "identity",
		SchemaVer: 1,
		Payload:   []byte("p"),
	}
}

func TestAuditEventRejectsNilRequest(t *testing.T) {
	srv := &ChannelAuditServer{store: &fakeChannelAuditStore{}}
	_, err := srv.AuditEvent(context.Background(), nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHANNEL_AUDIT_MISSING_ROW")
}

func TestAuditEventRejectsNilRow(t *testing.T) {
	srv := &ChannelAuditServer{store: &fakeChannelAuditStore{}}
	_, err := srv.AuditEvent(context.Background(), &pluginv1.AuditEventRequest{})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHANNEL_AUDIT_MISSING_ROW")
}

func TestAuditEventRejectsMissingFields(t *testing.T) {
	cases := map[string]func(r *pluginv1.AuditRow){
		"codec":     func(r *pluginv1.AuditRow) { r.Codec = "" },
		"type":      func(r *pluginv1.AuditRow) { r.Type = "" },
		"subject":   func(r *pluginv1.AuditRow) { r.Subject = "" },
		"timestamp": func(r *pluginv1.AuditRow) { r.Timestamp = nil },
	}
	for field, mutate := range cases {
		t.Run(field, func(t *testing.T) {
			srv := &ChannelAuditServer{store: &fakeChannelAuditStore{}}
			row := validChannelAuditRow(t)
			mutate(row)
			_, err := srv.AuditEvent(context.Background(), &pluginv1.AuditEventRequest{Row: row})
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "CHANNEL_AUDIT_MISSING_FIELD")
		})
	}
}

func TestAuditEventRejectsBadSchemaVersion(t *testing.T) {
	srv := &ChannelAuditServer{store: &fakeChannelAuditStore{}}
	row := validChannelAuditRow(t)
	row.SchemaVer = -1
	_, err := srv.AuditEvent(context.Background(), &pluginv1.AuditEventRequest{Row: row})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHANNEL_AUDIT_BAD_SCHEMA_VERSION")
}

func TestAuditEventRejectsBadID(t *testing.T) {
	srv := &ChannelAuditServer{store: &fakeChannelAuditStore{}}
	row := validChannelAuditRow(t)
	row.Id = []byte("8-bytes!")
	_, err := srv.AuditEvent(context.Background(), &pluginv1.AuditEventRequest{Row: row})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHANNEL_AUDIT_MISSING_ID")
}

func TestAuditEventHappyPathReturnsResponse(t *testing.T) {
	srv := &ChannelAuditServer{store: &fakeChannelAuditStore{}}
	resp, err := srv.AuditEvent(context.Background(), &pluginv1.AuditEventRequest{Row: validChannelAuditRow(t)})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}
