// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// resourceTypeChannel is the single resource type the channel plugin owns.
const resourceTypeChannel = "channel"

// channelMembershipStorer is the narrow persistence dependency the resolver
// needs: a single-round-trip read of a channel plus its membership lists. The
// concrete *channelStore satisfies it.
type channelMembershipStorer interface {
	GetWithMembership(ctx context.Context, id string) (channel *channelRow, members, banned, muted []string, err error)
}

// ChannelResolver implements pluginv1.AttributeResolverServiceServer.
type ChannelResolver struct {
	pluginv1.UnimplementedAttributeResolverServiceServer
	store channelMembershipStorer
}

// NewChannelResolver returns a resolver backed by the given store.
func NewChannelResolver(store channelMembershipStorer) *ChannelResolver {
	return &ChannelResolver{store: store}
}

// GetSchema is a RED-phase stub.
func (r *ChannelResolver) GetSchema(_ context.Context, _ *pluginv1.GetSchemaRequest) (*pluginv1.GetSchemaResponse, error) {
	return &pluginv1.GetSchemaResponse{}, nil
}

// ResolveResource is a RED-phase stub.
func (r *ChannelResolver) ResolveResource(_ context.Context, _ *pluginv1.ResolveResourceRequest) (*pluginv1.ResolveResourceResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
