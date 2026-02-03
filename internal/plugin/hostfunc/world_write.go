// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"errors"
	"log/slog"

	"github.com/oklog/ulid/v2"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/holo"
)

// WorldMutator is an alias for world.Mutator used for type assertions.
// The interface is defined in internal/world/mutator.go.
type WorldMutator = world.Mutator

// propertyRegistry is used to validate properties for entity types.
// Uses the default registry from pkg/holo which defines standard properties.
var propertyRegistry = holo.DefaultRegistry()

// createLocationFn returns a Lua function that creates a new location.
// Lua signature: create_location(name, description, type) -> {id, name} or nil, error
func (f *Functions) createLocationFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			slog.Error("create_location called but world service unavailable",
				"plugin", pluginName,
				"hint", "use WithWorldService option when creating hostfunc.Functions")
			L.Push(lua.LNil)
			L.Push(lua.LString("world service not configured - contact server administrator"))
			return 2
		}

		mutator, ok := f.worldService.(WorldMutator)
		if !ok {
			slog.Warn("create_location called but world service does not support mutations",
				"plugin", pluginName)
			L.Push(lua.LNil)
			L.Push(lua.LString("world service does not support mutations"))
			return 2
		}

		name := L.CheckString(1)
		description := L.CheckString(2)
		locTypeStr := L.CheckString(3)

		locType := world.LocationType(locTypeStr)
		if err := locType.Validate(); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid location type: " + locTypeStr))
			return 2
		}

		loc := &world.Location{
			ID:          ulid.Make(),
			Name:        name,
			Description: description,
			Type:        locType,
		}

		// Inherit context from Lua state if available, otherwise use Background
		parentCtx := L.Context()
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(parentCtx, defaultPluginQueryTimeout)
		defer cancel()

		subjectID := "system:plugin:" + pluginName
		if err := mutator.CreateLocation(ctx, subjectID, loc); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(sanitizeErrorForPlugin(pluginName, "location", name, err)))
			return 2
		}

		result := L.NewTable()
		L.SetField(result, "id", lua.LString(loc.ID.String()))
		L.SetField(result, "name", lua.LString(loc.Name))
		L.Push(result)
		L.Push(lua.LNil)
		return 2
	}
}

// createExitFn returns a Lua function that creates a new exit.
// Lua signature: create_exit(from_id, to_id, name, opts) -> {id, name} or nil, error
// opts: { bidirectional = true, return_name = "south" }
func (f *Functions) createExitFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			slog.Error("create_exit called but world service unavailable",
				"plugin", pluginName,
				"hint", "use WithWorldService option when creating hostfunc.Functions")
			L.Push(lua.LNil)
			L.Push(lua.LString("world service not configured - contact server administrator"))
			return 2
		}

		mutator, ok := f.worldService.(WorldMutator)
		if !ok {
			slog.Warn("create_exit called but world service does not support mutations",
				"plugin", pluginName)
			L.Push(lua.LNil)
			L.Push(lua.LString("world service does not support mutations"))
			return 2
		}

		fromIDStr := L.CheckString(1)
		toIDStr := L.CheckString(2)
		name := L.CheckString(3)

		fromID, err := ulid.Parse(fromIDStr)
		if err != nil {
			slog.Debug("create_exit: invalid from_id format",
				"plugin", pluginName,
				"from_id", fromIDStr,
				"error", err)
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid from_id: " + err.Error()))
			return 2
		}

		toID, err := ulid.Parse(toIDStr)
		if err != nil {
			slog.Debug("create_exit: invalid to_id format",
				"plugin", pluginName,
				"to_id", toIDStr,
				"error", err)
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid to_id: " + err.Error()))
			return 2
		}

		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: fromID,
			ToLocationID:   toID,
			Name:           name,
			Visibility:     world.VisibilityAll,
		}

		// Parse optional options table
		if L.GetTop() >= 4 && L.Get(4).Type() == lua.LTTable {
			opts := L.ToTable(4)
			if bidir := opts.RawGetString("bidirectional"); bidir.Type() == lua.LTBool {
				if bidirBool, ok := bidir.(lua.LBool); ok {
					exit.Bidirectional = bool(bidirBool)
				}
			}
			if retName := opts.RawGetString("return_name"); retName.Type() == lua.LTString {
				if retNameStr, ok := retName.(lua.LString); ok {
					exit.ReturnName = string(retNameStr)
				}
			}
		}

		// Inherit context from Lua state if available, otherwise use Background
		parentCtx := L.Context()
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(parentCtx, defaultPluginQueryTimeout)
		defer cancel()

		subjectID := "system:plugin:" + pluginName
		if err := mutator.CreateExit(ctx, subjectID, exit); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(sanitizeErrorForPlugin(pluginName, "exit", name, err)))
			return 2
		}

		result := L.NewTable()
		L.SetField(result, "id", lua.LString(exit.ID.String()))
		L.SetField(result, "name", lua.LString(exit.Name))
		L.Push(result)
		L.Push(lua.LNil)
		return 2
	}
}

