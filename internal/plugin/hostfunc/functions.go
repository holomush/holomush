// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package hostfunc provides host functions to Lua plugins.
//
// Host functions expose server capabilities to plugins in a controlled way.
// Functions that access sensitive resources require capability checks.
package hostfunc

import (
	"context"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	lua "github.com/yuin/gopher-lua"
)

// kvTimeout is the default timeout for KV operations to prevent indefinite hangs.
const kvTimeout = 5 * time.Second

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
	kvStore  KVStore
	enforcer CapabilityChecker
	world    WorldQuerier
}

// Option configures Functions.
type Option func(*Functions)

// WithWorldQuerier sets the world querier for world query functions.
func WithWorldQuerier(w WorldQuerier) Option {
	return func(f *Functions) {
		f.world = w
	}
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
	return f
}

// Register adds host functions to a Lua state.
func (f *Functions) Register(ls *lua.LState, pluginName string) {
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

		ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
		defer cancel()

		value, err := f.kvStore.Get(ctx, pluginName, key)
		if err != nil {
			slog.Error("kv_get failed",
				"plugin", pluginName,
				"key", key,
				"error", err)
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
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

		ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
		defer cancel()

		if err := f.kvStore.Set(ctx, pluginName, key, []byte(value)); err != nil {
			slog.Error("kv_set failed",
				"plugin", pluginName,
				"key", key,
				"error", err)
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
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

		ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
		defer cancel()

		if err := f.kvStore.Delete(ctx, pluginName, key); err != nil {
			slog.Error("kv_delete failed",
				"plugin", pluginName,
				"key", key,
				"error", err)
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		L.Push(lua.LNil) // No result
		L.Push(lua.LNil) // No error
		return 2
	}
}
