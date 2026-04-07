// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

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
	// Reject any resource type other than "widget". This catches host-side
	// misrouting bugs (e.g., a per-resource-type registration regression
	// that sends `location:abc` to this resolver) — without it, the E2E
	// coverage would silently pass on routing bugs.
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
