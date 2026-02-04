// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"log/slog"
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
//
// This method trusts the input data and does NOT validate for circular references.
// Callers are expected to provide pre-validated data, typically loaded from the
// database where [SetSystemAlias] already enforced circularity checks before storage.
//
// If you need to validate untrusted alias data (e.g., from a migration or manual
// import), use [ValidateAliasSet] first.
func (c *AliasCache) LoadSystemAliases(aliases map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	maps.Copy(c.systemAliases, aliases)
}

// LoadPlayerAliases loads a player's aliases when their session is established.
//
// This method trusts the input data and does NOT validate for circular references.
// Callers are expected to provide pre-validated data, typically loaded from the
// database where [SetPlayerAlias] already enforced circularity checks before storage.
//
// If you need to validate untrusted alias data (e.g., from a migration or manual
// import), use [ValidateAliasSet] first.
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
		slog.Debug("circular system alias rejected",
			slog.String("alias", alias),
			slog.String("command", command),
		)
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
		slog.Debug("circular player alias rejected",
			slog.String("alias", alias),
			slog.String("command", command),
			slog.String("player_id", playerID.String()),
		)
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
//
// Algorithm: Uses a visited set to detect true cycles. This distinguishes between:
//   - Circular references (A→B→C→A): Detected by revisiting a node already in visited set
//   - Deep chains (A→B→C→...→Z): Valid as long as no node repeats
//
// The depth limit (MaxExpansionDepth) serves as a safety bound to prevent runaway
// expansion, not as a cycle detection mechanism.
func (c *AliasCache) wouldBeCircularLocked(playerID ulid.ULID, alias string) bool {
	visited := make(map[string]bool)
	cmd := alias

	for depth := 0; depth < MaxExpansionDepth; depth++ {
		// Check for cycle: if we've seen this command before, it's circular
		if visited[cmd] {
			return true
		}
		visited[cmd] = true

		// Check player alias first (player aliases override system aliases)
		if playerAliases, ok := c.playerAliases[playerID]; ok {
			if next, ok := playerAliases[cmd]; ok {
				nextFirst, _ := splitFirstWord(next)
				if nextFirst == "" {
					// Empty expansion - chain terminates
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
				// Empty expansion - chain terminates
				return false
			}
			cmd = nextFirst
			continue
		}

		// No more aliases - chain terminates at a real command (or unknown)
		return false
	}

	// Hit depth limit without finding a cycle or termination.
	// This is a safety bound - the chain is too deep but not necessarily circular.
	// We treat this as "circular" to prevent potential DoS from extremely deep chains.
	return true
}

// ClearPlayer removes all aliases for a player (typically on session termination).
func (c *AliasCache) ClearPlayer(playerID ulid.ULID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.playerAliases, playerID)
}

// GetPlayerAlias returns a player's alias and whether it exists.
func (c *AliasCache) GetPlayerAlias(playerID ulid.ULID, alias string) (command string, exists bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if aliases, ok := c.playerAliases[playerID]; ok {
		command, exists = aliases[alias]
	}
	return command, exists
}

// GetSystemAlias returns a system alias and whether it exists.
func (c *AliasCache) GetSystemAlias(alias string) (command string, exists bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	command, exists = c.systemAliases[alias]
	return command, exists
}

// ListPlayerAliases returns a copy of all aliases for a player.
// Returns an empty map if the player has no aliases.
func (c *AliasCache) ListPlayerAliases(playerID ulid.ULID) map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]string)
	if aliases, ok := c.playerAliases[playerID]; ok {
		maps.Copy(result, aliases)
	}
	return result
}

// ListSystemAliases returns a copy of all system aliases.
func (c *AliasCache) ListSystemAliases() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]string)
	maps.Copy(result, c.systemAliases)
	return result
}

// ShadowsCommand checks if the given alias name matches a registered command.
func (c *AliasCache) ShadowsCommand(alias string, registry *Registry) bool {
	if registry == nil {
		return false
	}
	_, exists := registry.Get(alias)
	return exists
}

// ShadowsSystemAlias checks if the given alias shadows an existing system alias.
// Returns the command the system alias expands to and whether it shadows.
func (c *AliasCache) ShadowsSystemAlias(alias string) (command string, shadows bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	command, shadows = c.systemAliases[alias]
	return command, shadows
}

// AliasResult contains the result of alias resolution.
type AliasResult struct {
	Resolved  string // The resolved command string
	WasAlias  bool   // Whether an alias was expanded
	AliasUsed string // The alias that was matched (empty if no alias)
}

// aliasLookupResult holds the result of a locked alias lookup.
// This intermediate type allows the Resolve method to perform all map lookups
// under a single RLock, then release the lock before building the final result.
type aliasLookupResult struct {
	resolvedCmd string // The resolved command (or prefix command)
	expanded    bool   // Whether any alias was expanded
	aliasUsed   string // The alias that was matched
	isPrefix    bool   // Whether this was a prefix alias match
	rest        string // For prefix aliases: the text after the prefix
}

