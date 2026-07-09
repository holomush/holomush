// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"

	"github.com/samber/oops"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// resourceTypeChannel is the single resource type the channel plugin owns.
// Declared as a constant so the resolver and the manifest stay in sync.
const resourceTypeChannel = "channel"

// channelMembershipStorer is the narrow persistence dependency the resolver
// needs: a single-round-trip read of a channel row plus its membership /
// banned / muted character-id lists. The concrete *channelStore satisfies it.
type channelMembershipStorer interface {
	GetWithMembership(ctx context.Context, id string) (channel *channelRow, members, banned, muted []string, err error)
}

// ChannelResolver implements pluginv1.AttributeResolverServiceServer for the
// channel plugin. It exposes the schema of attributes the plugin can resolve
// (GetSchema) and resolves attributes for individual channel resources
// (ResolveResource) when called by the host's ABAC engine during policy
// evaluation.
//
// This is the D-03 (Landmine 1) reconciliation: CONTEXT's D-03 specifies a
// principal-side `ChannelAttributeProvider` resolving
// `principal.channel_memberships`, but the plugin AttributeResolverService proto
// exposes only GetSchema + ResolveResource — there is no subject/principal RPC
// (PluginAttributeProvider.ResolveSubject returns nil). Membership is therefore
// modelled RESOURCE-side as `resource.channel.members`, exactly like
// resource.scene.participants, and policies read
// `principal.id in resource.channel.members`.
//
// This resolver exposes only non-content attributes
// (name/type/owner/has_owner/archived/members/banned/muted). Channel message
// CONTENT lives behind the membership-gated QueryHistory RPC (01-06), never on
// the ABAC attribute path.
type ChannelResolver struct {
	pluginv1.UnimplementedAttributeResolverServiceServer
	store channelMembershipStorer
}

// NewChannelResolver returns a resolver backed by the given store.
func NewChannelResolver(store channelMembershipStorer) *ChannelResolver {
	return &ChannelResolver{store: store}
}

// GetSchema returns the attribute schema the plugin can resolve. Called once by
// the host's plugin manager after host.Load returns (schema discovery, gated on
// the manifest's resource_types: [channel] declaration).
func (r *ChannelResolver) GetSchema(ctx context.Context, _ *pluginv1.GetSchemaRequest) (*pluginv1.GetSchemaResponse, error) {
	_, span := startSpan(ctx, "channel.resolver.get_schema")
	defer span.End()

	return &pluginv1.GetSchemaResponse{
		ResourceTypes: map[string]*pluginv1.ResourceTypeSchema{
			resourceTypeChannel: {
				Attributes: map[string]pluginv1.AttributeType{
					"name":      pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
					"type":      pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
					"owner":     pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
					"has_owner": pluginv1.AttributeType_ATTRIBUTE_TYPE_BOOL,
					"archived":  pluginv1.AttributeType_ATTRIBUTE_TYPE_BOOL,
					"members":   pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST,
					"banned":    pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST,
					"muted":     pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST,
				},
			},
		},
	}, nil
}

