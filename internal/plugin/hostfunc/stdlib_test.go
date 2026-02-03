// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

// Helper to create a Lua state with stdlib registered.
func newLuaStateWithStdlib(t *testing.T) *lua.LState {
	t.Helper()
	L := lua.NewState()
	hostfunc.RegisterStdlib(L)
	return L
}

// =============================================================================
// holo.fmt.bold()
// =============================================================================

func TestFmtBold(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.bold("important")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	// Should contain ANSI bold code
	resultStr := result.String()
	assert.Contains(t, resultStr, "\x1b[1m", "should contain bold ANSI code")
	assert.Contains(t, resultStr, "important")
	assert.Contains(t, resultStr, "\x1b[0m", "should contain reset ANSI code")
}

func TestFmtBold_EmptyString(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.bold("")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())
	// Empty input with bold still works but produces nothing visible
	assert.Equal(t, "\x1b[1m\x1b[0m", result.String())
}

// =============================================================================
// holo.fmt.italic()
// =============================================================================

func TestFmtItalic(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.italic("whispered")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	resultStr := result.String()
	assert.Contains(t, resultStr, "\x1b[3m", "should contain italic ANSI code")
	assert.Contains(t, resultStr, "whispered")
	assert.Contains(t, resultStr, "\x1b[0m", "should contain reset ANSI code")
}

// =============================================================================
// holo.fmt.dim()
// =============================================================================

func TestFmtDim(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.dim("(quietly)")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	resultStr := result.String()
	assert.Contains(t, resultStr, "\x1b[2m", "should contain dim ANSI code")
	assert.Contains(t, resultStr, "(quietly)")
	assert.Contains(t, resultStr, "\x1b[0m", "should contain reset ANSI code")
}

// =============================================================================
// holo.fmt.underline()
// =============================================================================

func TestFmtUnderline(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.underline("link")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	resultStr := result.String()
	assert.Contains(t, resultStr, "\x1b[4m", "should contain underline ANSI code")
	assert.Contains(t, resultStr, "link")
	assert.Contains(t, resultStr, "\x1b[0m", "should contain reset ANSI code")
}

// =============================================================================
// holo.fmt.color()
// =============================================================================

func TestFmtColor(t *testing.T) {
	tests := []struct {
		name     string
		color    string
		wantANSI string
	}{
		{"red", "red", "\x1b[31m"},
		{"green", "green", "\x1b[32m"},
		{"blue", "blue", "\x1b[34m"},
		{"cyan", "cyan", "\x1b[36m"},
		{"magenta", "magenta", "\x1b[35m"},
		{"yellow", "yellow", "\x1b[33m"},
		{"white", "white", "\x1b[37m"},
		{"black", "black", "\x1b[30m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			L := newLuaStateWithStdlib(t)
			defer L.Close()

			err := L.DoString(`result = holo.fmt.color("` + tt.color + `", "text")`)
			require.NoError(t, err)

			result := L.GetGlobal("result")
			require.Equal(t, lua.LTString, result.Type())

			resultStr := result.String()
			assert.Contains(t, resultStr, tt.wantANSI, "should contain color ANSI code")
			assert.Contains(t, resultStr, "text")
			assert.Contains(t, resultStr, "\x1b[0m", "should contain reset ANSI code")
		})
	}
}

func TestFmtColor_UnknownColor(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.color("purple", "text")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	// Unknown color returns plain text (no ANSI codes)
	assert.Equal(t, "text", result.String())
}

// =============================================================================
// holo.fmt.list()
// =============================================================================

func TestFmtList(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.list({"sword", "shield", "potion"})`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	resultStr := result.String()
	assert.Contains(t, resultStr, "  - sword")
	assert.Contains(t, resultStr, "  - shield")
	assert.Contains(t, resultStr, "  - potion")
}

func TestFmtList_Empty(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.list({})`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())
	assert.Equal(t, "", result.String())
}

func TestFmtList_SingleItem(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.list({"only one"})`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())
	assert.Equal(t, "  - only one", result.String())
}

// =============================================================================
// holo.fmt.pairs()
// =============================================================================

func TestFmtPairs(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	// Note: keys are sorted alphabetically
	err := L.DoString(`result = holo.fmt.pairs({HP = 100, MP = 50, Level = 5})`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	resultStr := result.String()
	assert.Contains(t, resultStr, "HP: 100")
	assert.Contains(t, resultStr, "Level: 5")
	assert.Contains(t, resultStr, "MP: 50")
}

func TestFmtPairs_Empty(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.pairs({})`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())
	assert.Equal(t, "", result.String())
}

