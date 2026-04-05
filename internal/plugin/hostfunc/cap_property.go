// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"

	lua "github.com/yuin/gopher-lua"
)

// PropertyInfo carries a single property entry for use within the hostfunc package.
// It mirrors the property types used by the host service to avoid cross-package coupling
// in the narrow-interface layer.
type PropertyInfo struct {
	Name       string
	Value      string
	Visibility string
	ParentType string
	ParentID   string
}

// PropertyAccess is the narrow interface required by PropertyCapability.
// It covers the property operations needed by the capability module without
// exposing the full host service surface.
type PropertyAccess interface {
	// ListPropertiesByParent returns all properties owned by a parent entity.
	ListPropertiesByParent(ctx context.Context, subjectID, parentType, parentID string) ([]PropertyInfo, error)

	// FindPropertyByPrefix returns all property definitions whose name starts with the given prefix.
	FindPropertyByPrefix(ctx context.Context, prefix string) ([]PropertyInfo, error)

	// UpdateCharacterDescription sets the description property on a character.
	UpdateCharacterDescription(ctx context.Context, subjectID, characterID, description string) error
}

// PropertyCapability implements the Capability interface for the property namespace.
// It registers Lua host functions under the global "property" table.
type PropertyCapability struct {
	properties PropertyAccess
}

// Ensure PropertyCapability satisfies the Capability interface at compile time.
var _ Capability = (*PropertyCapability)(nil)

// NewPropertyCapability creates a PropertyCapability backed by the given PropertyAccess.
func NewPropertyCapability(properties PropertyAccess) *PropertyCapability {
	return &PropertyCapability{properties: properties}
}

// Namespace returns "property", the Lua global table name for this capability.
func (c *PropertyCapability) Namespace() string {
	return "property"
}

// Register injects the property.* functions into the Lua state as a global table.
func (c *PropertyCapability) Register(L *lua.LState, pluginName string) { //nolint:gocritic // L is conventional gopher-lua parameter name
	tbl := L.NewTable()
	L.SetField(tbl, "list_by_parent", L.NewFunction(c.listByParentFn(pluginName)))
	L.SetField(tbl, "find_by_prefix", L.NewFunction(c.findByPrefixFn(pluginName)))
	L.SetField(tbl, "update_character_description", L.NewFunction(c.updateCharacterDescriptionFn(pluginName)))
	L.SetGlobal("property", tbl)
}

// listByParentFn returns a Lua function implementing property.list_by_parent(subject_id, parent_type, parent_id).
// Returns an array table of {name, value, visibility} tables, or nil + error string on failure.
func (c *PropertyCapability) listByParentFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		subjectID := L.CheckString(1)
		parentType := L.CheckString(2)
		parentID := L.CheckString(3)

		ctx := luaContext(L)
		props, err := c.properties.ListPropertiesByParent(ctx, subjectID, parentType, parentID)
		if err != nil {
			msg := SanitizeErrorForPlugin(PluginErrorContext{
				Plugin:    pluginName,
				Operation: "list_by_parent",
				Subject:   "property",
				SubjectID: parentID,
			}, err)
			L.Push(lua.LNil)
			L.Push(lua.LString(msg))
			return 2
		}

		arr := L.NewTable()
		for _, p := range props {
			row := L.NewTable()
			L.SetField(row, "name", lua.LString(p.Name))
			L.SetField(row, "value", lua.LString(p.Value))
			L.SetField(row, "visibility", lua.LString(p.Visibility))
			arr.Append(row)
		}
		L.Push(arr)
		return 1
	}
}

// findByPrefixFn returns a Lua function implementing property.find_by_prefix(prefix).
// Returns an array table of {name, value, visibility, parent_type, parent_id} tables,
// or nil + error string on failure.
func (c *PropertyCapability) findByPrefixFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		prefix := L.CheckString(1)

		ctx := luaContext(L)
		props, err := c.properties.FindPropertyByPrefix(ctx, prefix)
		if err != nil {
			msg := SanitizeErrorForPlugin(PluginErrorContext{
				Plugin:    pluginName,
				Operation: "find_by_prefix",
				Subject:   "property",
				SubjectID: prefix,
			}, err)
			L.Push(lua.LNil)
			L.Push(lua.LString(msg))
			return 2
		}

		arr := L.NewTable()
		for _, p := range props {
			row := L.NewTable()
			L.SetField(row, "name", lua.LString(p.Name))
			L.SetField(row, "value", lua.LString(p.Value))
			L.SetField(row, "visibility", lua.LString(p.Visibility))
			L.SetField(row, "parent_type", lua.LString(p.ParentType))
			L.SetField(row, "parent_id", lua.LString(p.ParentID))
			arr.Append(row)
		}
		L.Push(arr)
		return 1
	}
}

// updateCharacterDescriptionFn returns a Lua function implementing
// property.update_character_description(subject_id, character_id, description).
// Returns nil on success, or nil + error string on failure.
func (c *PropertyCapability) updateCharacterDescriptionFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		subjectID := L.CheckString(1)
		characterID := L.CheckString(2)
		description := L.CheckString(3)

		ctx := luaContext(L)
		if err := c.properties.UpdateCharacterDescription(ctx, subjectID, characterID, description); err != nil {
			msg := SanitizeErrorForPlugin(PluginErrorContext{
				Plugin:    pluginName,
				Operation: "update_character_description",
				Subject:   "property",
				SubjectID: characterID,
			}, err)
			L.Push(lua.LNil)
			L.Push(lua.LString(msg))
			return 2
		}
		return 0
	}
}
