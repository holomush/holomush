// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package main implements the test-abac-widget binary plugin used by the
// host's plugin integration tests to exercise the ABAC attribute resolver
// and command dispatcher contracts. Not deployed in production.
//
// Why this fixture is kept (decision holomush-6g45d): it is the only
// dependency-free, deterministic binary AttributeResolver in the tree.
// core-scenes is the only other plugin that resolves ABAC attributes, but it
// requires WorldService + Postgres + scene seeding and resolves
// non-deterministic, data-dependent values — so it cannot substitute for the
// deterministic permit/forbid and zero-RPC-preflight assertions in
// test/integration/plugin/abac_widget_test.go. Replacing the binary with a Lua
// fixture would exercise the Lua resolver path instead of the host↔binary gRPC
// AttributeResolverService contract those tests target. Hence: keep.
//
// (Unrelated to the keep decision: this manifest's provides: entry for
// AttributeResolverService is a known anomaly tracked in holomush-vr6yo —
// AttributeResolverService is host-auto-registered and MUST NOT be declared in
// provides:. Do not copy that line into a new plugin.)
package main

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

type widgetPlugin struct{}

func (p *widgetPlugin) HandleEvent(_ context.Context, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	return nil, nil
}

//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
func (p *widgetPlugin) HandleCommand(_ context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: "Widget: " + req.Args,
	}, nil
}

func (p *widgetPlugin) RegisterServices(_ grpc.ServiceRegistrar) {}

func (p *widgetPlugin) Init(_ context.Context, _ *pluginv1.ServiceConfig) error {
	return nil
}

func (p *widgetPlugin) RegisterAttributeResolver(registrar grpc.ServiceRegistrar) {
	pluginv1.RegisterAttributeResolverServiceServer(registrar, &widgetResolver{})
}

// widgetResolver is the canonical example of an ABAC attribute resolver
// for a binary plugin. It illustrates two properties the host contract
// guarantees:
//
//  1. The host only calls ResolveResource with real instance IDs it
//     believes exist. There is no preflight sentinel to handle.
//  2. Every attribute returned here ("type", "owner") must also appear
//     in GetSchema — otherwise the returned value is silently dropped
//     at runtime and the plugin fails to load if a policy references
//     the undeclared attribute.
//
// Plugin authors writing new resolvers should model theirs on this
// pattern: map instance ID → backing store lookup → return a map keyed
// by names declared in GetSchema.
type widgetResolver struct {
	pluginv1.UnimplementedAttributeResolverServiceServer
}

func (r *widgetResolver) GetSchema(_ context.Context, _ *pluginv1.GetSchemaRequest) (*pluginv1.GetSchemaResponse, error) {
	return &pluginv1.GetSchemaResponse{
		ResourceTypes: map[string]*pluginv1.ResourceTypeSchema{
			"widget": {
				Attributes: map[string]pluginv1.AttributeType{
					"type":  pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
					"owner": pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
				},
			},
		},
	}, nil
}

func (r *widgetResolver) ResolveResource(_ context.Context, req *pluginv1.ResolveResourceRequest) (*pluginv1.ResolveResourceResponse, error) {
	// Reject any resource type other than "widget". This is defense in
	// depth against a host routing bug (e.g., a per-resource-type
	// registration regression that sends `location:abc` to this resolver).
	// The host contract guarantees that ResolveResource is only called
	// with instance IDs the host believes to exist; this check protects
	// against the host violating that contract, not against synthetic
	// preflight IDs (which no longer exist — see spec
	// 2026-04-07-plugin-abac-hardening-design.md).
	if req.GetResourceType() != "widget" {
		return nil, status.Errorf(codes.InvalidArgument,
			"test-abac-widget only resolves resource type %q, got %q",
			"widget", req.GetResourceType())
	}

	widgetType := "normal"
	owner := "test-owner"
	if strings.Contains(req.GetResourceId(), "restricted") {
		widgetType = "restricted"
		owner = "system"
	}

	return &pluginv1.ResolveResourceResponse{
		Attributes: map[string]*pluginv1.AttributeValue{
			"type":  {Kind: &pluginv1.AttributeValue_StringValue{StringValue: widgetType}},
			"owner": {Kind: &pluginv1.AttributeValue_StringValue{StringValue: owner}},
		},
	}, nil
}

func main() {
	plugin := &widgetPlugin{}
	pluginsdk.ServeWithServices(
		&pluginsdk.ServeConfig{Handler: plugin},
		plugin,
	)
}
