// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package hostfunc provides host functions to Lua plugins.
//
// Host functions expose server capabilities to plugins in a controlled way.
// Functions that access sensitive resources require capability checks.
package hostfunc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/property"
	"github.com/holomush/holomush/internal/world"
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

// CapabilityChecker validates plugin capabilities.
type CapabilityChecker interface {
	Check(plugin, capability string) bool
}

// Functions provides host functions to Lua plugins.
type Functions struct {
	kvStore          KVStore
	enforcer         CapabilityChecker
	worldMutator     WorldMutator
	commandRegistry  CommandRegistry
	engine           types.AccessPolicyEngine
	propertyRegistry *property.Registry
}

// Option configures Functions.
type Option func(*Functions)

// WithWorldService sets the world service for world query and mutation functions.
// Each plugin will get its own adapter with authorization subject "system:plugin:<name>".
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

// WithWorldQuerier is no longer supported and has been removed.
//
// Deprecated: WithWorldQuerier was removed because WorldQuerier and WorldMutator
// have incompatible method signatures. Use [WithWorldService] instead, which
// requires a WorldMutator (which includes all read and write operations).
//
// Migration example:
//
//	// Before: WithWorldQuerier(querier)
//	// After:  WithWorldService(service) // service must implement WorldMutator
//
// This function always panics to fail fast at startup. Update your code to use
// WithWorldService with a service that implements the WorldMutator interface.
func WithWorldQuerier(_ WorldQuerier) Option {
	panic("hostfunc.WithWorldQuerier: this function has been removed. " +
		"Use WithWorldService instead with a service that implements WorldMutator. " +
		"WorldMutator includes all read methods (GetLocation, GetCharacter, etc.) " +
		"plus write methods (CreateLocation, UpdateLocation, etc.).")
}

