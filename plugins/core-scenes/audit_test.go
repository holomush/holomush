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

	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// fakeAuditServerStream satisfies pluginv1.PluginAuditService_QueryHistoryServer
// and records every Send.
type fakeAuditServerStream struct {
	grpc.ServerStream
	ctx     context.Context
	sends   []*pluginv1.QueryHistoryResponse
	sendErr error
}

func (s *fakeAuditServerStream) Context() context.Context     { return s.ctx }
func (s *fakeAuditServerStream) SendHeader(metadata.MD) error { return nil }
func (s *fakeAuditServerStream) SetHeader(metadata.MD) error  { return nil }
func (s *fakeAuditServerStream) SetTrailer(metadata.MD)       {}
func (s *fakeAuditServerStream) Send(resp *pluginv1.QueryHistoryResponse) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.sends = append(s.sends, resp)
	return nil
}

// fakeAuditStore records calls made by SceneAuditServer during a test.
// Satisfies BOTH sceneAuditLogStore and sceneMembershipLookup so a single
// instance can serve as the test double for both store fields.
type fakeAuditStore struct {
	queryLogCalled bool
	rows           []logRow
	queryLogErr    error
	isMemberMap    map[string]bool // key: sceneID|characterID
}

func (s *fakeAuditStore) IsMember(_ context.Context, sceneID, characterID string) (bool, error) {
	if s.isMemberMap == nil {
		return false, nil
	}
	return s.isMemberMap[sceneID+"|"+characterID], nil
}

// Insert is a no-op for tests that exercise QueryHistory (Insert is the
// AuditEvent path and not under test here, but the interface requires it).
func (s *fakeAuditStore) Insert(
	_ context.Context,
	_ []byte,
	_, _ string,
	_ *timestamppb.Timestamp,
	_ string,
	_ []byte,
	_ []byte,
	_ int,
	_ string,
	_ *int64,
	_ *int32,
) error {
	return nil
}

// queryLog matches *SceneAuditStore.queryLog verbatim.
func (s *fakeAuditStore) queryLog(
	_ context.Context,
	_ string,
	_, _ []byte,
	_, _ *timestamppb.Timestamp,
	_ bool,
	_ int,
) ([]logRow, error) {
	s.queryLogCalled = true
	if s.queryLogErr != nil {
		return nil, s.queryLogErr
	}
	return s.rows, nil
}

func ulidStringBytes(t *testing.T, s string) []byte {
	t.Helper()
	id, err := ulid.Parse(s)
	require.NoError(t, err)
	b := id.Bytes()
	return b[:]
}

func TestQueryHistoryRejectsNilCaller(t *testing.T) {
	srv := &SceneAuditServer{
		store:        &fakeAuditStore{},
		memberLookup: &fakeAuditStore{},
	}
	stream := &fakeAuditServerStream{ctx: context.Background()}

	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: "events.main.scene.01ABC000000000000000000000.ic",
		Caller:  nil,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "auth denial MUST emit gRPC status")
	assert.Equal(t, codes.PermissionDenied, st.Code())
}

func TestQueryHistoryRejectsCharacterCallerWithZeroID(t *testing.T) {
	srv := &SceneAuditServer{
		store:        &fakeAuditStore{},
		memberLookup: &fakeAuditStore{},
	}
	stream := &fakeAuditServerStream{ctx: context.Background()}

	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: "events.main.scene.01ABC000000000000000000000.ic",
		Caller: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   nil,
		},
	}, stream)

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.PermissionDenied, st.Code())
}

func TestQueryHistoryRejectsNonCharacterKinds(t *testing.T) {
	cases := []eventbusv1.ActorKind{
		eventbusv1.ActorKind_ACTOR_KIND_UNSPECIFIED,
		eventbusv1.ActorKind_ACTOR_KIND_PLAYER,
		eventbusv1.ActorKind_ACTOR_KIND_SYSTEM,
		eventbusv1.ActorKind_ACTOR_KIND_PLUGIN,
	}
	for _, k := range cases {
		t.Run(k.String(), func(t *testing.T) {
			srv := &SceneAuditServer{
				store:        &fakeAuditStore{},
				memberLookup: &fakeAuditStore{},
			}
			stream := &fakeAuditServerStream{ctx: context.Background()}

			err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
				Subject: "events.main.scene.01ABC000000000000000000000.ic",
				Caller: &eventbusv1.Actor{
					Kind: k,
					Id:   ulidStringBytes(t, "01CHAR00000000000000000000"),
				},
			}, stream)
			require.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.PermissionDenied, st.Code())
		})
	}
}

