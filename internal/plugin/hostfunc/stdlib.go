// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"log/slog"

	"github.com/samber/oops"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/pkg/holo"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// RegisterStdlib registers the holo.* standard library functions in the Lua state.
// This provides access to holo.fmt.*, holo.emit.*, and holo.comm.* namespaces.
func RegisterStdlib(ls *lua.LState) {
	// Create the root holo table
	holoTable := ls.NewTable()

	// Register holo.fmt namespace
	registerFmt(ls, holoTable)

	// Register holo.emit namespace
	registerEmit(ls, holoTable)

	// Register holo.comm namespace (say/pose/ooc/emit content builders)
	registerComm(ls, holoTable)

	ls.SetGlobal("holo", holoTable)
}

// registerFmt sets up the holo.fmt.* functions.
func registerFmt(ls *lua.LState, holoTable *lua.LTable) {
	fmtMod := ls.NewTable()

	ls.SetField(fmtMod, "bold", ls.NewFunction(fmtBold))
	ls.SetField(fmtMod, "italic", ls.NewFunction(fmtItalic))
	ls.SetField(fmtMod, "dim", ls.NewFunction(fmtDim))
	ls.SetField(fmtMod, "underline", ls.NewFunction(fmtUnderline))
	ls.SetField(fmtMod, "color", ls.NewFunction(fmtColor))
	ls.SetField(fmtMod, "list", ls.NewFunction(fmtList))
	ls.SetField(fmtMod, "pairs", ls.NewFunction(fmtPairs))
	ls.SetField(fmtMod, "table", ls.NewFunction(fmtTableFn))
	ls.SetField(fmtMod, "separator", ls.NewFunction(fmtSeparator))
	ls.SetField(fmtMod, "header", ls.NewFunction(fmtHeader))
	ls.SetField(fmtMod, "parse", ls.NewFunction(fmtParse))

	ls.SetField(holoTable, "fmt", fmtMod)
}

// fmtBold wraps holo.Fmt.Bold.
func fmtBold(ls *lua.LState) int {
	text := ls.CheckString(1)
	result := holo.Fmt.Bold(text).RenderANSI()
	ls.Push(lua.LString(result))
	return 1
}

// fmtItalic wraps holo.Fmt.Italic.
func fmtItalic(ls *lua.LState) int {
	text := ls.CheckString(1)
	result := holo.Fmt.Italic(text).RenderANSI()
	ls.Push(lua.LString(result))
	return 1
}

// fmtDim wraps holo.Fmt.Dim.
func fmtDim(ls *lua.LState) int {
	text := ls.CheckString(1)
	result := holo.Fmt.Dim(text).RenderANSI()
	ls.Push(lua.LString(result))
	return 1
}

// fmtUnderline wraps holo.Fmt.Underline.
func fmtUnderline(ls *lua.LState) int {
	text := ls.CheckString(1)
	result := holo.Fmt.Underline(text).RenderANSI()
	ls.Push(lua.LString(result))
	return 1
}

// fmtColor wraps holo.Fmt.Color.
func fmtColor(ls *lua.LState) int {
	color := ls.CheckString(1)
	text := ls.CheckString(2)
	result := holo.Fmt.Color(color, text).RenderANSI()
	ls.Push(lua.LString(result))
	return 1
}

// fmtList wraps holo.Fmt.List.
// Expects a Lua table array as input.
func fmtList(ls *lua.LState) int {
	tbl := ls.CheckTable(1)

	// Convert Lua table array to Go slice
	items := luaTableToStringSlice(tbl)

	result := holo.Fmt.List(items).RenderANSI()
	ls.Push(lua.LString(result))
	return 1
}

// fmtPairs wraps holo.Fmt.Pairs.
// Expects a Lua table (key-value pairs) as input.
func fmtPairs(ls *lua.LState) int {
	tbl := ls.CheckTable(1)

	// Convert Lua table to Go map
	pairs := luaTableToMap(tbl)

	result := holo.Fmt.Pairs(pairs).RenderANSI()
	ls.Push(lua.LString(result))
	return 1
}

