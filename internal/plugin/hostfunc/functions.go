// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package hostfunc provides host functions to Lua plugins.
//
// Host functions expose server capabilities to plugins in a controlled way.
// Access control is enforced via ABAC policies: world operations at the
// service layer (world.Service.checkAccess), KV operations at the hostfunc
// layer (checkKVAccess), and command access via the AccessPolicyEngine.
package hostfunc

import (
	"context"
	"log/slog"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/idgen"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/property"
	"github.com/holomush/holomush/internal/session"
)

// defaultPluginQueryTimeout is the timeout for plugin host function operations
// including KV operations and world queries. This prevents indefinite hangs
// when backend services are slow or unresponsive.
const defaultPluginQueryTimeout = 5 * time.Second

// KVStore provides namespaced key-value storage.
type KVStore interface {
	Get(ctx context.Context, namespace, key string) ([]byte, error)
	Set(ctx context.Context, namespace, key string, value []byte) error
	Delete(ctx context.Context, namespace, key string) error
}

// Functions provides host functions to Lua plugins.
type Functions struct {
	kvStore          KVStore
	worldMutator     WorldMutator
	commandRegistry  CommandRegistry
	engine           types.AccessPolicyEngine
	propertyRegistry *property.Registry
	sessionAccess    session.Access
	capabilities     *CapabilityRegistry
	streamRegistry   plugins.StreamRegistry
	focusOps         FocusOps
	historyReader    HistoryReader
}

// Option configures Functions.
type Option func(*Functions)

// WithWorldService sets the world service for world query and mutation functions.
// Each plugin will get its own adapter with authorization subject "plugin:<name>".
// The service must implement WorldMutator; this is enforced at compile-time.
func WithWorldService(svc WorldMutator) Option {
	return func(f *Functions) {
		f.worldMutator = svc
	}
}

// WithPropertyRegistry sets the property registry for property host functions.
func WithPropertyRegistry(registry *property.Registry) Option {
	return func(f *Functions) {
		f.propertyRegistry = registry
	}
}

// WithSessionAccess sets the session access dependency for holo.session.* host functions.
// When set, plugins can call holo.session.find_by_name and holo.session.set_last_whispered.
func WithSessionAccess(sa session.Access) Option {
	return func(f *Functions) {
		f.sessionAccess = sa
	}
}

// WithCapabilities sets the capability registry for requires-based Lua function injection.
// When set, Register injects capability modules declared in the plugin's manifest requires list.
func WithCapabilities(reg *CapabilityRegistry) Option {
	return func(f *Functions) {
		f.capabilities = reg
	}
}

// WithStreamRegistry sets the stream registry for add/remove session stream host functions.
func WithStreamRegistry(r plugins.StreamRegistry) Option {
	return func(f *Functions) {
		f.streamRegistry = r
	}
}

// WithFocusOps sets the focus coordinator for join/leave/present focus host functions.
func WithFocusOps(fo FocusOps) Option {
	return func(f *Functions) { f.focusOps = fo }
}

// WithHistoryReader sets the event store reader for query_stream_history host function.
func WithHistoryReader(hr HistoryReader) Option {
	return func(f *Functions) { f.historyReader = hr }
}

// SetFocusOps sets the focus coordinator for join/leave/present focus host
// functions. Supports late-binding: the coordinator is created during gRPC
// subsystem Start, which runs after plugin loading. Lua VMs are created
// per-event delivery, so the value is read at Register time.
func (f *Functions) SetFocusOps(fo FocusOps) {
	f.focusOps = fo
}

// SetHistoryReader sets the event store reader for query_stream_history host
// function. Same late-binding rationale as SetFocusOps.
func (f *Functions) SetHistoryReader(hr HistoryReader) {
	f.historyReader = hr
}

// New creates host functions with dependencies.
// KVStore may be nil; KV functions will return errors if called.
func New(kv KVStore, opts ...Option) *Functions {
	f := &Functions{
		kvStore: kv,
	}
	for _, opt := range opts {
		opt(f)
	}
	if f.propertyRegistry == nil {
		f.propertyRegistry = property.SharedRegistry()
	}
	return f
}

