// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"maps"
	"strings"
	"sync"

	"github.com/oklog/ulid/v2"
)

// MaxExpansionDepth is the maximum depth for alias expansion to prevent infinite loops.
const MaxExpansionDepth = 10

// AliasCache manages alias resolution with player and system aliases.
// It is thread-safe for concurrent access.
type AliasCache struct {
	playerAliases map[ulid.ULID]map[string]string // playerID → alias → command
	systemAliases map[string]string               // alias → command
	mu            sync.RWMutex
}

// NewAliasCache creates a new alias cache.
func NewAliasCache() *AliasCache {
	return &AliasCache{
		playerAliases: make(map[ulid.ULID]map[string]string),
		systemAliases: make(map[string]string),
	}
}

// LoadSystemAliases bulk loads system aliases at startup.
func (c *AliasCache) LoadSystemAliases(aliases map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	maps.Copy(c.systemAliases, aliases)
}

// LoadPlayerAliases loads a player's aliases when their session is established.
func (c *AliasCache) LoadPlayerAliases(playerID ulid.ULID, aliases map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.playerAliases[playerID] == nil {
		c.playerAliases[playerID] = make(map[string]string)
	}

	maps.Copy(c.playerAliases[playerID], aliases)
}

// SetSystemAlias adds or updates a single system alias.
// Returns an error if the alias would create a circular reference.
func (c *AliasCache) SetSystemAlias(alias, command string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Temporarily set the alias to check for circularity
	oldCmd, existed := c.systemAliases[alias]
	c.systemAliases[alias] = command

	if c.wouldBeCircularLocked(ulid.ULID{}, alias) {
		// Restore previous state
		if existed {
			c.systemAliases[alias] = oldCmd
		} else {
			delete(c.systemAliases, alias)
		}
		return ErrCircularAlias(alias)
	}

	return nil
}

// SetPlayerAlias adds or updates a single player alias.
// Returns an error if the alias would create a circular reference.
func (c *AliasCache) SetPlayerAlias(playerID ulid.ULID, alias, command string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.playerAliases[playerID] == nil {
		c.playerAliases[playerID] = make(map[string]string)
	}

	// Temporarily set the alias to check for circularity
	oldCmd, existed := c.playerAliases[playerID][alias]
	c.playerAliases[playerID][alias] = command

	if c.wouldBeCircularLocked(playerID, alias) {
		// Restore previous state
		if existed {
			c.playerAliases[playerID][alias] = oldCmd
		} else {
			delete(c.playerAliases[playerID], alias)
		}
		return ErrCircularAlias(alias)
	}

	return nil
}

// RemoveSystemAlias removes a system alias.
func (c *AliasCache) RemoveSystemAlias(alias string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.systemAliases, alias)
}

// RemovePlayerAlias removes a player alias.
func (c *AliasCache) RemovePlayerAlias(playerID ulid.ULID, alias string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.playerAliases[playerID] != nil {
		delete(c.playerAliases[playerID], alias)
	}
}

// wouldBeCircularLocked checks if an alias would create a circular reference.
// Must be called with Lock held (not RLock) since it's used during mutation.
func (c *AliasCache) wouldBeCircularLocked(playerID ulid.ULID, alias string) bool {
	// Track the expansion chain
	_, expanded := c.resolveWithDepth(playerID, alias, 0)
	// If we expanded at all and hit the depth limit, it's circular
	// We check by seeing if resolving the alias leads back through a long chain
	cmd := alias
	for depth := 0; depth < MaxExpansionDepth; depth++ {
		// Check player alias first
		if playerAliases, ok := c.playerAliases[playerID]; ok {
			if next, ok := playerAliases[cmd]; ok {
				nextFirst, _ := splitFirstWord(next)
				if nextFirst == "" {
					return false
				}
				cmd = nextFirst
				continue
			}
		}
		// Check system alias
		if next, ok := c.systemAliases[cmd]; ok {
			nextFirst, _ := splitFirstWord(next)
			if nextFirst == "" {
				return false
			}
			cmd = nextFirst
			continue
		}
		// No more aliases - not circular
		return false
	}
	// Hit depth limit - circular or too long
	return expanded
}

// ClearPlayer removes all aliases for a player (typically on session termination).
func (c *AliasCache) ClearPlayer(playerID ulid.ULID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.playerAliases, playerID)
}

// Resolve expands an input string through alias resolution.
// Resolution order:
// 1. Check if input matches a registered command name → return unchanged
// 2. Check player aliases for the given playerID
// 3. Check system aliases
// 4. No match → return original input unchanged
//
// Returns the resolved string and whether an alias was expanded.
func (c *AliasCache) Resolve(playerID ulid.ULID, input string, registry *Registry) (resolved string, wasAlias bool) {
	if input == "" {
		return input, false
	}

	// Extract the first word (command) and any remaining args
	firstWord, args := splitFirstWord(input)
	if firstWord == "" {
		return input, false
	}

	// Step 1: Check if first word is a registered command
	if registry != nil {
		if _, ok := registry.Get(firstWord); ok {
			return input, false
		}
	}

	// Resolve with depth tracking to prevent circular aliases
	c.mu.RLock()
	resolvedCmd, expanded := c.resolveWithDepth(playerID, firstWord, 0)
	c.mu.RUnlock()

	if !expanded {
		return input, false
	}

	// Reassemble with original args
	if args != "" {
		return resolvedCmd + " " + args, true
	}
	return resolvedCmd, true
}

// resolveWithDepth performs alias resolution with depth tracking.
// Must be called with at least RLock held.
func (c *AliasCache) resolveWithDepth(playerID ulid.ULID, cmd string, depth int) (string, bool) {
	if depth >= MaxExpansionDepth {
		return cmd, depth > 0
	}

	// Check player alias first
	if playerAliases, ok := c.playerAliases[playerID]; ok {
		if expanded, ok := playerAliases[cmd]; ok {
			// Recursively resolve the expanded command's first word
			expandedFirst, expandedArgs := splitFirstWord(expanded)
			if expandedFirst != "" {
				furtherResolved, _ := c.resolveWithDepth(playerID, expandedFirst, depth+1)
				if expandedArgs != "" {
					return furtherResolved + " " + expandedArgs, true
				}
				return furtherResolved, true
			}
			return expanded, true
		}
	}

	// Check system alias
	if expanded, ok := c.systemAliases[cmd]; ok {
		// Recursively resolve the expanded command's first word
		expandedFirst, expandedArgs := splitFirstWord(expanded)
		if expandedFirst != "" {
			furtherResolved, _ := c.resolveWithDepth(playerID, expandedFirst, depth+1)
			if expandedArgs != "" {
				return furtherResolved + " " + expandedArgs, true
			}
			return furtherResolved, true
		}
		return expanded, true
	}

	// No alias found
	return cmd, depth > 0
}

// splitFirstWord splits input into the first word and remaining args.
func splitFirstWord(input string) (first, rest string) {
	input = strings.TrimLeft(input, " \t")
	if input == "" {
		return "", ""
	}

	idx := strings.IndexAny(input, " \t")
	if idx == -1 {
		return input, ""
	}

	return input[:idx], strings.TrimLeft(input[idx+1:], " \t")
}