// fmtTableFn wraps holo.Fmt.Table.
// Expects a Lua table with optional "headers" and "rows" fields.
func fmtTableFn(ls *lua.LState) int {
	tbl := ls.CheckTable(1)

	opts := holo.TableOpts{}

	// Extract headers - type already checked by the if condition
	if headersVal := tbl.RawGetString("headers"); headersVal.Type() == lua.LTTable {
		headersTbl, ok := headersVal.(*lua.LTable)
		if ok {
			opts.Headers = luaTableToStringSlice(headersTbl)
		}
	}

	// Extract rows - type already checked by the if condition
	if rowsVal := tbl.RawGetString("rows"); rowsVal.Type() == lua.LTTable {
		rowsTbl, ok := rowsVal.(*lua.LTable)
		if ok {
			rowsTbl.ForEach(func(_, v lua.LValue) {
				if rowTbl, rowOK := v.(*lua.LTable); rowOK {
					opts.Rows = append(opts.Rows, luaTableToStringSlice(rowTbl))
				}
			})
		}
	}

	result := holo.Fmt.Table(opts).RenderANSI()
	ls.Push(lua.LString(result))
	return 1
}

// fmtSeparator wraps holo.Fmt.Separator.
func fmtSeparator(ls *lua.LState) int {
	result := holo.Fmt.Separator().RenderANSI()
	ls.Push(lua.LString(result))
	return 1
}

// fmtHeader wraps holo.Fmt.Header.
func fmtHeader(ls *lua.LState) int {
	text := ls.CheckString(1)
	result := holo.Fmt.Header(text).RenderANSI()
	ls.Push(lua.LString(result))
	return 1
}

// fmtParse wraps holo.Fmt.Parse.
func fmtParse(ls *lua.LState) int {
	text := ls.CheckString(1)
	result := holo.Fmt.Parse(text).RenderANSI()
	ls.Push(lua.LString(result))
	return 1
}

// luaTableToStringSlice converts a Lua array table to a Go string slice.
func luaTableToStringSlice(tbl *lua.LTable) []string {
	var result []string
	tbl.ForEach(func(k, v lua.LValue) {
		// Only process integer keys (array elements)
		if _, ok := k.(lua.LNumber); ok {
			result = append(result, v.String())
		}
	})
	return result
}

// luaTableToMap converts a Lua table to a Go map[string]any.
func luaTableToMap(tbl *lua.LTable) map[string]any {
	result := make(map[string]any)
	tbl.ForEach(func(k, v lua.LValue) {
		key := k.String()
		result[key] = luaValueToGo(v)
	})
	return result
}

// luaValueToGo converts a Lua value to a Go value.
func luaValueToGo(v lua.LValue) any {
	switch val := v.(type) {
	case lua.LString:
		return string(val)
	case lua.LNumber:
		return float64(val)
	case lua.LBool:
		return bool(val)
	case *lua.LTable:
		// Check if it's an array or map
		if isArray(val) {
			return luaTableToSlice(val)
		}
		return luaTableToMap(val)
	case *lua.LNilType:
		return nil
	default:
		return v.String()
	}
}

// isArray checks if a Lua table is an array (sequential integer keys starting from 1).
func isArray(tbl *lua.LTable) bool {
	maxN := tbl.MaxN()
	if maxN == 0 {
		// Empty or purely map-like table
		count := 0
		tbl.ForEach(func(_, _ lua.LValue) {
			count++
		})
		return count == 0
	}
	return true
}

// luaTableToSlice converts a Lua array table to a Go []any slice.
func luaTableToSlice(tbl *lua.LTable) []any {
	var result []any
	tbl.ForEach(func(k, v lua.LValue) {
		if _, ok := k.(lua.LNumber); ok {
			result = append(result, luaValueToGo(v))
		}
	})
	return result
}