// createObjectFn returns a Lua function that creates a new object.
// Lua signature: create_object(name, opts) -> {id, name} or nil, error
// opts: { location_id = "...", character_id = "...", container_id = "...", description = "..." }
// Exactly one containment field must be specified.
func (f *Functions) createObjectFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			slog.Error("create_object called but world service unavailable",
				"plugin", pluginName,
				"hint", "use WithWorldService option when creating hostfunc.Functions")
			L.Push(lua.LNil)
			L.Push(lua.LString("world service not configured - contact server administrator"))
			return 2
		}

		mutator, ok := f.worldService.(WorldMutator)
		if !ok {
			slog.Warn("create_object called but world service does not support mutations",
				"plugin", pluginName)
			L.Push(lua.LNil)
			L.Push(lua.LString("world service does not support mutations"))
			return 2
		}

		name := L.CheckString(1)
		opts := L.ToTable(2)

		// Parse containment from options
		var containment world.Containment
		containmentCount := 0

		if locIDVal := opts.RawGetString("location_id"); locIDVal.Type() == lua.LTString {
			locID, err := ulid.Parse(locIDVal.String())
			if err != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString("invalid location_id: " + err.Error()))
				return 2
			}
			containment = world.InLocation(locID)
			containmentCount++
		}

		if charIDVal := opts.RawGetString("character_id"); charIDVal.Type() == lua.LTString {
			charID, err := ulid.Parse(charIDVal.String())
			if err != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString("invalid character_id: " + err.Error()))
				return 2
			}
			containment = world.HeldByCharacter(charID)
			containmentCount++
		}

		if containerIDVal := opts.RawGetString("container_id"); containerIDVal.Type() == lua.LTString {
			containerID, err := ulid.Parse(containerIDVal.String())
			if err != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString("invalid container_id: " + err.Error()))
				return 2
			}
			containment = world.ContainedInObject(containerID)
			containmentCount++
		}

		if containmentCount != 1 {
			L.Push(lua.LNil)
			L.Push(lua.LString("must specify exactly one containment: location_id, character_id, or container_id"))
			return 2
		}

		obj, err := world.NewObjectWithID(ulid.Make(), name, containment)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid object: " + err.Error()))
			return 2
		}

		// Optional description
		if descVal := opts.RawGetString("description"); descVal.Type() == lua.LTString {
			obj.Description = descVal.String()
		}

		// Inherit context from Lua state if available, otherwise use Background
		parentCtx := L.Context()
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(parentCtx, defaultPluginQueryTimeout)
		defer cancel()

		subjectID := "system:plugin:" + pluginName
		if err := mutator.CreateObject(ctx, subjectID, obj); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(sanitizeErrorForPlugin(pluginName, "object", name, err)))
			return 2
		}

		result := L.NewTable()
		L.SetField(result, "id", lua.LString(obj.ID.String()))
		L.SetField(result, "name", lua.LString(obj.Name))
		L.Push(result)
		L.Push(lua.LNil)
		return 2
	}
}

// findLocationFn returns a Lua function that finds a location by name.
// Lua signature: find_location(name) -> {id, name} or nil, error
func (f *Functions) findLocationFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			slog.Error("find_location called but world service unavailable",
				"plugin", pluginName,
				"hint", "use WithWorldService option when creating hostfunc.Functions")
			L.Push(lua.LNil)
			L.Push(lua.LString("world service not configured - contact server administrator"))
			return 2
		}

		mutator, ok := f.worldService.(WorldMutator)
		if !ok {
			slog.Warn("find_location called but world service does not support FindLocationByName",
				"plugin", pluginName)
			L.Push(lua.LNil)
			L.Push(lua.LString("world service does not support location search"))
			return 2
		}

		name := L.CheckString(1)

		// Inherit context from Lua state if available, otherwise use Background
		parentCtx := L.Context()
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(parentCtx, defaultPluginQueryTimeout)
		defer cancel()

		subjectID := "system:plugin:" + pluginName
		loc, err := mutator.FindLocationByName(ctx, subjectID, name)
		if err != nil {
			if errors.Is(err, world.ErrNotFound) {
				slog.Debug("find_location: location not found",
					"plugin", pluginName,
					"name", name)
			}
			L.Push(lua.LNil)
			L.Push(lua.LString(sanitizeErrorForPlugin(pluginName, "location", name, err)))
			return 2
		}

		result := L.NewTable()
		L.SetField(result, "id", lua.LString(loc.ID.String()))
		L.SetField(result, "name", lua.LString(loc.Name))
		L.Push(result)
		L.Push(lua.LNil)
		return 2
	}
}