// Register adds host functions to a Lua state.
// The optional requires parameter lists proto service names from the plugin manifest;
// matching capability modules are injected into the Lua state. Plugins without
// requires declarations call Register with no requires argument — this is a no-op
// for capability injection and is always safe.
func (f *Functions) Register(ls *lua.LState, pluginName string, requires ...string) {
	// Register the holo.* stdlib (fmt, emit namespaces)
	RegisterStdlib(ls)

	// Register holo.session namespace if session access is configured.
	if f.sessionAccess != nil {
		holoTable, ok := ls.GetGlobal("holo").(*lua.LTable)
		if ok {
			RegisterSessionFuncs(ls, holoTable, f.sessionAccess)
		}
	}

	mod := ls.NewTable()

	// Logging
	ls.SetField(mod, "log", ls.NewFunction(f.logFn(pluginName)))

	// Request ID
	ls.SetField(mod, "new_request_id", ls.NewFunction(f.newRequestIDFn()))

	// KV operations
	ls.SetField(mod, "kv_get", ls.NewFunction(f.kvGetFn(pluginName)))
	ls.SetField(mod, "kv_set", ls.NewFunction(f.kvSetFn(pluginName)))
	ls.SetField(mod, "kv_delete", ls.NewFunction(f.kvDeleteFn(pluginName)))

	// World queries
	ls.SetField(mod, "query_location", ls.NewFunction(f.queryLocationFn(pluginName)))
	ls.SetField(mod, "query_character", ls.NewFunction(f.queryCharacterFn(pluginName)))
	ls.SetField(mod, "query_location_characters", ls.NewFunction(f.queryLocationCharactersFn(pluginName)))
	ls.SetField(mod, "query_object", ls.NewFunction(f.queryObjectFn(pluginName)))

	// World mutations
	ls.SetField(mod, "create_location", ls.NewFunction(f.createLocationFn(pluginName)))
	ls.SetField(mod, "create_exit", ls.NewFunction(f.createExitFn(pluginName)))
	ls.SetField(mod, "create_object", ls.NewFunction(f.createObjectFn(pluginName)))
	ls.SetField(mod, "find_location", ls.NewFunction(f.findLocationFn(pluginName)))
	ls.SetField(mod, "set_property", ls.NewFunction(f.setPropertyFn(pluginName)))
	ls.SetField(mod, "get_property", ls.NewFunction(f.getPropertyFn(pluginName)))

	// Command registry functions
	ls.SetField(mod, "list_commands", ls.NewFunction(f.listCommandsFn(pluginName)))
	ls.SetField(mod, "get_command_help", ls.NewFunction(f.getCommandHelpFn(pluginName)))

	// Register stream management functions (always; guard against nil registry inside).
	RegisterStreamFuncs(ls, mod, f.streamRegistry)

	// Register focus management functions.
	RegisterFocusFuncs(ls, mod, f.focusOps, f.historyReader)

	// INV-S5: install a no-op register_emit_type in the per-delivery
	// hostfunc surface. Lua plugins call register_emit_type at top level
	// (idempotent registrations), and main.lua is re-executed on every
	// event/command delivery — so the function MUST exist at runtime even
	// though only Load-time calls matter. RegisterWithEmitCapture (used by
	// the Lua Host's INV-S5 Load pass) overwrites this with the capturing
	// variant.
	ls.SetField(mod, "register_emit_type", ls.NewFunction(func(ls *lua.LState) int {
		_ = ls.CheckString(1)
		ls.Push(lua.LTrue)
		return 1
	}))

	ls.SetGlobal("holomush", mod)

	// Inject capability modules for declared requires.
	if f.capabilities != nil && len(requires) > 0 {
		f.capabilities.InjectRequired(ls, requires, pluginName)
	}
}

