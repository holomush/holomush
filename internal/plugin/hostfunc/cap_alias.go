// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"

	lua "github.com/yuin/gopher-lua"
)

// AliasEntry carries a single alias mapping for use within the hostfunc package.
// It mirrors the plugins.AliasEntry type to avoid a cross-package import in
// the narrow-interface layer.
type AliasEntry struct {
	Alias   string
	Command string
}

// AliasAccess is the narrow interface required by AliasCapability.
// It covers the alias operations needed by the capability module without
// exposing the full host service surface.
type AliasAccess interface {
	// SetPlayerAlias creates or updates a player alias.
	SetPlayerAlias(ctx context.Context, playerID, alias, command string) error

	// DeletePlayerAlias removes a player alias.
	DeletePlayerAlias(ctx context.Context, playerID, alias string) error

	// ListPlayerAliases returns all aliases for a player.
	ListPlayerAliases(ctx context.Context, playerID string) ([]AliasEntry, error)

	// CheckAliasShadow checks whether an alias name shadows an existing command.
	// Returns (shadows bool, command string, err error).
	CheckAliasShadow(ctx context.Context, alias string) (bool, string, error)

	// SetSystemAlias creates or updates a system-wide alias. See
	// store.AliasRepository.SetSystemAlias for parameter semantics.
	// For operator-driven sysalias: createdBy is the player ID and source is
	// the literal "sysalias".
	SetSystemAlias(ctx context.Context, alias, cmd, createdBy, source string) error

	// DeleteSystemAlias removes a system-wide alias.
	DeleteSystemAlias(ctx context.Context, alias string) error

	// ListSystemAliases returns all system-wide aliases.
	ListSystemAliases(ctx context.Context) ([]AliasEntry, error)
}

// AliasCapability implements the Capability interface for the alias namespace.
// It registers Lua host functions under the global "alias" table.
type AliasCapability struct {
	aliases AliasAccess
}

// Ensure AliasCapability satisfies the Capability interface at compile time.
var _ Capability = (*AliasCapability)(nil)

// NewAliasCapability creates an AliasCapability backed by the given AliasAccess.
func NewAliasCapability(aliases AliasAccess) *AliasCapability {
	return &AliasCapability{aliases: aliases}
}

// Namespace returns "alias", the Lua global table name for this capability.
func (c *AliasCapability) Namespace() string {
	return "alias"
}

// Register injects the alias.* functions into the Lua state as a global table.
func (c *AliasCapability) Register(L *lua.LState, pluginName string) { //nolint:gocritic // L is conventional gopher-lua parameter name
	tbl := L.NewTable()
	L.SetField(tbl, "set_player", L.NewFunction(c.setPlayerFn(pluginName)))
	L.SetField(tbl, "delete_player", L.NewFunction(c.deletePlayerFn(pluginName)))
	L.SetField(tbl, "list_player", L.NewFunction(c.listPlayerFn(pluginName)))
	L.SetField(tbl, "check_shadow", L.NewFunction(c.checkShadowFn(pluginName)))
	L.SetField(tbl, "set_system", L.NewFunction(c.setSystemFn(pluginName)))
	L.SetField(tbl, "delete_system", L.NewFunction(c.deleteSystemFn(pluginName)))
	L.SetField(tbl, "list_system", L.NewFunction(c.listSystemFn(pluginName)))
	L.SetGlobal("alias", tbl)
}

// setPlayerFn returns a Lua function implementing alias.set_player(player_id, name, command).
// Returns nil on success, or nil + error string on failure.
func (c *AliasCapability) setPlayerFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		playerID := L.CheckString(1)
		alias := L.CheckString(2)
		command := L.CheckString(3)

		ctx := luaContext(L)
		if err := c.aliases.SetPlayerAlias(ctx, playerID, alias, command); err != nil {
			return capError(L, PluginErrorContext{
				Plugin:    pluginName,
				Operation: "set_player",
				Subject:   "alias",
				SubjectID: alias,
			}, err)
		}
		return 0
	}
}

