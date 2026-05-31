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
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/idgen"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
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
	engine           types.AccessPolicyEngine
	commandQuerier   *commandquery.Querier
	auditor          pluginauthz.Auditor
	propertyRegistry *property.Registry
	sessionAccess    session.Access
	capabilities     *CapabilityRegistry
	streamRegistry   plugins.StreamRegistry
	focusOps         FocusOps
	settingsOps      SettingsOps
	historyReader    HistoryReader
	auditDecryptor   AuditDecryptor
	// pluginConfigs holds the merged (opaque) config per plugin, set by the
	// Lua host before Register. nil/absent → empty holomush.config. Guarded by
	// pluginConfigsMu because SetPluginConfigs/pluginConfigFor run per delivery
	// and DeliverEvent/DeliverCommand/QuerySessionStreams can race on a shared
	// *Functions; the other Set* setters are startup-only so need no lock.
	pluginConfigsMu sync.RWMutex
	pluginConfigs   map[string]map[string]string
}

// Option configures Functions.
type Option func(*Functions)

// WithEngine sets the access policy engine for holomush.evaluate (evaluate.go)
// and the requires-gated capability checks (functions.go). Command-visibility
// filtering does NOT use this engine directly — that flows through the
// commandquery.Querier wired via WithCommandQuerier / SetCommandQuerier
// (design spec INV-1: single command-visibility filter).
func WithEngine(engine types.AccessPolicyEngine) Option {
	return func(f *Functions) {
		f.engine = engine
	}
}

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

// WithSettingsOps sets the plugin-partitioned settings store seam for the
// holomush.get_setting / set_setting host functions (plugin-runtime-symmetry
// with the binary GetSetting / SetSetting RPCs; INV-8).
func WithSettingsOps(so SettingsOps) Option {
	return func(f *Functions) { f.settingsOps = so }
}

// WithHistoryReader sets the event store reader for query_stream_history host function.
func WithHistoryReader(hr HistoryReader) Option {
	return func(f *Functions) { f.historyReader = hr }
}

// WithAuditLogger sets the audit sink for holomush.evaluate calls.
// When set, each authorization decision is logged via pluginauthz.Auditor.
// The *audit.Logger type satisfies this interface.
func WithAuditLogger(a pluginauthz.Auditor) Option {
	return func(f *Functions) { f.auditor = a }
}

// WithAuditDecryptor sets the audit read-back decryptor for the
// decrypt_own_audit_rows host function.
func WithAuditDecryptor(d AuditDecryptor) Option {
	return func(f *Functions) { f.auditDecryptor = d }
}

// SetAuditDecryptor injects the audit read-back decryptor after construction.
// Same late-binding rationale as SetHistoryReader: the decryptor's OwnerMap +
// crypto deps are assembled during gRPC subsystem Start, after plugin loading.
func (f *Functions) SetAuditDecryptor(d AuditDecryptor) {
	f.auditDecryptor = d
}

// SetFocusOps sets the focus coordinator for join/leave/present focus host
// functions. Supports late-binding: the coordinator is created during gRPC
// subsystem Start, which runs after plugin loading. Lua VMs are created
// per-event delivery, so the value is read at Register time.
func (f *Functions) SetFocusOps(fo FocusOps) {
	f.focusOps = fo
}

// SetSettingsOps sets the plugin-partitioned settings store seam for the
// holomush.get_setting / set_setting host functions. Supports late-binding: the
// settings stores are assembled during gRPC subsystem Start, after plugin
// loading. Lua VMs are created per-event delivery, so the value is read at
// Register time (same late-binding rationale as SetFocusOps).
func (f *Functions) SetSettingsOps(so SettingsOps) {
	f.settingsOps = so
}

// SetHistoryReader sets the event store reader for query_stream_history host
// function. Same late-binding rationale as SetFocusOps.
func (f *Functions) SetHistoryReader(hr HistoryReader) {
	f.historyReader = hr
}

// SetCommandQuerier late-binds the shared command querier after the command
// registry is built. The querier is constructed in PluginSubsystem.Start after
// both s.cmdRegistry (line ~391) and s.aliasCache are populated — after
// hostfunc.New (line ~193) — so it cannot be injected via WithCommandQuerier at
// construction time. This setter is the production wiring point.
func (f *Functions) SetCommandQuerier(q *commandquery.Querier) {
	f.commandQuerier = q
}

// SetPluginConfigs injects the merged per-plugin config map (plugin name →
// merged key/value pairs) into the Functions bridge. Called by the Lua host
// before each per-delivery Register so holomush.config reflects the plugin's
// effective config (manifest defaults overlaid by server overrides).
func (f *Functions) SetPluginConfigs(c map[string]map[string]string) {
	f.pluginConfigsMu.Lock()
	defer f.pluginConfigsMu.Unlock()
	f.pluginConfigs = c
}

// pluginConfigFor returns the merged config for the named plugin, or nil when
// absent. nil produces an empty holomush.config table (all accessors → nil).
// The returned inner map is stable after Load (never mutated), so Lua closures
// may read it lock-free once Register hands it to registerConfigTable.
func (f *Functions) pluginConfigFor(name string) map[string]string {
	f.pluginConfigsMu.RLock()
	defer f.pluginConfigsMu.RUnlock()
	return f.pluginConfigs[name]
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

	// Authorization query
	ls.SetField(mod, "evaluate", ls.NewFunction(f.evaluateFn(pluginName)))

	// Plugin-partitioned settings (parity with the binary GetSetting/SetSetting RPCs).
	ls.SetField(mod, "get_setting", ls.NewFunction(f.getSettingFn(pluginName)))
	ls.SetField(mod, "set_setting", ls.NewFunction(f.setSettingFn(pluginName)))

	// Register stream management functions (always; guard against nil registry inside).
	RegisterStreamFuncs(ls, mod, f.streamRegistry)

	// Register focus management functions.
	RegisterFocusFuncs(ls, mod, f.focusOps, f.historyReader)

	// Register audit read-back decrypt functions.
	RegisterAuditFuncs(ls, mod, pluginName, f.auditDecryptor)

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

	// Inject holomush.config typed accessors from the merged plugin config.
	// Called last on mod so it can coexist with all other mod fields.
	registerConfigTable(ls, mod, f.pluginConfigFor(pluginName))

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
		{Name: "holomush.evaluate"},
		{Name: "holomush.get_setting"},
		{Name: "holomush.set_setting"},
		// Unconditionally registered by RegisterStreamFuncs.
		{Name: "holomush.add_session_stream"},
		{Name: "holomush.remove_session_stream"},
		// Unconditionally registered by RegisterFocusFuncs.
		{Name: "holomush.join_focus"},
		{Name: "holomush.leave_focus"},
		{Name: "holomush.present_focus"},
		{Name: "holomush.query_stream_history"},
		// Unconditionally registered by RegisterAuditFuncs.
		{Name: "holomush.decrypt_own_audit_rows"},
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
			//nolint:sloglint // plugin-supplied log message, dynamic by design
			logger.Debug(message)
		case "info":
			//nolint:sloglint // plugin-supplied log message, dynamic by design
			logger.Info(message)
		case "warn":
			//nolint:sloglint // plugin-supplied log message, dynamic by design
			logger.Warn(message)
		case "error":
			//nolint:sloglint // plugin-supplied log message, dynamic by design
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
		slog.ErrorContext(ctx, "failed to create KV access request",
			"plugin", pluginName, "action", action, "key", key, "error", err)
		return "access check failed"
	}

	decision, err := f.engine.Evaluate(ctx, req)
	if err != nil {
		slog.ErrorContext(ctx, "KV access check engine error",
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
