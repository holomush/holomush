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

// resourceTypeScene is the single resource type the scene plugin owns.
// Declared as a constant so the resolver and the manifest stay in sync.
const resourceTypeScene = "scene"

// SceneResolver implements pluginv1.AttributeResolverServiceServer for the
// scene plugin. It exposes the schema of attributes the plugin can resolve
// (GetSchema) and resolves attributes for individual scene resources
// (ResolveResource) when called by the host's ABAC engine during policy
// evaluation.
//
// Per the spec section 5.5 hard-privacy boundary, this resolver MUST NOT
// expose log content, vote tallies, or any other content that lives behind
// the privacy boundary. It exposes only non-content attributes
// (id/owner/state/visibility/location/has_location/participants/invitees)
// for use by scene read-access policies.
type SceneResolver struct {
	pluginv1.UnimplementedAttributeResolverServiceServer
	store sceneStorer
}

// NewSceneResolver returns a resolver backed by the given store.
func NewSceneResolver(store sceneStorer) *SceneResolver {
	return &SceneResolver{store: store}
}

// GetSchema returns the attribute schema the plugin can resolve. Called
// once by the host's plugin manager after host.Load returns.
func (r *SceneResolver) GetSchema(ctx context.Context, _ *pluginv1.GetSchemaRequest) (*pluginv1.GetSchemaResponse, error) {
	_, span := startSpan(ctx, "scene.resolver.get_schema")
	defer span.End()

	return &pluginv1.GetSchemaResponse{
		ResourceTypes: map[string]*pluginv1.ResourceTypeSchema{
			resourceTypeScene: {
				Attributes: map[string]pluginv1.AttributeType{
					"id":           pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
					"owner":        pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
					"state":        pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
					"visibility":   pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
					"location":     pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
					"has_location": pluginv1.AttributeType_ATTRIBUTE_TYPE_BOOL,
					"participants": pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST,
					"invitees":     pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST,
				},
			},
		},
	}, nil
}

// ResolveResource returns the attributes for a specific scene resource.
// Called by the host's ABAC engine when evaluating a policy that references
// a scene attribute (e.g., the read-own-scene policy that checks
// resource.scene.owner).
//
// Resource type other than "scene" is rejected with InvalidArgument so
// host-side misrouting bugs surface immediately.
func (r *SceneResolver) ResolveResource(ctx context.Context, req *pluginv1.ResolveResourceRequest) (*pluginv1.ResolveResourceResponse, error) {
	ctx, span := startSpan(
		ctx, "scene.resolver.resolve_resource",
		attribute.String("resource_type", req.GetResourceType()),
		attribute.String("resource_id", req.GetResourceId()),
	)
	defer span.End()

	if req.GetResourceType() != resourceTypeScene {
		err := status.Errorf(codes.InvalidArgument,
			"core-scenes only resolves resource type %q, got %q",
			resourceTypeScene, req.GetResourceType())
		recordError(span, err)
		return nil, err
	}

	row, participants, invitees, err := r.store.GetWithMembership(ctx, req.GetResourceId())
	if err != nil {
		recordError(span, err)
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "SCENE_NOT_FOUND" {
			return nil, status.Errorf(codes.NotFound, "scene not found: %s", req.GetResourceId())
		}
		// Inner error is already captured on the span via recordError above;
		// returning a generic message keeps internal detail (table names, query
		// fragments) off the wire per .claude/rules/grpc-errors.md.
		return nil, status.Error(codes.Internal, "failed to resolve scene") //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	attrs := map[string]*pluginv1.AttributeValue{
		"id":         {Kind: &pluginv1.AttributeValue_StringValue{StringValue: row.ID}},
		"owner":      {Kind: &pluginv1.AttributeValue_StringValue{StringValue: row.OwnerID}},
		"state":      {Kind: &pluginv1.AttributeValue_StringValue{StringValue: row.State}},
		"visibility": {Kind: &pluginv1.AttributeValue_StringValue{StringValue: row.Visibility}},
		"participants": {Kind: &pluginv1.AttributeValue_StringListValue{
			StringListValue: &pluginv1.StringList{Values: participants},
		}},
		"invitees": {Kind: &pluginv1.AttributeValue_StringListValue{
			StringListValue: &pluginv1.StringList{Values: invitees},
		}},
	}

	// Optional attribute: emit location only when resolved; the has_location
	// witness is always present. An empty-string sentinel would fail-open in the
	// DSL evaluator (a present "" is a value, not a missing key), so the key is
	// omitted when absent — .claude/rules/abac-providers.md, ADR holomush-ti1b.
	if row.LocationID != nil && *row.LocationID != "" {
		attrs["location"] = &pluginv1.AttributeValue{Kind: &pluginv1.AttributeValue_StringValue{StringValue: *row.LocationID}}
		attrs["has_location"] = &pluginv1.AttributeValue{Kind: &pluginv1.AttributeValue_BoolValue{BoolValue: true}}
	} else {
		attrs["has_location"] = &pluginv1.AttributeValue{Kind: &pluginv1.AttributeValue_BoolValue{BoolValue: false}}
		// location key INTENTIONALLY ABSENT (fail-safe).
	}

	return &pluginv1.ResolveResourceResponse{Attributes: attrs}, nil
}