// deletePlayerFn returns a Lua function implementing alias.delete_player(player_id, name).
// Returns nil on success, or nil + error string on failure.
func (c *AliasCapability) deletePlayerFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		playerID := L.CheckString(1)
		alias := L.CheckString(2)

		ctx := luaContext(L)
		if err := c.aliases.DeletePlayerAlias(ctx, playerID, alias); err != nil {
			return capError(L, PluginErrorContext{
				Plugin:    pluginName,
				Operation: "delete_player",
				Subject:   "alias",
				SubjectID: alias,
			}, err)
		}
		return 0
	}
}

// listPlayerFn returns a Lua function implementing alias.list_player(player_id).
// Returns an array table of {alias, command} tables, or nil + error string on failure.
func (c *AliasCapability) listPlayerFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		playerID := L.CheckString(1)

		ctx := luaContext(L)
		entries, err := c.aliases.ListPlayerAliases(ctx, playerID)
		if err != nil {
			return capError(L, PluginErrorContext{
				Plugin:    pluginName,
				Operation: "list_player",
				Subject:   "alias",
				SubjectID: playerID,
			}, err)
		}

		arr := L.NewTable()
		for _, e := range entries {
			row := L.NewTable()
			L.SetField(row, "alias", lua.LString(e.Alias))
			L.SetField(row, "command", lua.LString(e.Command))
			arr.Append(row)
		}
		L.Push(arr)
		return 1
	}
}

// checkShadowFn returns a Lua function implementing alias.check_shadow(name).
// Returns a table {shadows bool, command string} on success, or nil + error string on failure.
func (c *AliasCapability) checkShadowFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		alias := L.CheckString(1)

		ctx := luaContext(L)
		shadows, cmd, err := c.aliases.CheckAliasShadow(ctx, alias)
		if err != nil {
			return capError(L, PluginErrorContext{
				Plugin:    pluginName,
				Operation: "check_shadow",
				Subject:   "alias",
				SubjectID: alias,
			}, err)
		}

		tbl := L.NewTable()
		L.SetField(tbl, "shadows", lua.LBool(shadows))
		L.SetField(tbl, "command", lua.LString(cmd))
		L.Push(tbl)
		return 1
	}
}

// setSystemFn returns a Lua function implementing alias.set_system(name, command, created_by).
// Returns nil on success, or nil + error string on failure.
func (c *AliasCapability) setSystemFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		alias := L.CheckString(1)
		command := L.CheckString(2)
		createdBy := L.CheckString(3)

		ctx := luaContext(L)
		if err := c.aliases.SetSystemAlias(ctx, alias, command, createdBy, "sysalias"); err != nil {
			return capError(L, PluginErrorContext{
				Plugin:    pluginName,
				Operation: "set_system",
				Subject:   "alias",
				SubjectID: alias,
			}, err)
		}
		return 0
	}
}

// deleteSystemFn returns a Lua function implementing alias.delete_system(name).
// Returns nil on success, or nil + error string on failure.
func (c *AliasCapability) deleteSystemFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		alias := L.CheckString(1)

		ctx := luaContext(L)
		if err := c.aliases.DeleteSystemAlias(ctx, alias); err != nil {
			return capError(L, PluginErrorContext{
				Plugin:    pluginName,
				Operation: "delete_system",
				Subject:   "alias",
				SubjectID: alias,
			}, err)
		}
		return 0
	}
}

// listSystemFn returns a Lua function implementing alias.list_system().
// Returns an array table of {alias, command} tables, or nil + error string on failure.
func (c *AliasCapability) listSystemFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		ctx := luaContext(L)
		entries, err := c.aliases.ListSystemAliases(ctx)
		if err != nil {
			return capError(L, PluginErrorContext{
				Plugin:    pluginName,
				Operation: "list_system",
				Subject:   "alias",
			}, err)
		}

		arr := L.NewTable()
		for _, e := range entries {
			row := L.NewTable()
			L.SetField(row, "alias", lua.LString(e.Alias))
			L.SetField(row, "command", lua.LString(e.Command))
			arr.Append(row)
		}
		L.Push(arr)
		return 1
	}
}