// =============================================================================
// holo.fmt.table()
// =============================================================================

func TestFmtTable(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`
		result = holo.fmt.table({
			headers = {"Name", "Location", "Idle"},
			rows = {
				{"Alice", "Town Square", "2m"},
				{"Bob", "Forest", "5m"},
			}
		})
	`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	resultStr := result.String()
	assert.Contains(t, resultStr, "Name")
	assert.Contains(t, resultStr, "Location")
	assert.Contains(t, resultStr, "Idle")
	assert.Contains(t, resultStr, "Alice")
	assert.Contains(t, resultStr, "Town Square")
	assert.Contains(t, resultStr, "Bob")
	assert.Contains(t, resultStr, "Forest")
	// Should have separator line
	assert.Contains(t, resultStr, "---")
}

func TestFmtTable_Empty(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.table({headers = {}, rows = {}})`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())
	assert.Equal(t, "", result.String())
}

func TestFmtTable_NoHeaders(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`
		result = holo.fmt.table({
			rows = {
				{"Alice", "Town Square"},
				{"Bob", "Forest"},
			}
		})
	`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	resultStr := result.String()
	assert.Contains(t, resultStr, "Alice")
	assert.Contains(t, resultStr, "Bob")
	// No separator line without headers
	assert.NotContains(t, resultStr, "---")
}

// =============================================================================
// holo.fmt.separator()
// =============================================================================

func TestFmtSeparator(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.separator()`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	resultStr := result.String()
	assert.Len(t, resultStr, 40, "separator should be 40 characters")
	assert.Equal(t, "----------------------------------------", resultStr)
}

// =============================================================================
// holo.fmt.header()
// =============================================================================

func TestFmtHeader(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.header("Inventory")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	resultStr := result.String()
	// Header is bold
	assert.Contains(t, resultStr, "\x1b[1m", "should contain bold ANSI code")
	assert.Contains(t, resultStr, "Inventory")
	assert.Contains(t, resultStr, "\x1b[0m", "should contain reset ANSI code")
}

func TestFmtHeader_Empty(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.header("")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())
	assert.Equal(t, "", result.String())
}

// =============================================================================
// holo.fmt.parse()
// =============================================================================

func TestFmtParse(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.parse("%xhbold%xn and %xrred%xn text")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	resultStr := result.String()
	assert.Contains(t, resultStr, "\x1b[1m", "should contain bold ANSI code")
	assert.Contains(t, resultStr, "\x1b[0m", "should contain reset ANSI code")
	assert.Contains(t, resultStr, "\x1b[31m", "should contain red ANSI code")
	assert.Contains(t, resultStr, "bold")
	assert.Contains(t, resultStr, "red")
	assert.Contains(t, resultStr, "text")
}

func TestFmtParse_WhitespaceCodes(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.parse("line1%rline2%bword%ttab")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	resultStr := result.String()
	assert.Contains(t, resultStr, "line1\nline2", "should convert %r to newline")
	assert.Contains(t, resultStr, "line2 word", "should convert %b to space")
	assert.Contains(t, resultStr, "word    tab", "should convert %t to 4 spaces")
}

func TestFmtParse_256Color(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.parse("%x196red%xn")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	resultStr := result.String()
	assert.Contains(t, resultStr, "\x1b[38;5;196m", "should contain 256-color ANSI code")
}

func TestFmtParse_Empty(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.parse("")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())
	assert.Equal(t, "", result.String())
}

func TestFmtParse_UnknownCodes(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.parse("%xz unknown code")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	// Unknown codes are preserved
	resultStr := result.String()
	assert.Contains(t, resultStr, "%xz", "unknown codes should be preserved")
}

// =============================================================================
// holo.emit.location()
// =============================================================================

func TestEmitLocation(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`
		holo.emit.location("01ABC123XYZ456", "say", {message = "Hello", speaker = "Alice"})
		result = holo.emit.flush()
	`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type(), "flush should return a table")

	tbl := result.(*lua.LTable)
	assert.Equal(t, 1, tbl.Len(), "should have 1 event")

	event := tbl.RawGetInt(1).(*lua.LTable)
	assert.Equal(t, "location:01ABC123XYZ456", event.RawGetString("stream").String())
	assert.Equal(t, "say", event.RawGetString("type").String())
	// Payload should be JSON
	payload := event.RawGetString("payload").String()
	assert.Contains(t, payload, `"message"`)
	assert.Contains(t, payload, `"Hello"`)
	assert.Contains(t, payload, `"speaker"`)
	assert.Contains(t, payload, `"Alice"`)
}

