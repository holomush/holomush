// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"errors"
	"log/slog"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
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
			return f.pushServiceUnavailable(L, "create_location", pluginName)
		}

		name := L.CheckString(1)
		description := L.CheckString(2)
		locTypeStr := L.CheckString(3)

		locType := world.LocationType(locTypeStr)
		if err := locType.Validate(); err != nil {
			return pushError(L, "invalid location type: "+locTypeStr)
		}

		loc := &world.Location{
			ID:          ulid.Make(),
			Name:        name,
			Description: description,
			Type:        locType,
		}

		return f.withMutatorContext(L, "create_location", pluginName,
			func(ctx context.Context, mutator WorldMutator, subjectID string, _ *WorldQuerierAdapter) int {
				if err := mutator.CreateLocation(ctx, subjectID, loc); err != nil {
					return pushError(L, sanitizeErrorForPlugin(pluginName, "location", name, err))
				}

				result := L.NewTable()
				L.SetField(result, "id", lua.LString(loc.ID.String()))
				L.SetField(result, "name", lua.LString(loc.Name))
				return pushSuccess(L, result)
			})
	}
}

// createExitFn returns a Lua function that creates a new exit.
// Lua signature: create_exit(from_id, to_id, name, opts) -> {id, name} or nil, error
// opts: { bidirectional = true, return_name = "south" }
func (f *Functions) createExitFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			return f.pushServiceUnavailable(L, "create_exit", pluginName)
		}

		fromIDStr := L.CheckString(1)
		toIDStr := L.CheckString(2)
		name := L.CheckString(3)

		fromID, ok := parseULID(L, fromIDStr, pluginName, "create_exit", "from_id")
		if !ok {
			return 2
		}

		toID, ok := parseULID(L, toIDStr, pluginName, "create_exit", "to_id")
		if !ok {
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

		return f.withMutatorContext(L, "create_exit", pluginName,
			func(ctx context.Context, mutator WorldMutator, subjectID string, _ *WorldQuerierAdapter) int {
				if err := mutator.CreateExit(ctx, subjectID, exit); err != nil {
					return pushError(L, sanitizeErrorForPlugin(pluginName, "exit", name, err))
				}

				result := L.NewTable()
				L.SetField(result, "id", lua.LString(exit.ID.String()))
				L.SetField(result, "name", lua.LString(exit.Name))
				return pushSuccess(L, result)
			})
	}
}

// createObjectFn returns a Lua function that creates a new object.
// Lua signature: create_object(name, opts) -> {id, name} or nil, error
// opts: { location_id = "...", character_id = "...", container_id = "...", description = "..." }
// Exactly one containment field must be specified.
func (f *Functions) createObjectFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			return f.pushServiceUnavailable(L, "create_object", pluginName)
		}

		name := L.CheckString(1)

		// Validate opts parameter is provided and is a table
		opts, ok := L.Get(2).(*lua.LTable)
		if !ok {
			return pushError(L, "second argument must be an options table")
		}

		// Parse containment from options
		var containment world.Containment
		containmentCount := 0

		if locIDVal := opts.RawGetString("location_id"); locIDVal.Type() == lua.LTString {
			locID, err := ulid.Parse(locIDVal.String())
			if err != nil {
				return pushError(L, "invalid location_id: "+err.Error())
			}
			containment = world.InLocation(locID)
			containmentCount++
		}

		if charIDVal := opts.RawGetString("character_id"); charIDVal.Type() == lua.LTString {
			charID, err := ulid.Parse(charIDVal.String())
			if err != nil {
				return pushError(L, "invalid character_id: "+err.Error())
			}
			containment = world.HeldByCharacter(charID)
			containmentCount++
		}

		if containerIDVal := opts.RawGetString("container_id"); containerIDVal.Type() == lua.LTString {
			containerID, err := ulid.Parse(containerIDVal.String())
			if err != nil {
				return pushError(L, "invalid container_id: "+err.Error())
			}
			containment = world.ContainedInObject(containerID)
			containmentCount++
		}

		if containmentCount != 1 {
			return pushError(L, "must specify exactly one containment: location_id, character_id, or container_id")
		}

		obj, err := world.NewObjectWithID(ulid.Make(), name, containment)
		if err != nil {
			return pushError(L, "invalid object: "+err.Error())
		}

		// Optional description
		if descVal := opts.RawGetString("description"); descVal.Type() == lua.LTString {
			obj.Description = descVal.String()
		}

		return f.withMutatorContext(L, "create_object", pluginName,
			func(ctx context.Context, mutator WorldMutator, subjectID string, _ *WorldQuerierAdapter) int {
				if err := mutator.CreateObject(ctx, subjectID, obj); err != nil {
					return pushError(L, sanitizeErrorForPlugin(pluginName, "object", name, err))
				}

				result := L.NewTable()
				L.SetField(result, "id", lua.LString(obj.ID.String()))
				L.SetField(result, "name", lua.LString(obj.Name))
				return pushSuccess(L, result)
			})
	}
}