// RegisterWithEmitCapture is the variant of Register used during the
// Lua Host's INV-S5 Load-pass. Identical to Register, but overwrites the
// no-op register_emit_type stub with the capturing variant that appends
// to reg.
//
// The standard per-delivery Functions.Register installs a no-op
// register_emit_type (see the no-op installation block above); plugin
// main.lua is re-executed on every event/command delivery, so calls to
// register_emit_type MUST not raise at runtime. Only Load-time captures
// are honored by the INV-S5 substrate validator — per-delivery calls
// are accepted but discarded by the no-op stub.
func (f *Functions) RegisterWithEmitCapture(
	ls *lua.LState,
	pluginName string,
	reg *LuaEmitRegistry,
	requires ...string,
) {
	f.Register(ls, pluginName, requires...)
	if mod, ok := ls.GetGlobal("holomush").(*lua.LTable); ok {
		RegisterEmitTypeFuncs(ls, mod, reg)
	}
}

// AuditEntry names one Lua-visible global path installed by Register,
// for the purposes of the context-respect meta-test.
type AuditEntry struct {
	// Name is the Lua global path (e.g. "holomush.log"). The meta-test
	// invokes each by DoString("<name>()") under a cancelled context.
	Name string
}

// RegisteredFunctionsForAudit returns the list of holomush.* globals that
// an unconfigured Functions (hostfunc.New(nil)) installs via Register.
// Test-only; the audit meta-test in
// internal/plugin/hostfunc/context_audit_test.go iterates this list.
//
// Keep this list in sync with Register. New host functions that could
// block under adversarial input MUST be added here so the meta-test
// exercises them.
func (f *Functions) RegisteredFunctionsForAudit() []AuditEntry {
	return []AuditEntry{
		{Name: "holomush.log"},
		{Name: "holomush.new_request_id"},
		{Name: "holomush.kv_get"},
		{Name: "holomush.kv_set"},
		{Name: "holomush.kv_delete"},
		{Name: "holomush.query_location"},
		{Name: "holomush.query_character"},
		{Name: "holomush.query_location_characters"},
		{Name: "holomush.query_object"},
		{Name: "holomush.create_location"},
		{Name: "holomush.create_exit"},
		{Name: "holomush.create_object"},
		{Name: "holomush.find_location"},
		{Name: "holomush.set_property"},
		{Name: "holomush.get_property"},
		{Name: "holomush.list_commands"},
		{Name: "holomush.get_command_help"},
		// Unconditionally registered by RegisterStreamFuncs.
		{Name: "holomush.add_session_stream"},
		{Name: "holomush.remove_session_stream"},
		// Unconditionally registered by RegisterFocusFuncs.
		{Name: "holomush.join_focus"},
		{Name: "holomush.leave_focus"},
		{Name: "holomush.present_focus"},
		{Name: "holomush.query_stream_history"},
		// INV-S5 (jg9b.3): per-delivery no-op; Load-pass capturing variant
		// is installed by RegisterWithEmitCapture.
		{Name: "holomush.register_emit_type"},
	}
}

func (f *Functions) logFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		level := L.CheckString(1)
		message := L.CheckString(2)

		logger := slog.Default().With("plugin", pluginName)
		switch level {
		case "debug":
			logger.Debug(message)
		case "info":
			logger.Info(message)
		case "warn":
			logger.Warn(message)
		case "error":
			logger.Error(message)
		default:
			slog.Warn("invalid log level from plugin",
				"plugin", pluginName,
				"requested_level", level)
			L.RaiseError("invalid log level %q: must be debug, info, warn, or error", level)
			return 0
		}
		return 0
	}
}

func (f *Functions) newRequestIDFn() lua.LGFunction {
	return func(L *lua.LState) int {
		// idgen.New() panics only if crypto/rand is unavailable (unrecoverable OS failure).
		reqID := idgen.New()
		L.Push(lua.LString(reqID.String()))
		return 1
	}
}