// New creates host functions with dependencies.
// Panics if enforcer is nil (required dependency).
// KVStore may be nil; KV functions will return errors if called.
func New(kv KVStore, enforcer CapabilityChecker, opts ...Option) *Functions {
	if enforcer == nil {
		panic("hostfunc.New: enforcer cannot be nil")
	}
	f := &Functions{
		kvStore:  kv,
		enforcer: enforcer,
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
func (f *Functions) Register(ls *lua.LState, pluginName string) {
	// Register the holo.* stdlib (fmt, emit namespaces)
	RegisterStdlib(ls)

	mod := ls.NewTable()

	// Logging (no capability required)
	ls.SetField(mod, "log", ls.NewFunction(f.logFn(pluginName)))

	// Request ID (no capability required)
	ls.SetField(mod, "new_request_id", ls.NewFunction(f.newRequestIDFn()))

	// KV operations (capability required)
	ls.SetField(mod, "kv_get", ls.NewFunction(f.wrap(pluginName, "kv.read", f.kvGetFn(pluginName))))
	ls.SetField(mod, "kv_set", ls.NewFunction(f.wrap(pluginName, "kv.write", f.kvSetFn(pluginName))))
	ls.SetField(mod, "kv_delete", ls.NewFunction(f.wrap(pluginName, "kv.write", f.kvDeleteFn(pluginName))))

	// World queries (capability required)
	ls.SetField(mod, "query_room", ls.NewFunction(f.wrap(pluginName, "world.read.location", f.queryRoomFn(pluginName))))
	ls.SetField(mod, "query_character", ls.NewFunction(f.wrap(pluginName, "world.read.character", f.queryCharacterFn(pluginName))))
	ls.SetField(mod, "query_room_characters", ls.NewFunction(f.wrap(pluginName, "world.read.character", f.queryRoomCharactersFn(pluginName))))
	ls.SetField(mod, "query_object", ls.NewFunction(f.wrap(pluginName, "world.read.object", f.queryObjectFn(pluginName))))

	// World mutations (capability required)
	ls.SetField(mod, "create_location", ls.NewFunction(f.wrap(pluginName, "world.write.location", f.createLocationFn(pluginName))))
	ls.SetField(mod, "create_exit", ls.NewFunction(f.wrap(pluginName, "world.write.exit", f.createExitFn(pluginName))))
	ls.SetField(mod, "create_object", ls.NewFunction(f.wrap(pluginName, "world.write.object", f.createObjectFn(pluginName))))
	ls.SetField(mod, "find_location", ls.NewFunction(f.wrap(pluginName, "world.read.location", f.findLocationFn(pluginName))))
	ls.SetField(mod, "set_property", ls.NewFunction(f.wrap(pluginName, "property.set", f.setPropertyFn(pluginName))))
	ls.SetField(mod, "get_property", ls.NewFunction(f.wrap(pluginName, "property.get", f.getPropertyFn(pluginName))))

	// Command registry functions (capability required)
	ls.SetField(mod, "list_commands", ls.NewFunction(f.wrap(pluginName, "command.list", f.listCommandsFn(pluginName))))
	ls.SetField(mod, "get_command_help", ls.NewFunction(f.wrap(pluginName, "command.help", f.getCommandHelpFn(pluginName))))

	ls.SetGlobal("holomush", mod)
}

func (f *Functions) wrap(plugin, capName string, fn lua.LGFunction) lua.LGFunction {
	return func(L *lua.LState) int {
		if !f.enforcer.Check(plugin, capName) {
			slog.Warn("capability denied",
				"plugin", plugin,
				"capability", capName)
			L.RaiseError("capability denied: %s requires %s", plugin, capName)
			return 0
		}
		return fn(L)
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
		// ulid.Make() cannot panic per library documentation:
		// "NOTE: MustNew can't panic since DefaultEntropy never returns an error."
		// See: https://github.com/oklog/ulid/blob/main/ulid.go (func Make)
		id := ulid.Make()
		L.Push(lua.LString(id.String()))
		return 1
	}
}

// sanitizeKVErrorForPlugin converts internal KV errors to safe messages for plugins.
// It handles known error types (timeouts, not-found) with specific messages, and logs
// internal errors at ERROR level for operators while returning a generic message with
// a correlation ID to the plugin. This prevents leaking database internals to Lua code.
func sanitizeKVErrorForPlugin(pluginName, operation, key string, err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		slog.Warn("plugin KV operation timed out",
			"plugin", pluginName,
			"operation", operation,
			"key", key)
		return "operation timed out"
	}
	if errors.Is(err, world.ErrNotFound) {
		return "key not found"
	}
	if errors.Is(err, world.ErrPermissionDenied) {
		return "access denied"
	}

	// Generate correlation ID for this error instance.
	errorID := ulid.Make().String()

	slog.Error("internal error in plugin KV operation",
		"error_id", errorID,
		"plugin", pluginName,
		"operation", operation,
		"key", key,
		"error", err)
	return fmt.Sprintf("internal error (ref: %s)", errorID)
}

func (f *Functions) kvGetFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		key := L.CheckString(1)
		if key == "" {
			L.RaiseError("kv_get: key cannot be empty")
			return 0
		}

		if f.kvStore == nil {
			slog.Error("kv_get called but store unavailable",
				"plugin", pluginName,
				"key", key)
			L.Push(lua.LNil)
			L.Push(lua.LString("kv store not available"))
			return 2
		}

		ctx, cancel := context.WithTimeout(context.Background(), defaultPluginQueryTimeout)
		defer cancel()

		value, err := f.kvStore.Get(ctx, pluginName, key)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(sanitizeKVErrorForPlugin(pluginName, "get", key, err)))
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

		if f.kvStore == nil {
			slog.Error("kv_set called but store unavailable",
				"plugin", pluginName,
				"key", key)
			L.Push(lua.LNil)
			L.Push(lua.LString("kv store not available"))
			return 2
		}

		ctx, cancel := context.WithTimeout(context.Background(), defaultPluginQueryTimeout)
		defer cancel()

		if err := f.kvStore.Set(ctx, pluginName, key, []byte(value)); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(sanitizeKVErrorForPlugin(pluginName, "set", key, err)))
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

		if f.kvStore == nil {
			slog.Error("kv_delete called but store unavailable",
				"plugin", pluginName,
				"key", key)
			L.Push(lua.LNil)
			L.Push(lua.LString("kv store not available"))
			return 2
		}

		ctx, cancel := context.WithTimeout(context.Background(), defaultPluginQueryTimeout)
		defer cancel()

		if err := f.kvStore.Delete(ctx, pluginName, key); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(sanitizeKVErrorForPlugin(pluginName, "delete", key, err)))
			return 2
		}

		L.Push(lua.LNil) // No result
		L.Push(lua.LNil) // No error
		return 2
	}
}