// ResolveResource returns the attributes for a specific channel resource.
// Called by the host's ABAC engine when evaluating a policy that references a
// channel attribute (e.g. the read/emit/write policies that check
// `principal.id in resource.channel.members`).
//
// A resource type other than "channel" is rejected with InvalidArgument so
// host-side misrouting bugs surface immediately. A missing channel returns
// codes.NotFound uniformly (the hidden-channel existence-oracle mitigation,
// T-01-12). Any other store error is translated to a generic codes.Internal
// with no inner-error detail on the wire (.claude/rules/grpc-errors.md).
func (r *ChannelResolver) ResolveResource(ctx context.Context, req *pluginv1.ResolveResourceRequest) (*pluginv1.ResolveResourceResponse, error) {
	ctx, span := startSpan(
		ctx, "channel.resolver.resolve_resource",
		attribute.String("resource_type", req.GetResourceType()),
		attribute.String("resource_id", req.GetResourceId()),
	)
	defer span.End()

	if req.GetResourceType() != resourceTypeChannel {
		err := status.Errorf(codes.InvalidArgument,
			"core-channels only resolves resource type %q, got %q",
			resourceTypeChannel, req.GetResourceType())
		recordError(span, err)
		return nil, err
	}

	// Create sentinel: the create gate evaluates ABAC against `channel:new`
	// (createRateResource) BEFORE any channel row exists. Under the real seeded
	// ABAC engine the resolver IS invoked for this ref (the assumption that it is
	// "never called" holds only for a mock evaluator), so a store lookup for the
	// non-existent id would fail-close CHANNEL_NOT_FOUND and deny EVERY create.
	// The admin-create policy references only principal.character.roles, so the
	// sentinel resolves to an empty attribute bag: all resource.channel.* read as
	// missing (fail-closed for instance-scoped policies per the DSL evaluator),
	// while the principal-only create permit fires. A real channel id is never
	// the sentinel (ids are ULIDs), so genuine missing-channel reads still return
	// the uniform NotFound below.
	if req.GetResourceId() == createSentinelResourceID {
		return &pluginv1.ResolveResourceResponse{
			Attributes: map[string]*pluginv1.AttributeValue{},
		}, nil
	}

	row, members, banned, muted, err := r.store.GetWithMembership(ctx, req.GetResourceId())
	if err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "CHANNEL_NOT_FOUND" {
			return nil, status.Errorf(codes.NotFound, "channel not found: %s", req.GetResourceId())
		}
		// Inner error is already captured on the span via recordError above;
		// returning a generic message keeps internal detail (table names, query
		// fragments, hostnames) off the wire per .claude/rules/grpc-errors.md.
		return nil, status.Error(codes.Internal, "failed to resolve channel") //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	attrs := map[string]*pluginv1.AttributeValue{
		"name":     {Kind: &pluginv1.AttributeValue_StringValue{StringValue: row.Name}},
		"type":     {Kind: &pluginv1.AttributeValue_StringValue{StringValue: row.Type}},
		"archived": {Kind: &pluginv1.AttributeValue_BoolValue{BoolValue: row.Archived}},
		"members": {Kind: &pluginv1.AttributeValue_StringListValue{
			StringListValue: &pluginv1.StringList{Values: members},
		}},
		"banned": {Kind: &pluginv1.AttributeValue_StringListValue{
			StringListValue: &pluginv1.StringList{Values: banned},
		}},
		"muted": {Kind: &pluginv1.AttributeValue_StringListValue{
			StringListValue: &pluginv1.StringList{Values: muted},
		}},
	}

	// Optional attribute: owner is emitted only when the channel has a real
	// character owner. A system-owned default channel (owner_id == systemOwnerID,
	// D-01) has no character owner, so the "owner" key is OMITTED and has_owner is
	// false — an owner-moderation policy (resource.channel.owner == principal.id)
	// then fail-closes on system channels (T-01-14; only admin override applies).
	// An empty-string sentinel would fail-OPEN in the DSL evaluator (a present ""
	// is a value, not a missing key) — .claude/rules/abac-providers.md, ADR
	// holomush-ti1b. The has_owner witness is always present.
	if row.OwnerID != "" && row.OwnerID != systemOwnerID {
		attrs["owner"] = &pluginv1.AttributeValue{Kind: &pluginv1.AttributeValue_StringValue{StringValue: row.OwnerID}}
		attrs["has_owner"] = &pluginv1.AttributeValue{Kind: &pluginv1.AttributeValue_BoolValue{BoolValue: true}}
	} else {
		attrs["has_owner"] = &pluginv1.AttributeValue{Kind: &pluginv1.AttributeValue_BoolValue{BoolValue: false}}
		// owner key INTENTIONALLY ABSENT (fail-safe).
	}

	return &pluginv1.ResolveResourceResponse{Attributes: attrs}, nil
}