// findLocationFn returns a Lua function that finds a location by name.
// Lua signature: find_location(name) -> {id, name} or nil, error
func (f *Functions) findLocationFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			return f.pushServiceUnavailable(L, "find_location", pluginName)
		}

		// Check for mutator support (find_location needs FindLocationByName method)
		mutator, ok := f.worldService.(WorldMutator)
		if !ok {
			slog.Warn("find_location called but world service does not support FindLocationByName",
				"plugin", pluginName)
			return pushError(L, "world service does not support location search")
		}

		name := L.CheckString(1)

		return f.withQueryContext(L, pluginName, func(ctx context.Context, _ *WorldQuerierAdapter) int {
			subjectID := "system:plugin:" + pluginName
			loc, err := mutator.FindLocationByName(ctx, subjectID, name)
			if err != nil {
				if errors.Is(err, world.ErrNotFound) {
					slog.Debug("find_location: location not found",
						"plugin", pluginName,
						"name", name)
				}
				return pushError(L, sanitizeErrorForPlugin(pluginName, "location", name, err))
			}

			result := L.NewTable()
			L.SetField(result, "id", lua.LString(loc.ID.String()))
			L.SetField(result, "name", lua.LString(loc.Name))
			return pushSuccess(L, result)
		})
	}
}

// propertyOpts holds validated parameters for property operations.
type propertyOpts struct {
	entityType  string
	entityID    ulid.ULID
	entityIDStr string
	property    string
}

// validatePropertyArgs validates and parses common property function arguments.
// Returns propertyOpts and true on success, or pushes error and returns false.
//
//nolint:gocritic // captLocal: L is the idiomatic name for lua.LState in gopher-lua
func validatePropertyArgs(L *lua.LState, pluginName, fnName string, hasValue bool) (*propertyOpts, bool) {
	entityType := L.CheckString(1)
	entityIDStr := L.CheckString(2)
	property := L.CheckString(3)
	if hasValue {
		L.CheckString(4) // validate value is present
	}

	// Validate entity type
	if entityType != "location" && entityType != "object" {
		pushError(L, "invalid entity type: "+entityType+" (must be 'location' or 'object')")
		return nil, false
	}

	// Validate entity ID
	entityID, ok := parseULID(L, entityIDStr, pluginName, fnName, "entity_id")
	if !ok {
		return nil, false
	}

	// Validate property using PropertyRegistry
	if !propertyRegistry.ValidFor(entityType, property) {
		pushError(L, "invalid property: "+property+" for "+entityType)
		return nil, false
	}

	return &propertyOpts{
		entityType:  entityType,
		entityID:    entityID,
		entityIDStr: entityIDStr,
		property:    property,
	}, true
}

// getEntityProperty retrieves a property value from an entity.
func getEntityProperty(ctx context.Context, adapter *WorldQuerierAdapter, opts *propertyOpts) (string, error) {
	switch opts.entityType {
	case "location":
		loc, err := adapter.GetLocation(ctx, opts.entityID)
		if err != nil {
			return "", err
		}
		switch opts.property {
		case "name":
			return loc.Name, nil
		case "description":
			return loc.Description, nil
		}
	case "object":
		obj, err := adapter.GetObject(ctx, opts.entityID)
		if err != nil {
			return "", err
		}
		switch opts.property {
		case "name":
			return obj.Name, nil
		case "description":
			return obj.Description, nil
		}
	}
	return "", nil
}

// setEntityProperty sets a property value on an entity.
func setEntityProperty(ctx context.Context, adapter *WorldQuerierAdapter, mutator WorldMutator, subjectID string, opts *propertyOpts, value string) error {
	switch opts.entityType {
	case "location":
		loc, err := adapter.GetLocation(ctx, opts.entityID)
		if err != nil {
			return err
		}
		switch opts.property {
		case "name":
			loc.Name = value
		case "description":
			loc.Description = value
		}
		if err := mutator.UpdateLocation(ctx, subjectID, loc); err != nil {
			return oops.Wrapf(err, "update location %s", opts.entityID)
		}
		return nil
	case "object":
		obj, err := adapter.GetObject(ctx, opts.entityID)
		if err != nil {
			return err
		}
		switch opts.property {
		case "name":
			obj.Name = value
		case "description":
			obj.Description = value
		}
		if err := mutator.UpdateObject(ctx, subjectID, obj); err != nil {
			return oops.Wrapf(err, "update object %s", opts.entityID)
		}
		return nil
	}
	return nil
}

// setPropertyFn returns a Lua function that sets a property on an entity.
// Lua signature: set_property(entity_type, entity_id, property, value) -> true or nil, error
// entity_type: "location" or "object"
// property: "name" or "description"
func (f *Functions) setPropertyFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			return f.pushServiceUnavailable(L, "set_property", pluginName)
		}

		opts, ok := validatePropertyArgs(L, pluginName, "set_property", true)
		if !ok {
			return 2
		}
		value := L.CheckString(4)

		return f.withMutatorContext(L, "set_property", pluginName,
			func(ctx context.Context, mutator WorldMutator, subjectID string, adapter *WorldQuerierAdapter) int {
				if err := setEntityProperty(ctx, adapter, mutator, subjectID, opts, value); err != nil {
					return pushError(L, sanitizeErrorForPlugin(pluginName, opts.entityType, opts.entityIDStr, err))
				}
				return pushSuccess(L, lua.LTrue)
			})
	}
}

// getPropertyFn returns a Lua function that gets a property from an entity.
// Lua signature: get_property(entity_type, entity_id, property) -> value or nil, error
// entity_type: "location" or "object"
// property: "name" or "description"
func (f *Functions) getPropertyFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			return f.pushServiceUnavailable(L, "get_property", pluginName)
		}

		opts, ok := validatePropertyArgs(L, pluginName, "get_property", false)
		if !ok {
			return 2
		}

		return f.withQueryContext(L, pluginName, func(ctx context.Context, adapter *WorldQuerierAdapter) int {
			value, err := getEntityProperty(ctx, adapter, opts)
			if err != nil {
				return pushError(L, sanitizeErrorForPlugin(pluginName, opts.entityType, opts.entityIDStr, err))
			}
			return pushSuccess(L, lua.LString(value))
		})
	}
}