func TestQueryHistoryAllowsMemberAndReturnsRows(t *testing.T) {
	sceneID := "01ABC000000000000000000000"
	charIDStr := "01CHAR00000000000000000000"
	charBytes := ulidStringBytes(t, charIDStr)

	logStore := &fakeAuditStore{
		rows: []logRow{
			{
				id:        ulidStringBytes(t, "01EVENT0000000000000000000"),
				subject:   "events.main.scene." + sceneID + ".ic",
				eventType: "scene.pose.posted",
				timestamp: time.Unix(100, 0),
			},
		},
	}
	memberStore := &fakeAuditStore{
		isMemberMap: map[string]bool{sceneID + "|" + charIDStr: true},
	}

	srv := &SceneAuditServer{store: logStore, memberLookup: memberStore}
	stream := &fakeAuditServerStream{ctx: context.Background()}

	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: "events.main.scene." + sceneID + ".ic",
		Caller: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   charBytes,
		},
	}, stream)

	require.NoError(t, err)
	require.Len(t, stream.sends, 1)
	assert.Equal(t, "scene.pose.posted", stream.sends[0].GetRow().GetType())
}

func TestQueryHistoryDeniesNonMemberWithoutHittingLogStore(t *testing.T) {
	sceneID := "01ABC000000000000000000000"
	charIDStr := "01CHAR00000000000000000000"
	charBytes := ulidStringBytes(t, charIDStr)

	logStore := &fakeAuditStore{}
	memberStore := &fakeAuditStore{isMemberMap: map[string]bool{}}

	srv := &SceneAuditServer{store: logStore, memberLookup: memberStore}
	stream := &fakeAuditServerStream{ctx: context.Background()}

	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: "events.main.scene." + sceneID + ".ic",
		Caller: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   charBytes,
		},
	}, stream)

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.PermissionDenied, st.Code())
	assert.False(t, logStore.queryLogCalled,
		"log store MUST NOT be queried when auth denies — auth is step 1, before any DB work")
}

func TestQueryHistoryReChecksMembershipAcrossPaginations(t *testing.T) {
	sceneID := "01ABC000000000000000000000"
	charIDStr := "01CHAR00000000000000000000"
	charBytes := ulidStringBytes(t, charIDStr)

	logStore := &fakeAuditStore{
		rows: []logRow{{
			id:        ulidStringBytes(t, "01EVENT0000000000000000000"),
			subject:   "events.main.scene." + sceneID + ".ic",
			eventType: "scene.pose.posted",
			timestamp: time.Unix(100, 0),
		}},
	}
	memberStore := &fakeAuditStore{
		isMemberMap: map[string]bool{sceneID + "|" + charIDStr: true},
	}

	srv := &SceneAuditServer{store: logStore, memberLookup: memberStore}

	// Page 1 — member, rows returned.
	stream1 := &fakeAuditServerStream{ctx: context.Background()}
	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: "events.main.scene." + sceneID + ".ic",
		Caller: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   charBytes,
		},
	}, stream1)
	require.NoError(t, err)
	require.Len(t, stream1.sends, 1)

	// Simulate kick.
	delete(memberStore.isMemberMap, sceneID+"|"+charIDStr)

	// Page 2 — no longer member, denied.
	stream2 := &fakeAuditServerStream{ctx: context.Background()}
	err = srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: "events.main.scene." + sceneID + ".ic",
		Caller: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   charBytes,
		},
	}, stream2)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.PermissionDenied, st.Code())
	assert.Empty(t, stream2.sends)
}

func TestQueryHistoryRejectsMalformedSubject(t *testing.T) {
	cases := []string{
		"",
		"events.main.location.01XYZ.ic",
		"not.events.prefix.scene.01.ic",
		"events.main.scene.*.ic",
		"events.main.scene.>",
		"events.main.scene",
		"events.main.scene..ic",  // empty sceneID token (regression guard for empty-token rejection)
		"events..scene.01ABC.ic", // empty gameID token
	}
	for _, subj := range cases {
		t.Run(subj, func(t *testing.T) {
			srv := &SceneAuditServer{
				store:        &fakeAuditStore{},
				memberLookup: &fakeAuditStore{isMemberMap: map[string]bool{}},
			}
			stream := &fakeAuditServerStream{ctx: context.Background()}

			err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
				Subject: subj,
				Caller: &eventbusv1.Actor{
					Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
					Id:   ulidStringBytes(t, "01CHAR00000000000000000000"),
				},
			}, stream)
			require.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.InvalidArgument, st.Code())
		})
	}
}

// TestQueryHistoryFailsClosedWhenMemberLookupNotConfigured pins the
// defensive nil-check at the membership-lookup boundary. SceneAuditServer
// uses field injection (main.go:108-109); a missed wire-up would otherwise
// produce a runtime panic on the first audit read instead of a structured
// Internal error.
func TestQueryHistoryFailsClosedWhenMemberLookupNotConfigured(t *testing.T) {
	srv := &SceneAuditServer{
		store:        &fakeAuditStore{},
		memberLookup: nil, // simulate missed field injection
	}
	stream := &fakeAuditServerStream{ctx: context.Background()}

	err := srv.QueryHistory(&pluginv1.QueryHistoryRequest{
		Subject: "events.main.scene.01ABC000000000000000000000.ic",
		Caller: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   ulidStringBytes(t, "01CHAR00000000000000000000"),
		},
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "nil memberLookup MUST surface as a gRPC status error")
	assert.Equal(t, codes.Internal, st.Code(),
		"nil memberLookup MUST fail closed with codes.Internal, not panic")
}

