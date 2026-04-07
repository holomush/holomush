// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// recordingRegistrar is a no-op grpc.ServiceRegistrar that captures the
// services passed to RegisterService so tests can assert wiring without
// spinning up a real grpc.Server.
type recordingRegistrar struct {
	services []*grpc.ServiceDesc
}

func (r *recordingRegistrar) RegisterService(desc *grpc.ServiceDesc, _ any) {
	r.services = append(r.services, desc)
}

func TestWidgetPluginHandleCommandReturnsOKWithEchoedArgs(t *testing.T) {
	p := &widgetPlugin{}
	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "widget",
		Args:    "abc-123",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "abc-123",
		"command output should echo the supplied args")
}

func TestWidgetPluginHandleEventReturnsNilWithoutError(t *testing.T) {
	p := &widgetPlugin{}
	emits, err := p.HandleEvent(context.Background(), pluginsdk.Event{Type: "test"})
	require.NoError(t, err)
	assert.Nil(t, emits, "test plugin emits no events")
}

func TestWidgetPluginInitReturnsNilForEmptyConfig(t *testing.T) {
	p := &widgetPlugin{}
	require.NoError(t, p.Init(context.Background(), &pluginv1.ServiceConfig{}))
}

func TestWidgetPluginRegisterServicesIsNoOpWithNilRegistrar(t *testing.T) {
	p := &widgetPlugin{}
	assert.NotPanics(t, func() { p.RegisterServices(nil) },
		"RegisterServices is intentionally a no-op for the test plugin")
}

func TestWidgetResolverGetSchemaDeclaresWidgetTypeWithStringAttributes(t *testing.T) {
	r := &widgetResolver{}
	resp, err := r.GetSchema(context.Background(), &pluginv1.GetSchemaRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Contains(t, resp.ResourceTypes, "widget")
	schema := resp.ResourceTypes["widget"]
	require.NotNil(t, schema)
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING, schema.Attributes["type"])
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING, schema.Attributes["owner"])
}

func TestWidgetResolverResolveResourceReturnsNormalForDefaultID(t *testing.T) {
	r := &widgetResolver{}
	resp, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: "widget",
		ResourceId:   "widget-normal-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	typeAttr := resp.Attributes["type"]
	require.NotNil(t, typeAttr)
	assert.Equal(t, "normal", typeAttr.GetStringValue(),
		"non-restricted IDs should resolve to 'normal' type")

	ownerAttr := resp.Attributes["owner"]
	require.NotNil(t, ownerAttr)
	assert.Equal(t, "test-owner", ownerAttr.GetStringValue())
}

func TestWidgetResolverResolveResourceReturnsRestrictedForRestrictedID(t *testing.T) {
	r := &widgetResolver{}
	resp, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: "widget",
		ResourceId:   "widget-restricted-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	typeAttr := resp.Attributes["type"]
	require.NotNil(t, typeAttr)
	assert.Equal(t, "restricted", typeAttr.GetStringValue(),
		"IDs containing 'restricted' should resolve to 'restricted' type")

	ownerAttr := resp.Attributes["owner"]
	require.NotNil(t, ownerAttr)
	assert.Equal(t, "system", ownerAttr.GetStringValue(),
		"restricted widgets should be owned by 'system'")
}

func TestWidgetResolverResolveResourceRejectsForeignResourceType(t *testing.T) {
	// The fixture must reject any resource type other than "widget" so that a
	// host-side misrouting bug (e.g. sending location/gadget lookups here)
	// fails loudly instead of silently passing E2E coverage.
	r := &widgetResolver{}
	_, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: "location",
		ResourceId:   "loc-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "location")
}

func TestWidgetPluginRegisterAttributeResolverDoesNotPanic(t *testing.T) {
	// RegisterAttributeResolver is exercised end-to-end by the integration
	// test; here we just ensure the wiring with a nil registrar would panic
	// (it must not be called with nil in practice). Pass a no-op stub that
	// records the registration so we exercise the call path safely.
	rec := &recordingRegistrar{}
	p := &widgetPlugin{}
	assert.NotPanics(t, func() { p.RegisterAttributeResolver(rec) })
	assert.NotEmpty(t, rec.services,
		"RegisterAttributeResolver should call into the registrar")
}