// setPropertyFn returns a Lua function that sets a property on an entity.
// Lua signature: set_property(entity_type, entity_id, property, value) -> true or nil, error
// entity_type: "location" or "object"
// property: "name" or "description"
func (f *Functions) setPropertyFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			slog.Error("set_property called but world service unavailable",
				"plugin", pluginName,
				"hint", "use WithWorldService option when creating hostfunc.Functions")
			L.Push(lua.LNil)
			L.Push(lua.LString("world service not configured - contact server administrator"))
			return 2
		}

		mutator, ok := f.worldService.(WorldMutator)
		if !ok {
			slog.Warn("set_property called but world service does not support mutations",
				"plugin", pluginName)
			L.Push(lua.LNil)
			L.Push(lua.LString("world service does not support mutations"))
			return 2
		}

		entityType := L.CheckString(1)
		entityIDStr := L.CheckString(2)
		property := L.CheckString(3)
		value := L.CheckString(4)

		// Validate entity type
		if entityType != "location" && entityType != "object" {
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid entity type: " + entityType + " (must be 'location' or 'object')"))
			return 2
		}

		// Validate entity ID
		entityID, err := ulid.Parse(entityIDStr)
		if err != nil {
			slog.Debug("set_property: invalid entity_id format",
				"plugin", pluginName,
				"entity_id", entityIDStr,
				"error", err)
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid entity_id: " + err.Error()))
			return 2
		}

		// Validate property using PropertyRegistry
		if !propertyRegistry.ValidFor(entityType, property) {
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid property: " + property + " for " + entityType))
			return 2
		}

		// Inherit context from Lua state if available, otherwise use Background
		parentCtx := L.Context()
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(parentCtx, defaultPluginQueryTimeout)
		defer cancel()

		subjectID := "system:plugin:" + pluginName

		// Get and update the entity
		switch entityType {
		case "location":
			adapter := NewWorldQuerierAdapter(f.worldService, pluginName)
			loc, err := adapter.GetLocation(ctx, entityID)
			if err != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(sanitizeErrorForPlugin(pluginName, "location", entityIDStr, err)))
				return 2
			}
			switch property {
			case "name":
				loc.Name = value
			case "description":
				loc.Description = value
			}
			if err := mutator.UpdateLocation(ctx, subjectID, loc); err != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(sanitizeErrorForPlugin(pluginName, "location", entityIDStr, err)))
				return 2
			}

		case "object":
			adapter := NewWorldQuerierAdapter(f.worldService, pluginName)
			obj, err := adapter.GetObject(ctx, entityID)
			if err != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(sanitizeErrorForPlugin(pluginName, "object", entityIDStr, err)))
				return 2
			}
			switch property {
			case "name":
				obj.Name = value
			case "description":
				obj.Description = value
			}
			if err := mutator.UpdateObject(ctx, subjectID, obj); err != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(sanitizeErrorForPlugin(pluginName, "object", entityIDStr, err)))
				return 2
			}
		}

		L.Push(lua.LTrue)
		L.Push(lua.LNil)
		return 2
	}
}

// getPropertyFn returns a Lua function that gets a property from an entity.
// Lua signature: get_property(entity_type, entity_id, property) -> value or nil, error
// entity_type: "location" or "object"
// property: "name" or "description"
func (f *Functions) getPropertyFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			slog.Error("get_property called but world service unavailable",
				"plugin", pluginName,
				"hint", "use WithWorldService option when creating hostfunc.Functions")
			L.Push(lua.LNil)
			L.Push(lua.LString("world service not configured - contact server administrator"))
			return 2
		}

		entityType := L.CheckString(1)
		entityIDStr := L.CheckString(2)
		property := L.CheckString(3)

		// Validate entity type
		if entityType != "location" && entityType != "object" {
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid entity type: " + entityType + " (must be 'location' or 'object')"))
			return 2
		}

		// Validate entity ID
		entityID, err := ulid.Parse(entityIDStr)
		if err != nil {
			slog.Debug("get_property: invalid entity_id format",
				"plugin", pluginName,
				"entity_id", entityIDStr,
				"error", err)
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid entity_id: " + err.Error()))
			return 2
		}

		// Validate property using PropertyRegistry
		if !propertyRegistry.ValidFor(entityType, property) {
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid property: " + property + " for " + entityType))
			return 2
		}

		// Inherit context from Lua state if available, otherwise use Background
		parentCtx := L.Context()
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(parentCtx, defaultPluginQueryTimeout)
		defer cancel()

		adapter := NewWorldQuerierAdapter(f.worldService, pluginName)

		// Get the property value
		var value string
		switch entityType {
		case "location":
			loc, err := adapter.GetLocation(ctx, entityID)
			if err != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(sanitizeErrorForPlugin(pluginName, "location", entityIDStr, err)))
				return 2
			}
			switch property {
			case "name":
				value = loc.Name
			case "description":
				value = loc.Description
			}

		case "object":
			obj, err := adapter.GetObject(ctx, entityID)
			if err != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(sanitizeErrorForPlugin(pluginName, "object", entityIDStr, err)))
				return 2
			}
			switch property {
			case "name":
				value = obj.Name
			case "description":
				value = obj.Description
			}
		}

		L.Push(lua.LString(value))
		L.Push(lua.LNil)
		return 2
	}
}