func TestEmitLocation_EmptyPayload(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`
		holo.emit.location("01ABC123XYZ456", "system", {})
		result = holo.emit.flush()
	`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())

	tbl := result.(*lua.LTable)
	assert.Equal(t, 1, tbl.Len())

	event := tbl.RawGetInt(1).(*lua.LTable)
	assert.Equal(t, "{}", event.RawGetString("payload").String())
}

// =============================================================================
// holo.emit.character()
// =============================================================================

func TestEmitCharacter(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`
		holo.emit.character("01DEF456ABC789", "tell", {message = "Secret", sender = "Bob"})
		result = holo.emit.flush()
	`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())

	tbl := result.(*lua.LTable)
	assert.Equal(t, 1, tbl.Len())

	event := tbl.RawGetInt(1).(*lua.LTable)
	assert.Equal(t, "char:01DEF456ABC789", event.RawGetString("stream").String())
	assert.Equal(t, "tell", event.RawGetString("type").String())
}

// =============================================================================
// holo.emit.global()
// =============================================================================

func TestEmitGlobal(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`
		holo.emit.global("announcement", {message = "Server restart in 5 minutes"})
		result = holo.emit.flush()
	`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())

	tbl := result.(*lua.LTable)
	assert.Equal(t, 1, tbl.Len())

	event := tbl.RawGetInt(1).(*lua.LTable)
	assert.Equal(t, "global", event.RawGetString("stream").String())
	assert.Equal(t, "announcement", event.RawGetString("type").String())
}

// =============================================================================
// holo.emit.flush()
// =============================================================================

func TestEmitFlush_MultipleEvents(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`
		holo.emit.location("01ABC", "say", {message = "First"})
		holo.emit.character("01DEF", "tell", {message = "Second"})
		holo.emit.global("system", {message = "Third"})
		result = holo.emit.flush()
	`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())

	tbl := result.(*lua.LTable)
	assert.Equal(t, 3, tbl.Len(), "should have 3 events")

	// Verify order is preserved
	event1 := tbl.RawGetInt(1).(*lua.LTable)
	assert.Equal(t, "location:01ABC", event1.RawGetString("stream").String())

	event2 := tbl.RawGetInt(2).(*lua.LTable)
	assert.Equal(t, "char:01DEF", event2.RawGetString("stream").String())

	event3 := tbl.RawGetInt(3).(*lua.LTable)
	assert.Equal(t, "global", event3.RawGetString("stream").String())
}

func TestEmitFlush_ClearsBuffer(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`
		holo.emit.location("01ABC", "say", {message = "Hello"})
		first_result = holo.emit.flush()
		second_result = holo.emit.flush()
	`)
	require.NoError(t, err)

	firstResult := L.GetGlobal("first_result")
	require.Equal(t, lua.LTTable, firstResult.Type())
	assert.Equal(t, 1, firstResult.(*lua.LTable).Len())

	secondResult := L.GetGlobal("second_result")
	// Second flush returns nil (no events)
	assert.Equal(t, lua.LTNil, secondResult.Type())
}

func TestEmitFlush_Empty(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.emit.flush()`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	assert.Equal(t, lua.LTNil, result.Type(), "empty flush should return nil")
}

// =============================================================================
// Nested payload conversion
// =============================================================================

func TestEmitLocation_NestedPayload(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`
		holo.emit.location("01ABC", "complex", {
			player = {name = "Alice", level = 10},
			items = {"sword", "shield"}
		})
		result = holo.emit.flush()
	`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())

	tbl := result.(*lua.LTable)
	event := tbl.RawGetInt(1).(*lua.LTable)
	payload := event.RawGetString("payload").String()

	// Payload should contain nested structures as JSON
	assert.Contains(t, payload, `"player"`)
	assert.Contains(t, payload, `"name"`)
	assert.Contains(t, payload, `"Alice"`)
	assert.Contains(t, payload, `"level"`)
	assert.Contains(t, payload, `"items"`)
	assert.Contains(t, payload, `"sword"`)
	assert.Contains(t, payload, `"shield"`)
}

func TestEmitLocation_NumericValues(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`
		holo.emit.location("01ABC", "stats", {hp = 100, mp = 50.5, alive = true})
		result = holo.emit.flush()
	`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())

	tbl := result.(*lua.LTable)
	event := tbl.RawGetInt(1).(*lua.LTable)
	payload := event.RawGetString("payload").String()

	assert.Contains(t, payload, `"hp":100`)
	assert.Contains(t, payload, `"mp":50.5`)
	assert.Contains(t, payload, `"alive":true`)
}

func TestEmitLocation_NilValue(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`
		holo.emit.location("01ABC", "test", {value = nil})
		result = holo.emit.flush()
	`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())

	tbl := result.(*lua.LTable)
	event := tbl.RawGetInt(1).(*lua.LTable)
	payload := event.RawGetString("payload").String()

	// Nil values are typically omitted in JSON or represented as null
	// The empty table {} has no value key
	assert.Equal(t, "{}", payload)
}

// =============================================================================
// Edge cases and error paths
// =============================================================================

func TestFmtTable_RowsWithDifferentLengths(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`
		result = holo.fmt.table({
			headers = {"A", "B", "C"},
			rows = {
				{"1", "2"},
				{"a", "b", "c", "d"},
			}
		})
	`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	// Should not panic with mismatched column counts
	resultStr := result.String()
	assert.Contains(t, resultStr, "A")
	assert.Contains(t, resultStr, "1")
}

func TestFmtPairs_MixedTypes(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.pairs({num = 42, str = "hello", flag = true})`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	resultStr := result.String()
	assert.Contains(t, resultStr, "num: 42")
	assert.Contains(t, resultStr, "str: hello")
	assert.Contains(t, resultStr, "flag: true")
}

