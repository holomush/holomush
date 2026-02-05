// Copyright 2026 HoloMUSH Contributors

package handlers

import "github.com/holomush/holomush/internal/command"

func checkCommandShadows(cache *command.AliasCache, registry *command.Registry, alias string) bool {
	return cache.ShadowsCommand(alias, registry)
}

func checkSystemAliasShadows(cache *command.AliasCache, alias string) (string, bool) {
	return cache.ShadowsSystemAlias(alias)
}