// validAuditRow returns a syntactically valid AuditRow fixture for the
// AuditEvent validation tests. Tests that exercise specific validation
// paths blank or override individual fields.
func validAuditRow(t *testing.T) *pluginv1.AuditRow {
	t.Helper()
	id := ulid.Make()
	idBytes := id.Bytes()
	return &pluginv1.AuditRow{
		Id:        idBytes[:],
		Subject:   "events.test.scene.01ABC.ic",
		Type:      "core-scenes:pose",
		Timestamp: timestamppb.New(time.Unix(1700000000, 0).UTC()),
		Codec:     "identity",
		SchemaVer: 1,
		Payload:   []byte("p"),
	}
}

func TestAuditEventRejectsMissingTimestamp(t *testing.T) {
	t.Parallel()
	srv := &SceneAuditServer{store: &fakeAuditStore{}}
	row := validAuditRow(t)
	row.Timestamp = nil
	_, err := srv.AuditEvent(context.Background(), &pluginv1.AuditEventRequest{Row: row})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_AUDIT_MISSING_FIELD")
	errutil.AssertErrorContext(t, err, "field", "timestamp")
}

func TestAuditEventRejectsNilRequest(t *testing.T) {
	t.Parallel()
	srv := &SceneAuditServer{store: &fakeAuditStore{}}
	_, err := srv.AuditEvent(context.Background(), nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_AUDIT_MISSING_ROW")
}

func TestAuditEventRejectsNilRow(t *testing.T) {
	t.Parallel()
	srv := &SceneAuditServer{store: &fakeAuditStore{}}
	_, err := srv.AuditEvent(context.Background(), &pluginv1.AuditEventRequest{})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_AUDIT_MISSING_ROW")
}

func TestAuditEventRejectsMissingCodec(t *testing.T) {
	t.Parallel()
	srv := &SceneAuditServer{store: &fakeAuditStore{}}
	row := validAuditRow(t)
	row.Codec = ""
	_, err := srv.AuditEvent(context.Background(), &pluginv1.AuditEventRequest{Row: row})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_AUDIT_MISSING_FIELD")
}

func TestAuditEventRejectsBadSchemaVersion(t *testing.T) {
	t.Parallel()
	srv := &SceneAuditServer{store: &fakeAuditStore{}}
	row := validAuditRow(t)
	row.SchemaVer = -1
	_, err := srv.AuditEvent(context.Background(), &pluginv1.AuditEventRequest{Row: row})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_AUDIT_BAD_SCHEMA_VERSION")
}

func TestAuditEventRejectsMissingType(t *testing.T) {
	t.Parallel()
	srv := &SceneAuditServer{store: &fakeAuditStore{}}
	row := validAuditRow(t)
	row.Type = ""
	_, err := srv.AuditEvent(context.Background(), &pluginv1.AuditEventRequest{Row: row})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_AUDIT_MISSING_FIELD")
}

func TestAuditEventRejectsMissingSubject(t *testing.T) {
	t.Parallel()
	srv := &SceneAuditServer{store: &fakeAuditStore{}}
	row := validAuditRow(t)
	row.Subject = ""
	_, err := srv.AuditEvent(context.Background(), &pluginv1.AuditEventRequest{Row: row})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_AUDIT_MISSING_FIELD")
}

func TestAuditEventRejectsMissingID(t *testing.T) {
	t.Parallel()
	srv := &SceneAuditServer{store: &fakeAuditStore{}}
	row := validAuditRow(t)
	row.Id = []byte("8-bytes!") // not 16
	_, err := srv.AuditEvent(context.Background(), &pluginv1.AuditEventRequest{Row: row})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_AUDIT_MISSING_ID")
}

// TestAuditEventHappyPathPersistsViaStore exercises the full validation
// chain with valid input. fakeAuditStore.Insert is a no-op so this only
// verifies AuditEvent does NOT error on a syntactically valid row — the
// actual PG INSERT is covered by integration tests.
func TestAuditEventHappyPathPersistsViaStore(t *testing.T) {
	t.Parallel()
	srv := &SceneAuditServer{store: &fakeAuditStore{}}
	resp, err := srv.AuditEvent(context.Background(), &pluginv1.AuditEventRequest{Row: validAuditRow(t)})
	require.NoError(t, err)
	assert.NotNil(t, resp, "successful AuditEvent MUST return a non-nil response")
}
