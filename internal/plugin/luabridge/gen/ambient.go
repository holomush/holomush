// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

// ambientParam is one parameter of an ambient stdlib function.
type ambientParam struct {
	Name string
	Type string // LuaLS type string
}

// ambientFn declares one ambient host function for the stub. Module is the dotted
// path of the table the function lives on (spec §4.2). The (Module, Name) set MUST
// equal the live registration set — enforced by TestAmbientDeclTableMatchesRegistrations.
type ambientFn struct {
	Module  string // "holomush" | "holomush.config" | "holo.fmt" | "holo.emit"
	Name    string
	Doc     string
	Params  []ambientParam
	Returns []string // LuaLS return type strings
}

// ambientDecls is the single source of truth for ambient-surface annotations.
// The NAME set is verified against runtime registration (§5.2); the param/return
// types are hand-authored (no machine source exists). Authoritative ambient
// boundary: internal/plugin/hostfunc/functions.go Register + RegisteredFunctionsForAudit.
// Signatures below are GROUNDED against the actual Lua arg-parsing in each
// implementation (see the source map in the plan's Step 1b). Do NOT alter a
// signature without re-reading the corresponding function — the parity test checks
// NAME equality only, so a wrong arity/order here ships green and misleads authors.
var ambientDecls = []ambientFn{
	// functions.go logFn:416-417 → (level, message).
	{
		Module: "holomush", Name: "log", Doc: "Write a structured log line.",
		Params: []ambientParam{{"level", "string"}, {"message", "string"}},
	},
	// functions.go newRequestIDFn:444-451 → () → string.
	{
		Module: "holomush", Name: "new_request_id", Doc: "Return a fresh request-scoped ULID string.",
		Returns: []string{"string"},
	},
	// commands.go listCommandsFn:55-59 → (character_id) [ignored for authz]; returns (table, err?).
	{
		Module: "holomush", Name: "list_commands", Doc: "List commands visible to the subject. character_id is accepted for call-signature compatibility but IGNORED for authorization.",
		Params: []ambientParam{{"character_id", "string"}}, Returns: []string{"table", "string?"},
	},
	// commands.go getCommandHelpFn:145-150 → (command_name, character_id) [character_id ignored]; returns (table, err?).
	{
		Module: "holomush", Name: "get_command_help", Doc: "Return help info for a command. character_id is accepted for compatibility but IGNORED.",
		Params: []ambientParam{{"command_name", "string"}, {"character_id", "string"}}, Returns: []string{"table", "string?"},
	},
	// stdlib_streams.go addSessionStreamFn:42-44 → (session_id, stream); true on success, (nil, err) on failure.
	{
		Module: "holomush", Name: "add_session_stream", Doc: "Subscribe a session to a stream.",
		Params: []ambientParam{{"session_id", "string"}, {"stream", "string"}}, Returns: []string{"boolean", "string?"},
	},
	// stdlib_streams.go removeSessionStreamFn:70-72 → (session_id, stream); true on success, (nil, err) on failure.
	{
		Module: "holomush", Name: "remove_session_stream", Doc: "Unsubscribe a session from a stream.",
		Params: []ambientParam{{"session_id", "string"}, {"stream", "string"}}, Returns: []string{"boolean", "string?"},
	},
	// stdlib_focus.go queryStreamHistoryFn:342-345 → single table arg {stream, count, not_before_ms?, cursor?}; returns (table, err?).
	{
		Module: "holomush", Name: "query_stream_history", Doc: "Read recent events from a stream. Pass a table: {stream=string, count=integer, not_before_ms?=integer, cursor?=string}.",
		Params: []ambientParam{{"args", "table"}}, Returns: []string{"table", "string?"},
	},
	// stdlib_audit.go decryptOwnAuditRowsImpl:60-70 → (rows: array of row tables); returns (table, err?).
	{
		Module: "holomush", Name: "decrypt_own_audit_rows", Doc: "Decrypt audit rows the plugin owns. rows is an array of row tables.",
		Params: []ambientParam{{"rows", "table"}}, Returns: []string{"table", "string?"},
	},
	// functions.go:326-330 → (event_type); returns true.
	{
		Module: "holomush", Name: "register_emit_type", Doc: "Declare a plugin-owned event type (Load-time; INV-PLUGIN-32).",
		Params: []ambientParam{{"event_type", "string"}}, Returns: []string{"boolean"},
	},

	// config.go: every accessor is (key); require_* error if absent. Non-require return optional.
	{Module: "holomush.config", Name: "string", Params: []ambientParam{{"key", "string"}}, Returns: []string{"string?"}, Doc: "Read a string config value."},
	{Module: "holomush.config", Name: "require_string", Params: []ambientParam{{"key", "string"}}, Returns: []string{"string"}, Doc: "Read a required string config value (errors if absent)."},
	{Module: "holomush.config", Name: "int", Params: []ambientParam{{"key", "string"}}, Returns: []string{"integer?"}, Doc: "Read an integer config value."},
	{Module: "holomush.config", Name: "require_int", Params: []ambientParam{{"key", "string"}}, Returns: []string{"integer"}, Doc: "Read a required integer config value."},
	{Module: "holomush.config", Name: "bool", Params: []ambientParam{{"key", "string"}}, Returns: []string{"boolean?"}, Doc: "Read a boolean config value."},
	{Module: "holomush.config", Name: "require_bool", Params: []ambientParam{{"key", "string"}}, Returns: []string{"boolean"}, Doc: "Read a required boolean config value."},
	// config.go duration:41 / require_duration:58 → returns d.Seconds() (a number in seconds), NOT nanoseconds.
	{Module: "holomush.config", Name: "duration", Params: []ambientParam{{"key", "string"}}, Returns: []string{"number?"}, Doc: "Read a duration config value (in seconds)."},
	{Module: "holomush.config", Name: "require_duration", Params: []ambientParam{{"key", "string"}}, Returns: []string{"number"}, Doc: "Read a required duration config value (in seconds)."},

	// stdlib.go fmt*: all (text) → string, EXCEPT color (color, text) and list/pairs/table (table arg), separator ().
	{Module: "holo.fmt", Name: "bold", Params: []ambientParam{{"text", "string"}}, Returns: []string{"string"}, Doc: "Wrap text in bold styling."},
	{Module: "holo.fmt", Name: "italic", Params: []ambientParam{{"text", "string"}}, Returns: []string{"string"}, Doc: "Wrap text in italic styling."},
	{Module: "holo.fmt", Name: "dim", Params: []ambientParam{{"text", "string"}}, Returns: []string{"string"}, Doc: "Wrap text in dim styling."},
	{Module: "holo.fmt", Name: "underline", Params: []ambientParam{{"text", "string"}}, Returns: []string{"string"}, Doc: "Wrap text in underline styling."},
	// stdlib.go fmtColor:84-85 → (color, text) — NOTE the order.
	{Module: "holo.fmt", Name: "color", Params: []ambientParam{{"color", "string"}, {"text", "string"}}, Returns: []string{"string"}, Doc: "Colorize text. Argument order is (color, text)."},
	{Module: "holo.fmt", Name: "list", Params: []ambientParam{{"items", "table"}}, Returns: []string{"string"}, Doc: "Format a list."},
	{Module: "holo.fmt", Name: "pairs", Params: []ambientParam{{"kv", "table"}}, Returns: []string{"string"}, Doc: "Format key/value pairs."},
	{Module: "holo.fmt", Name: "table", Params: []ambientParam{{"rows", "table"}}, Returns: []string{"string"}, Doc: "Format a table."},
	{Module: "holo.fmt", Name: "separator", Returns: []string{"string"}, Doc: "Return a horizontal separator."},
	{Module: "holo.fmt", Name: "header", Params: []ambientParam{{"text", "string"}}, Returns: []string{"string"}, Doc: "Format a header."},
	{Module: "holo.fmt", Name: "parse", Params: []ambientParam{{"markup", "string"}}, Returns: []string{"string"}, Doc: "Parse holo markup into rendered text."},

	// stdlib.go emitLocation:286-289 / emitCharacter:309-312 / emitGlobal:332-334 → (id..., event_type, payload table).
	{Module: "holo.emit", Name: "location", Params: []ambientParam{{"location_id", "string"}, {"event_type", "string"}, {"payload", "table"}}, Doc: "Emit an event to a location."},
	{Module: "holo.emit", Name: "character", Params: []ambientParam{{"character_id", "string"}, {"event_type", "string"}, {"payload", "table"}}, Doc: "Emit an event to a character."},
	{Module: "holo.emit", Name: "global", Params: []ambientParam{{"event_type", "string"}, {"payload", "table"}}, Doc: "Emit a global event."},
	{Module: "holo.emit", Name: "flush", Doc: "Flush buffered emit calls."},
}