// checkKVAccess evaluates ABAC for a KV operation. Returns an error string
// for Lua if denied, or empty string if allowed.
func (f *Functions) checkKVAccess(L *lua.LState, pluginName, action, key string) string { //nolint:gocritic // L is standard gopher-lua convention
	if f.engine == nil {
		slog.Warn("KV access denied: no ABAC engine configured",
			"plugin", pluginName, "action", action, "key", key)
		return "access engine not available"
	}

	subject := access.PluginSubject(pluginName)
	resource := access.KVResource(pluginName, key)

	parentCtx := L.Context()
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, defaultPluginQueryTimeout)
	defer cancel()

	req, err := types.NewAccessRequest(subject, action, resource, nil)
	if err != nil {
		slog.Error("failed to create KV access request",
			"plugin", pluginName, "action", action, "key", key, "error", err)
		return "access check failed"
	}

	decision, err := f.engine.Evaluate(ctx, req)
	if err != nil {
		slog.Error("KV access check engine error",
			"plugin", pluginName, "action", action, "key", key, "error", err)
		return "access check failed"
	}

	if !decision.IsAllowed() {
		return "access denied"
	}
	return ""
}

func (f *Functions) kvGetFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		key := L.CheckString(1)
		if key == "" {
			L.RaiseError("kv_get: key cannot be empty")
			return 0
		}

		if errMsg := f.checkKVAccess(L, pluginName, "read", key); errMsg != "" {
			L.Push(lua.LNil)
			L.Push(lua.LString(errMsg))
			return 2
		}

		if f.kvStore == nil {
			slog.Error("kv_get called but store unavailable",
				"plugin", pluginName,
				"key", key)
			L.Push(lua.LNil)
			L.Push(lua.LString("kv store not available"))
			return 2
		}

		parentCtx := L.Context()
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(parentCtx, defaultPluginQueryTimeout)
		defer cancel()

		value, err := f.kvStore.Get(ctx, pluginName, key)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(SanitizeErrorForPlugin(PluginErrorContext{Plugin: pluginName, Operation: "get", Subject: "key", SubjectID: key}, err)))
			return 2
		}

		if value == nil {
			L.Push(lua.LNil)
			L.Push(lua.LNil) // No error, just not found
			return 2
		}

		L.Push(lua.LString(string(value)))
		L.Push(lua.LNil) // No error
		return 2
	}
}

func (f *Functions) kvSetFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		key := L.CheckString(1)
		value := L.CheckString(2)
		if key == "" {
			L.RaiseError("kv_set: key cannot be empty")
			return 0
		}

		if errMsg := f.checkKVAccess(L, pluginName, "write", key); errMsg != "" {
			L.Push(lua.LNil)
			L.Push(lua.LString(errMsg))
			return 2
		}

		if f.kvStore == nil {
			slog.Error("kv_set called but store unavailable",
				"plugin", pluginName,
				"key", key)
			L.Push(lua.LNil)
			L.Push(lua.LString("kv store not available"))
			return 2
		}

		parentCtx := L.Context()
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(parentCtx, defaultPluginQueryTimeout)
		defer cancel()

		if err := f.kvStore.Set(ctx, pluginName, key, []byte(value)); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(SanitizeErrorForPlugin(PluginErrorContext{Plugin: pluginName, Operation: "set", Subject: "key", SubjectID: key}, err)))
			return 2
		}

		L.Push(lua.LNil) // No result
		L.Push(lua.LNil) // No error
		return 2
	}
}

func (f *Functions) kvDeleteFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		key := L.CheckString(1)
		if key == "" {
			L.RaiseError("kv_delete: key cannot be empty")
			return 0
		}

		if errMsg := f.checkKVAccess(L, pluginName, "delete", key); errMsg != "" {
			L.Push(lua.LNil)
			L.Push(lua.LString(errMsg))
			return 2
		}

		if f.kvStore == nil {
			slog.Error("kv_delete called but store unavailable",
				"plugin", pluginName,
				"key", key)
			L.Push(lua.LNil)
			L.Push(lua.LString("kv store not available"))
			return 2
		}

		parentCtx := L.Context()
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(parentCtx, defaultPluginQueryTimeout)
		defer cancel()

		if err := f.kvStore.Delete(ctx, pluginName, key); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(SanitizeErrorForPlugin(PluginErrorContext{Plugin: pluginName, Operation: "delete", Subject: "key", SubjectID: key}, err)))
			return 2
		}

		L.Push(lua.LNil) // No result
		L.Push(lua.LNil) // No error
		return 2
	}
}
