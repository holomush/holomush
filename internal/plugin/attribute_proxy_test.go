// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/access/policy/types"
	plugins "github.com/holomush/holomush/internal/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// mockAttributeResolverClient implements pluginv1.AttributeResolverClient for tests.
type mockAttributeResolverClient struct {
	response *pluginv1.ResolveResourceResponse
	err      error
	called   bool
}

func (m *mockAttributeResolverClient) GetSchema(_ context.Context, _ *pluginv1.GetSchemaRequest, _ ...grpc.CallOption) (*pluginv1.GetSchemaResponse, error) {
	return nil, nil
}

func (m *mockAttributeResolverClient) ResolveResource(_ context.Context, _ *pluginv1.ResolveResourceRequest, _ ...grpc.CallOption) (*pluginv1.ResolveResourceResponse, error) {
	m.called = true
	return m.response, m.err
}

func TestPluginAttributeProviderNamespaceReturnsConfiguredValue(t *testing.T) {
	p := plugins.NewPluginAttributeProvider("widget", nil, nil)
	assert.Equal(t, "widget", p.Namespace())
}

func TestPluginAttributeProviderResolveSubjectReturnsNil(t *testing.T) {
	p := plugins.NewPluginAttributeProvider("widget", nil, nil)
	attrs, err := p.ResolveSubject(context.Background(), "character:abc")
	require.NoError(t, err)
	assert.Nil(t, attrs)
}

func TestPluginAttributeProviderResolveResourceRoutesToGRPC(t *testing.T) {
	mock := &mockAttributeResolverClient{
		response: &pluginv1.ResolveResourceResponse{
			Attributes: map[string]*pluginv1.AttributeValue{
				"type":   {Kind: &pluginv1.AttributeValue_StringValue{StringValue: "normal"}},
				"active": {Kind: &pluginv1.AttributeValue_BoolValue{BoolValue: true}},
				"score":  {Kind: &pluginv1.AttributeValue_NumberValue{NumberValue: 42.5}},
				"tags": {Kind: &pluginv1.AttributeValue_StringListValue{
					StringListValue: &pluginv1.StringList{Values: []string{"a", "b"}},
				}},
			},
		},
	}

	p := plugins.NewPluginAttributeProvider("widget", mock, nil)
	attrs, err := p.ResolveResource(context.Background(), "widget:abc123")
	require.NoError(t, err)

	assert.Equal(t, "normal", attrs["type"])
	assert.Equal(t, true, attrs["active"])
	assert.Equal(t, 42.5, attrs["score"])
	assert.Equal(t, []string{"a", "b"}, attrs["tags"])
}

func TestPluginAttributeProviderResolveResourceSkipsWrongPrefix(t *testing.T) {
	mock := &mockAttributeResolverClient{}
	p := plugins.NewPluginAttributeProvider("widget", mock, nil)
	attrs, err := p.ResolveResource(context.Background(), "location:abc123")
	require.NoError(t, err)
	assert.Nil(t, attrs)
	assert.False(t, mock.called)
}

func TestPluginAttributeProviderResolveResourcePropagatesGRPCError(t *testing.T) {
	mock := &mockAttributeResolverClient{
		err: errors.New("connection refused"),
	}
	p := plugins.NewPluginAttributeProvider("widget", mock, nil)
	_, err := p.ResolveResource(context.Background(), "widget:abc123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin attribute resolution failed")
	assert.Contains(t, err.Error(), "connection refused")
}

func TestPluginAttributeProviderSchemaReturnsConfiguredSchema(t *testing.T) {
	schema := &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"type":  types.AttrTypeString,
			"score": types.AttrTypeFloat,
		},
	}
	p := plugins.NewPluginAttributeProvider("widget", nil, schema)
	assert.Equal(t, schema, p.Schema())
}

func TestConvertProtoSchemaConvertsResourceTypes(t *testing.T) {
	resp := &pluginv1.GetSchemaResponse{
		ResourceTypes: map[string]*pluginv1.ResourceTypeSchema{
			"channel": {
				Attributes: map[string]pluginv1.AttributeType{
					"name":    pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
					"locked":  pluginv1.AttributeType_ATTRIBUTE_TYPE_BOOL,
					"weight":  pluginv1.AttributeType_ATTRIBUTE_TYPE_FLOAT,
					"members": pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST,
				},
			},
		},
	}

	result := plugins.ConvertProtoSchema(resp)
	require.Len(t, result, 1)

	ch := result["channel"]
	require.NotNil(t, ch)
	assert.Equal(t, types.AttrTypeString, ch.Attributes["name"])
	assert.Equal(t, types.AttrTypeBool, ch.Attributes["locked"])
	assert.Equal(t, types.AttrTypeFloat, ch.Attributes["weight"])
	assert.Equal(t, types.AttrTypeStringList, ch.Attributes["members"])
}

func TestConvertProtoSchemaReturnsNilForNilResponse(t *testing.T) {
	assert.Nil(t, plugins.ConvertProtoSchema(nil))
}

func TestConvertProtoSchemaDefaultsUnspecifiedTypeToString(t *testing.T) {
	resp := &pluginv1.GetSchemaResponse{
		ResourceTypes: map[string]*pluginv1.ResourceTypeSchema{
			"thing": {
				Attributes: map[string]pluginv1.AttributeType{
					"unknown": pluginv1.AttributeType_ATTRIBUTE_TYPE_UNSPECIFIED,
				},
			},
		},
	}

	result := plugins.ConvertProtoSchema(resp)
	assert.Equal(t, types.AttrTypeString, result["thing"].Attributes["unknown"])
}
