// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"strings"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/types"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// PluginAttributeProvider implements attribute.AttributeProvider by routing
// ResolveResource calls to a plugin's AttributeResolver gRPC service.
// Subjects are not resolved (plugins only own resource types).
type PluginAttributeProvider struct {
	namespace string
	client    pluginv1.AttributeResolverServiceClient
	schema    *types.NamespaceSchema
}

// NewPluginAttributeProvider creates a provider for the given namespace that
// delegates resource attribute resolution to the plugin over gRPC.
func NewPluginAttributeProvider(
	namespace string,
	client pluginv1.AttributeResolverServiceClient,
	schema *types.NamespaceSchema,
) *PluginAttributeProvider {
	return &PluginAttributeProvider{
		namespace: namespace,
		client:    client,
		schema:    schema,
	}
}

// Namespace returns the resource type name this provider resolves.
func (p *PluginAttributeProvider) Namespace() string {
	return p.namespace
}

// ResolveSubject returns nil — plugins do not resolve principal attributes.
func (p *PluginAttributeProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

// ResolveResource routes to the plugin's AttributeResolver gRPC service.
func (p *PluginAttributeProvider) ResolveResource(ctx context.Context, resourceID string) (map[string]any, error) {
	prefix := p.namespace + ":"
	if !strings.HasPrefix(resourceID, prefix) {
		return nil, nil
	}
	id := resourceID[len(prefix):]

	resp, err := p.client.ResolveResource(ctx, &pluginv1.ResolveResourceRequest{
		ResourceType: p.namespace,
		ResourceId:   id,
	})
	if err != nil {
		return nil, oops.With("namespace", p.namespace).With("resource_id", id).
			Wrapf(err, "plugin attribute resolution failed")
	}

	return convertProtoAttributes(resp.GetAttributes()), nil
}

// Schema returns the namespace schema discovered via GetSchema at load time.
func (p *PluginAttributeProvider) Schema() *types.NamespaceSchema {
	return p.schema
}

// convertProtoAttributes converts proto AttributeValue map to map[string]any.
func convertProtoAttributes(attrs map[string]*pluginv1.AttributeValue) map[string]any {
	if len(attrs) == 0 {
		return nil
	}
	result := make(map[string]any, len(attrs))
	for k, v := range attrs {
		switch val := v.GetKind().(type) {
		case *pluginv1.AttributeValue_StringValue:
			result[k] = val.StringValue
		case *pluginv1.AttributeValue_NumberValue:
			result[k] = val.NumberValue
		case *pluginv1.AttributeValue_BoolValue:
			result[k] = val.BoolValue
		case *pluginv1.AttributeValue_StringListValue:
			if val.StringListValue != nil {
				result[k] = val.StringListValue.Values
			}
		}
	}
	return result
}

// ConvertProtoSchema converts a GetSchemaResponse into a map of NamespaceSchema
// keyed by resource type name. Used during plugin load.
func ConvertProtoSchema(resp *pluginv1.GetSchemaResponse) map[string]*types.NamespaceSchema {
	if resp == nil {
		return nil
	}
	result := make(map[string]*types.NamespaceSchema, len(resp.GetResourceTypes()))
	for typeName, typeSchema := range resp.GetResourceTypes() {
		ns := &types.NamespaceSchema{
			Attributes: make(map[string]types.AttrType, len(typeSchema.GetAttributes())),
		}
		for attrName, attrType := range typeSchema.GetAttributes() {
			ns.Attributes[attrName] = protoAttrTypeToAttrType(attrType)
		}
		result[typeName] = ns
	}
	return result
}

func protoAttrTypeToAttrType(pt pluginv1.AttributeType) types.AttrType {
	switch pt {
	case pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING:
		return types.AttrTypeString
	case pluginv1.AttributeType_ATTRIBUTE_TYPE_BOOL:
		return types.AttrTypeBool
	case pluginv1.AttributeType_ATTRIBUTE_TYPE_FLOAT:
		return types.AttrTypeFloat
	case pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST:
		return types.AttrTypeStringList
	default:
		return types.AttrTypeString
	}
}