func TestFmtList_WithNumbers(t *testing.T) {
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	err := L.DoString(`result = holo.fmt.list({1, 2, 3})`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type())

	resultStr := result.String()
	assert.Contains(t, resultStr, "  - 1")
	assert.Contains(t, resultStr, "  - 2")
	assert.Contains(t, resultStr, "  - 3")
}

// =============================================================================
// Integration test: Simulated plugin using stdlib
// =============================================================================

func TestIntegration_PluginUsesStdlib(t *testing.T) {
	// This test simulates a realistic plugin that uses both fmt and emit functions
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	// Define a plugin-like function that formats output and emits events
	pluginCode := `
		function handle_command(location_id, speaker_name, message)
			-- Format the message with styling
			local styled_msg = holo.fmt.parse("%xh" .. speaker_name .. "%xn says, \"" .. message .. "\"")

			-- Emit to the location
			holo.emit.location(location_id, "say", {
				message = styled_msg,
				speaker = speaker_name
			})

			-- Return the events
			return holo.emit.flush()
		end

		-- Call the handler
		result = handle_command("01ABC123XYZ456", "Alice", "Hello everyone!")
	`

	err := L.DoString(pluginCode)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())

	tbl := result.(*lua.LTable)
	assert.Equal(t, 1, tbl.Len())

	event := tbl.RawGetInt(1).(*lua.LTable)
	assert.Equal(t, "location:01ABC123XYZ456", event.RawGetString("stream").String())
	assert.Equal(t, "say", event.RawGetString("type").String())

	payload := event.RawGetString("payload").String()
	assert.Contains(t, payload, "Alice")
	assert.Contains(t, payload, "Hello everyone!")
	// The message should contain ANSI codes from parse (JSON-escaped as \u001b)
	assert.Contains(t, payload, `\u001b[1m`, "should contain bold ANSI from %xh (JSON-escaped)")
}

func TestIntegration_WhoCommand(t *testing.T) {
	// Simulates a 'who' command that formats a table and emits to the character
	L := newLuaStateWithStdlib(t)
	defer L.Close()

	pluginCode := `
		function handle_who(character_id)
			-- Build a table of online players
			local header = holo.fmt.header("Online Players")
			local sep = holo.fmt.separator()
			local players = holo.fmt.table({
				headers = {"Name", "Location", "Idle"},
				rows = {
					{"Alice", "Town Square", "2m"},
					{"Bob", "Forest", "5m"},
					{"Charlie", "Castle", "10s"},
				}
			})

			-- Combine into full output
			local output = header .. "\n" .. sep .. "\n" .. players

			-- Send to the requesting character
			holo.emit.character(character_id, "system", {
				message = output
			})

			return holo.emit.flush()
		end

		result = handle_who("01CHAR123ABC456")
	`

	err := L.DoString(pluginCode)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())

	tbl := result.(*lua.LTable)
	assert.Equal(t, 1, tbl.Len())

	event := tbl.RawGetInt(1).(*lua.LTable)
	assert.Equal(t, "char:01CHAR123ABC456", event.RawGetString("stream").String())
	assert.Equal(t, "system", event.RawGetString("type").String())

	payload := event.RawGetString("payload").String()
	assert.Contains(t, payload, "Online Players")
	assert.Contains(t, payload, "Alice")
	assert.Contains(t, payload, "Town Square")
}