// emitterRegistryKey is used to store the emitter in Lua's registry table.
const emitterRegistryKey = "__holo_emitter"

// registerEmit sets up the holo.emit.* functions.
// Uses module-level accumulator pattern cleared per event delivery.
func registerEmit(ls *lua.LState, holoTable *lua.LTable) {
	emitMod := ls.NewTable()

	// Store a fresh emitter in the registry for this state using SetGlobal technique
	emitter := holo.NewEmitter()
	ud := ls.NewUserData()
	ud.Value = emitter
	ls.SetGlobal(emitterRegistryKey, ud)

	ls.SetField(emitMod, "location", ls.NewFunction(emitLocation))
	ls.SetField(emitMod, "character", ls.NewFunction(emitCharacter))
	ls.SetField(emitMod, "global", ls.NewFunction(emitGlobal))
	ls.SetField(emitMod, "flush", ls.NewFunction(emitFlush))

	ls.SetField(holoTable, "emit", emitMod)
}

// getEmitter retrieves the per-state emitter from the Lua state.
// Returns nil if emitter not found, indicating RegisterStdlib was not called.
func getEmitter(ls *lua.LState) *holo.Emitter {
	ud := ls.GetGlobal(emitterRegistryKey)
	if ud.Type() == lua.LTUserData {
		userData, ok := ud.(*lua.LUserData)
		if ok {
			if emitter, emitOK := userData.Value.(*holo.Emitter); emitOK {
				return emitter
			}
		}
	}
	// Emitter not found - this indicates RegisterStdlib was not called.
	// Return nil so callers can raise an appropriate Lua error.
	slog.Error("emitter not found in Lua state registry",
		"registry_key", emitterRegistryKey,
		"hint", "RegisterStdlib must be called before emit functions")
	return nil
}

// emitLocation wraps holo.Emitter.LocationSensitive.
// Lua signature: holo.emit.location(locationID, eventType, payload [, opts])
// where opts is an optional table with { sensitive = bool }.
func emitLocation(ls *lua.LState) int {
	locationID := ls.CheckString(1)
	eventType := ls.CheckString(2)
	payload := ls.CheckTable(3)

	sensitive, err := readSensitiveOpts(ls, 4)
	if err != nil {
		ls.RaiseError("%s", formatSensitiveOptsErr(err))
		return 0
	}

	emitter := getEmitter(ls)
	if emitter == nil {
		ls.RaiseError("holo.emit: emitter not initialized (RegisterStdlib not called)")
		return 0
	}
	emitter.LocationSensitive(locationID, pluginsdk.EventType(eventType), luaTableToPayload(payload), sensitive)

	return 0
}

// emitCharacter wraps holo.Emitter.CharacterSensitive.
// Lua signature: holo.emit.character(characterID, eventType, payload [, opts])
func emitCharacter(ls *lua.LState) int {
	characterID := ls.CheckString(1)
	eventType := ls.CheckString(2)
	payload := ls.CheckTable(3)

	sensitive, err := readSensitiveOpts(ls, 4)
	if err != nil {
		ls.RaiseError("%s", formatSensitiveOptsErr(err))
		return 0
	}

	emitter := getEmitter(ls)
	if emitter == nil {
		ls.RaiseError("holo.emit: emitter not initialized (RegisterStdlib not called)")
		return 0
	}
	emitter.CharacterSensitive(characterID, pluginsdk.EventType(eventType), luaTableToPayload(payload), sensitive)

	return 0
}

// emitGlobal wraps holo.Emitter.GlobalSensitive.
// Lua signature: holo.emit.global(eventType, payload [, opts])
func emitGlobal(ls *lua.LState) int {
	eventType := ls.CheckString(1)
	payload := ls.CheckTable(2)

	sensitive, err := readSensitiveOpts(ls, 3)
	if err != nil {
		ls.RaiseError("%s", formatSensitiveOptsErr(err))
		return 0
	}

	emitter := getEmitter(ls)
	if emitter == nil {
		ls.RaiseError("holo.emit: emitter not initialized (RegisterStdlib not called)")
		return 0
	}
	emitter.GlobalSensitive(pluginsdk.EventType(eventType), luaTableToPayload(payload), sensitive)

	return 0
}

