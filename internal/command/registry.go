// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"log/slog"
	"sync"
)

// Registry manages command registration and lookup.
// Registry is safe for concurrent use. All methods are protected by a
// sync.RWMutex, allowing concurrent reads (Get, All) with exclusive writes
// (Register).
type Registry struct {
	commands map[string]CommandEntry
	mu       sync.RWMutex
}

// NewRegistry creates a new command registry.
func NewRegistry() *Registry {
	return &Registry{
		commands: make(map[string]CommandEntry),
	}
}

// Register adds a command to the registry. It is safe for concurrent use.
// If a command with the same name exists, it is overwritten and a warning is logged.
// This follows ADR 0006 and ADR 0008: last-loaded wins with warning.
//
// Returns an error if the command entry is invalid (empty name, invalid name format, or nil handler).
func (r *Registry) Register(entry CommandEntry) error {
	if entry.Name == "" {
		return ErrEmptyCommandName
	}
	if err := ValidateCommandName(entry.Name); err != nil {
		return err
	}
	if entry.Handler == nil {
		return ErrNilHandler
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.commands[entry.Name]; ok {
		slog.Warn("command conflict: overwriting existing command",
			"command", entry.Name,
			"previous_source", existing.Source,
			"new_source", entry.Source)
	}

	r.commands[entry.Name] = entry
	return nil
}

// Get retrieves a command by name. It is safe for concurrent use.
// Returns the command entry and true if found, or zero value and false if not found.
func (r *Registry) Get(name string) (CommandEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.commands[name]
	return entry, ok
}

// All returns all registered commands. It is safe for concurrent use.
// The returned slice is a defensive copy and safe to modify without
// affecting the registry's internal state.
func (r *Registry) All() []CommandEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := make([]CommandEntry, 0, len(r.commands))
	for _, e := range r.commands {
		entries = append(entries, e)
	}
	return entries
}