// Resolve expands an input string through alias resolution.
//
// Resolution order:
//  1. Check if input matches a registered command name → return unchanged
//  2. Check player aliases for the given playerID
//  3. Check system aliases
//  4. Check single-character prefix aliases (player then system)
//  5. No match → return original input unchanged
//
// Prefix alias semantics: A single-character alias like ":" is treated as a
// prefix when it appears attached to text without whitespace. For example,
// if ":" is aliased to "pose", then ":waves" resolves to "pose waves". The
// first character is the prefix, and the rest becomes an argument. This
// enables MUSH-style shortcuts like ":waves" (pose) or ";" (say variants).
// Prefix matching only occurs when the first word has 2+ characters.
//
// Returns the resolved string, whether an alias was expanded, and which alias was used.
//
// Locking strategy: All map lookups are performed in lookupAliasLocked() under
// a single RLock acquisition. The lock is released before any string building
// or result assembly. This ensures exactly one lock/unlock pair per call and
// makes the locking behavior predictable for future modifications.
func (c *AliasCache) Resolve(playerID ulid.ULID, input string, registry *Registry) AliasResult {
	if input == "" {
		return AliasResult{Resolved: input}
	}

	// Extract the first word (command) and any remaining args
	firstWord, args := splitFirstWord(input)
	if firstWord == "" {
		return AliasResult{Resolved: input}
	}

	// Step 1: Check if first word is a registered command
	if registry != nil {
		if _, ok := registry.Get(firstWord); ok {
			return AliasResult{Resolved: input}
		}
	}

	// Step 2-4: Look up aliases under lock, then build result outside the lock
	lookup := c.lookupAliasLocked(playerID, firstWord)

	if !lookup.expanded {
		return AliasResult{Resolved: input}
	}

	// Build the resolved string (no lock needed)
	parts := []string{lookup.resolvedCmd}
	if lookup.isPrefix {
		parts = append(parts, lookup.rest)
	}
	if args != "" {
		parts = append(parts, args)
	}
	resolved := strings.Join(parts, " ")
	return AliasResult{Resolved: resolved, WasAlias: true, AliasUsed: lookup.aliasUsed}
}

// lookupAliasLocked performs all alias map lookups under a single RLock.
// It checks regular aliases first (player then system), then prefix aliases.
// The lock is acquired at entry and released before return.
func (c *AliasCache) lookupAliasLocked(playerID ulid.ULID, firstWord string) aliasLookupResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Try regular alias resolution first
	resolvedCmd, expanded := c.resolveWithDepth(playerID, firstWord, 0)
	if expanded {
		return aliasLookupResult{
			resolvedCmd: resolvedCmd,
			expanded:    true,
			aliasUsed:   firstWord,
		}
	}

	// Check for single-character prefix aliases.
	// This handles MUSH-style shortcuts like ":waves" or ";'s eyes widen"
	// where the alias is a single character attached to the text.
	if len(firstWord) > 1 {
		prefix := firstWord[:1]
		rest := firstWord[1:]

		// Check player prefix aliases first
		if playerAliases, ok := c.playerAliases[playerID]; ok {
			if prefixCmd, ok := playerAliases[prefix]; ok {
				return aliasLookupResult{
					resolvedCmd: prefixCmd,
					expanded:    true,
					aliasUsed:   prefix,
					isPrefix:    true,
					rest:        rest,
				}
			}
		}

		// Check system prefix aliases
		if prefixCmd, ok := c.systemAliases[prefix]; ok {
			return aliasLookupResult{
				resolvedCmd: prefixCmd,
				expanded:    true,
				aliasUsed:   prefix,
				isPrefix:    true,
				rest:        rest,
			}
		}
	}

	return aliasLookupResult{expanded: false}
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

// ValidateAliasSet checks a set of aliases for circular references without
// modifying the cache. This can be used as a pre-flight check for untrusted
// alias data before calling [LoadSystemAliases] or [LoadPlayerAliases].
//
// The function returns an error describing the first circular reference found,
// or nil if all aliases are valid.
//
// Note: This validates the aliases in isolation. Cross-type cycles (where a
// player alias refers to a system alias that refers back) are detected at
// resolution time via depth limiting, not here.
func ValidateAliasSet(aliases map[string]string) error {
	for alias := range aliases {
		if detectCycleInSet(aliases, alias) {
			return ErrCircularAlias(alias)
		}
	}
	return nil
}

// detectCycleInSet checks if following an alias leads to a cycle within the set.
func detectCycleInSet(aliases map[string]string, start string) bool {
	visited := make(map[string]bool)
	cmd := start

	for depth := 0; depth < MaxExpansionDepth; depth++ {
		if visited[cmd] {
			return true
		}
		visited[cmd] = true

		next, ok := aliases[cmd]
		if !ok {
			// Chain terminates at non-alias
			return false
		}

		// Extract first word from expansion
		nextFirst, _ := splitFirstWord(next)
		if nextFirst == "" {
			return false
		}
		cmd = nextFirst
	}

	// Hit depth limit - treat as circular for safety
	return true
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