// formatSensitiveOptsErr formats a readSensitiveOpts error for surfacing
// through ls.RaiseError so the oops error code is visible in the Lua
// error string (oops embeds Code() in metadata, not in Error()).
func formatSensitiveOptsErr(err error) string {
	if oopsErr, ok := oops.AsOops(err); ok {
		if code, codeOK := oopsErr.Code().(string); codeOK && code != "" {
			return code + ": " + err.Error()
		}
	}
	return err.Error()
}

// readSensitiveOpts reads the `sensitive` boolean key from the optional
// opts table at the given Lua-stack position. Returns (false, nil) if
// the opts arg is absent (LNil or no-arg). Returns an error with code
// LUA_EMIT_SENSITIVE_TYPE if the key is present with a non-boolean value.
func readSensitiveOpts(ls *lua.LState, argIdx int) (bool, error) {
	optsVal := ls.Get(argIdx)
	if optsVal == lua.LNil {
		return false, nil
	}
	opts, ok := optsVal.(*lua.LTable)
	if !ok {
		return false, oops.Code("LUA_EMIT_SENSITIVE_TYPE").
			With("got_type", optsVal.Type().String()).
			Errorf("opts arg MUST be a table or nil")
	}
	sensitiveVal := opts.RawGetString("sensitive")
	if sensitiveVal == lua.LNil {
		return false, nil
	}
	sensitiveBool, ok := sensitiveVal.(lua.LBool)
	if !ok {
		return false, oops.Code("LUA_EMIT_SENSITIVE_TYPE").
			With("got_type", sensitiveVal.Type().String()).
			Errorf("opts.sensitive MUST be a boolean")
	}
	return bool(sensitiveBool), nil
}

// emitFlush returns all accumulated events and clears the buffer.
// Lua signature: events = holo.emit.flush()
// Returns a table of events or nil if no events were accumulated.
func emitFlush(ls *lua.LState) int {
	emitter := getEmitter(ls)
	if emitter == nil {
		ls.RaiseError("holo.emit: emitter not initialized (RegisterStdlib not called)")
		return 0
	}
	events, errs := emitter.Flush()
	for _, err := range errs {
		slog.Warn("emitter json error during flush", "error", err)
	}

	if len(events) == 0 {
		ls.Push(lua.LNil)
		return 1
	}

	// Convert events to Lua table. The `sensitive` field rides through
	// to internal/plugin/lua/host.go::parseEmitEvents on the canonical
	// `return holo.emit.flush()` path so a Lua plugin's per-event
	// sensitivity claim reaches Manager.EmitPluginEvent → EmitIntent.
	// Without this round-trip, Lua plugins emitting via the flush path
	// would silently degrade to Sensitive=false regardless of opts.
	result := ls.NewTable()
	for i, event := range events {
		eventTable := ls.NewTable()
		ls.SetField(eventTable, "stream", lua.LString(event.Stream))
		ls.SetField(eventTable, "type", lua.LString(string(event.Type)))
		ls.SetField(eventTable, "payload", lua.LString(event.Payload))
		ls.SetField(eventTable, "sensitive", lua.LBool(event.Sensitive))
		result.RawSetInt(i+1, eventTable)
	}

	ls.Push(result)
	return 1
}

// luaTableToPayload converts a Lua table to holo.Payload (map[string]any).
func luaTableToPayload(tbl *lua.LTable) holo.Payload {
	result := make(holo.Payload)
	tbl.ForEach(func(k, v lua.LValue) {
		key := k.String()
		result[key] = luaValueToGo(v)
	})
	return result
}
