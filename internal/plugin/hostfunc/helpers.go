// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package hostfunc note: L is the idiomatic variable name for lua.LState
// in the gopher-lua community, matching the reference implementation.
//nolint:gocritic // captLocal: L is the idiomatic name for lua.LState
package hostfunc

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/oklog/ulid/v2"
	lua "github.com/yuin/gopher-lua"
)

// pushError pushes nil followed by an error string to the Lua stack and returns 2.
// This is the standard pattern for returning errors from host functions.
func pushError(L *lua.LState, errMsg string) int {
	L.Push(lua.LNil)
	L.Push(lua.LString(errMsg))
	return 2
}

// pushSuccess pushes a value followed by nil (no error) to the Lua stack and returns 2.
// This is the standard pattern for returning successful results from host functions.
func pushSuccess(L *lua.LState, value lua.LValue) int {
	L.Push(value)
	L.Push(lua.LNil)
	return 2
}

// pushServiceUnavailable logs and returns a standard error when the world service
// is not configured. This consolidates the nil check boilerplate.
func (f *Functions) pushServiceUnavailable(L *lua.LState, funcName, pluginName string) int {
	slog.Error(funcName+" called but world service unavailable",
		"plugin", pluginName,
		"hint", "use WithWorldService option when creating hostfunc.Functions")
	return pushError(L, "world service not configured - contact server administrator")
}

// pushMutatorUnavailable logs and returns a standard error when the world service
// does not support mutations.
func pushMutatorUnavailable(L *lua.LState, funcName, pluginName string) int {
	slog.Warn(funcName+" called but world service does not support mutations",
		"plugin", pluginName)
	return pushError(L, "world service does not support mutations")
}

// parseULID parses a ULID string from Lua arguments. On failure, it pushes an
// appropriate error to the Lua stack and returns a zero ULID and false.
// The paramName is used to construct the error message (e.g., "room ID", "character ID").
func parseULID(L *lua.LState, idStr, pluginName, funcName, paramName string) (ulid.ULID, bool) {
	id, err := ulid.Parse(idStr)
	if err != nil {
		slog.Debug(funcName+": invalid "+paramName+" format",
			"plugin", pluginName,
			paramName, idStr,
			"error", err)
		pushError(L, fmt.Sprintf("invalid %s: %s", paramName, err.Error()))
		return ulid.ULID{}, false
	}
	return id, true
}

// withQueryContext creates a context with timeout derived from the Lua state's context
// (or context.Background() if none set), creates a WorldQuerierAdapter, and calls
// the provided function with these resources.
//
// This consolidates the common boilerplate:
//   - Getting parent context from Lua state
//   - Falling back to context.Background() if nil
//   - Creating a timeout context
//   - Creating the adapter
//   - Ensuring cancel is called
func (f *Functions) withQueryContext(
	L *lua.LState,
	pluginName string,
	fn func(ctx context.Context, adapter *WorldQuerierAdapter) int,
) int {
	parentCtx := L.Context()
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, defaultPluginQueryTimeout)
	defer cancel()

	adapter := NewWorldQuerierAdapter(f.worldService, pluginName)
	return fn(ctx, adapter)
}

// withMutatorContext is similar to withQueryContext but also checks that the world
// service supports mutations (implements WorldMutator). If not, it pushes an error
// and returns early.
//
// The subjectID for ABAC is constructed as "system:plugin:<pluginName>".
func (f *Functions) withMutatorContext(
	L *lua.LState,
	funcName, pluginName string,
	fn func(ctx context.Context, mutator WorldMutator, subjectID string, adapter *WorldQuerierAdapter) int,
) int {
	mutator, ok := f.worldService.(WorldMutator)
	if !ok {
		return pushMutatorUnavailable(L, funcName, pluginName)
	}

	parentCtx := L.Context()
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, defaultPluginQueryTimeout)
	defer cancel()

	subjectID := "system:plugin:" + pluginName
	adapter := NewWorldQuerierAdapter(f.worldService, pluginName)
	return fn(ctx, mutator, subjectID, adapter)
}
