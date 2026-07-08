// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// channelLogRow is the scanned representation of one channel_log row. Channel
// events are plaintext (D-04): there are NO dek_ref / dek_version columns.
type channelLogRow struct {
	id        []byte
	subject   string
	eventType string
	timestamp time.Time
	actorKind string
	actorID   []byte
	payload   []byte
	schemaVer int
	codec     string
}

// channelAuditLogStore is the log-storage surface ChannelAuditServer needs.
type channelAuditLogStore interface {
	Insert(
		ctx context.Context,
		id []byte,
		subject, eventType string,
		timestamp *timestamppb.Timestamp,
		actorKind string,
		actorID, payload []byte,
		schemaVer int,
		codec string,
	) error
	queryLog(
		ctx context.Context,
		subject string,
		after, before []byte,
		notBefore, notAfter *timestamppb.Timestamp,
		reverse bool,
		pageSize int,
	) ([]channelLogRow, error)
}

// channelMembershipAuthLookup is the membership-check surface ChannelAuditServer
// needs: reports membership and the member's most-recent joined_at (the D-07
// history floor). *channelStore satisfies it.
type channelMembershipAuthLookup interface {
	MembershipForHistory(ctx context.Context, channelID, characterID string) (isMember bool, joinedAt time.Time, err error)
}

// ChannelAuditServer implements PluginAuditService for core-channels, mirroring
// SceneAuditServer minus the DEK columns (plaintext, D-04).
type ChannelAuditServer struct {
	pluginv1.UnimplementedPluginAuditServiceServer
	store         channelAuditLogStore
	memberLookup  channelMembershipAuthLookup
	scrollbackCap int
}

// AuditEvent RED stub.
func (s *ChannelAuditServer) AuditEvent(_ context.Context, _ *pluginv1.AuditEventRequest) (*pluginv1.AuditEventResponse, error) {
	return &pluginv1.AuditEventResponse{}, nil
}

// QueryHistory RED stub.
func (s *ChannelAuditServer) QueryHistory(_ *pluginv1.QueryHistoryRequest, _ pluginv1.PluginAuditService_QueryHistoryServer) error {
	return nil
}

// parseChannelSubject RED stub.
func parseChannelSubject(_ string) (string, error) {
	return "", nil
}
