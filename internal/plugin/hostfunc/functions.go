// Package hostfunc provides host functions to Lua plugins.
//
// Host functions expose server capabilities to plugins in a controlled way.
// Functions that access sensitive resources require capability checks.
package hostfunc

import (
	"context"
	"log/slog"

	"github.com/holomush/holomush/internal/plugin/capability"
	"github.com/oklog/ulid/v2"
	lua "github.com/yuin/gopher-lua"
)

// KVStore provides namespaced key-value storage.
type KVStore interface {
	Get(ctx context.Context, namespace, key string) ([]byte, error)
	Set(ctx context.Context, namespace, key string, value []byte) error
	Delete(ctx context.Context, namespace, key string) error
}

// WorldReader provides read-only access to world data.
type WorldReader interface {
	// Future: GetLocation, GetCharacter, GetObject
}

// Functions provides host functions to Lua plugins.
type Functions struct {
	kvStore  KVStore
	world    WorldReader
	enforcer *capability.Enforcer
}

// New creates host functions with dependencies.
func New(kv KVStore, world WorldReader, enforcer *capability.Enforcer) *Functions {
	return &Functions{
		kvStore:  kv,
		world:    world,
		enforcer: enforcer,
	}
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

	ls.SetGlobal("holomush", mod)
}

func (f *Functions) wrap(plugin, capName string, fn lua.LGFunction) lua.LGFunction {
	return func(L *lua.LState) int {
		if !f.enforcer.Check(plugin, capName) {
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
			logger.Info(message)
		}
		return 0
	}
}

func (f *Functions) newRequestIDFn() lua.LGFunction {
	return func(L *lua.LState) int {
		id := ulid.Make()
		L.Push(lua.LString(id.String()))
		return 1
	}
}

func (f *Functions) kvGetFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		key := L.CheckString(1)

		if f.kvStore == nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("kv store not available"))
			return 2
		}

		value, err := f.kvStore.Get(context.Background(), pluginName, key)
		if err != nil {
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

		if f.kvStore == nil {
			L.Push(lua.LString("kv store not available"))
			return 1
		}

		if err := f.kvStore.Set(context.Background(), pluginName, key, []byte(value)); err != nil {
			L.Push(lua.LString(err.Error()))
			return 1
		}

		return 0
	}
}

func (f *Functions) kvDeleteFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		key := L.CheckString(1)

		if f.kvStore == nil {
			L.Push(lua.LString("kv store not available"))
			return 1
		}

		if err := f.kvStore.Delete(context.Background(), pluginName, key); err != nil {
			L.Push(lua.LString(err.Error()))
			return 1
		}

		return 0
	}
}
