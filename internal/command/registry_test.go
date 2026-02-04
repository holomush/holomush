// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noopHandler is a test helper that does nothing.
func noopHandler(_ context.Context, _ *CommandExecution) error {
	return nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()

	handler := func(_ context.Context, _ *CommandExecution) error {
		return nil
	}

	entry := CommandEntry{
		Name:         "look",
		Handler:      handler,
		Capabilities: []string{"world.look"},
		Help:         "Look at your surroundings",
		Usage:        "look [target]",
		Source:       "core",
	}

	err := reg.Register(entry)
	require.NoError(t, err)

	got, ok := reg.Get("look")
	assert.True(t, ok)
	assert.Equal(t, "look", got.Name)
	assert.Equal(t, []string{"world.look"}, got.Capabilities)
	assert.Equal(t, "Look at your surroundings", got.Help)
	assert.Equal(t, "look [target]", got.Usage)
	assert.Equal(t, "core", got.Source)
}

func TestRegistry_GetNotFound(t *testing.T) {
	reg := NewRegistry()
	_, ok := reg.Get("nonexistent")
	assert.False(t, ok)
}

func TestRegistry_All(t *testing.T) {
	reg := NewRegistry()

	_ = reg.Register(CommandEntry{Name: "look", Handler: noopHandler, Source: "core"})
	_ = reg.Register(CommandEntry{Name: "say", Handler: noopHandler, Source: "comms"})

	all := reg.All()
	assert.Len(t, all, 2)

	// Verify both commands are present (order may vary due to map iteration)
	names := make(map[string]bool)
	for _, e := range all {
		names[e.Name] = true
	}
	assert.True(t, names["look"])
	assert.True(t, names["say"])
}

func TestRegistry_AllEmpty(t *testing.T) {
	reg := NewRegistry()
	all := reg.All()
	assert.Empty(t, all)
	assert.NotNil(t, all) // Should return empty slice, not nil
}

func TestRegistry_ConflictWarning(t *testing.T) {
	reg := NewRegistry()

	_ = reg.Register(CommandEntry{Name: "look", Handler: noopHandler, Source: "core"})
	err := reg.Register(CommandEntry{Name: "look", Handler: noopHandler, Source: "plugin-a"})

	// Should succeed but we can check it overwrote
	require.NoError(t, err)
	got, _ := reg.Get("look")
	assert.Equal(t, "plugin-a", got.Source)
}

func TestRegistry_ConflictWarning_LogOutput(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, nil)
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	reg := NewRegistry()
	_ = reg.Register(CommandEntry{Name: "testcmd", Handler: noopHandler, Source: "core"})
	_ = reg.Register(CommandEntry{Name: "testcmd", Handler: noopHandler, Source: "plugin-override"})

	logOutput := buf.String()
	assert.Contains(t, logOutput, "command conflict: overwriting existing command")
	assert.Contains(t, logOutput, "testcmd")
	assert.Contains(t, logOutput, "previous_source")
	assert.Contains(t, logOutput, "core")
	assert.Contains(t, logOutput, "new_source")
	assert.Contains(t, logOutput, "plugin-override")
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewRegistry()

	// Pre-populate with some commands
	for i := range 10 {
		_ = reg.Register(CommandEntry{Name: "cmd" + string(rune('a'+i)), Handler: noopHandler, Source: "test"})
	}

	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 100

	// Concurrent reads and writes
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range iterations {
				if j%2 == 0 {
					// Read operation
					_, _ = reg.Get("cmda")
					_ = reg.All()
				} else {
					// Write operation
					_ = reg.Register(CommandEntry{
						Name:    "concurrent",
						Handler: noopHandler,
						Source:  "goroutine",
					})
				}
			}
		}()
	}

	wg.Wait()

	// Registry should still be in a consistent state
	_, ok := reg.Get("concurrent")
	assert.True(t, ok)
}

func TestRegistry_AllReturnsCopy(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(CommandEntry{Name: "look", Handler: noopHandler, Source: "core"})

	all1 := reg.All()
	all2 := reg.All()

	// Modifying one slice should not affect the other
	if len(all1) > 0 {
		all1[0].Name = "modified"
	}

	all3 := reg.All()
	assert.Equal(t, "look", all3[0].Name)
	assert.NotEqual(t, all1[0].Name, all2[0].Name) // all1 was modified, all2 was not
}

func TestRegistry_Register_EmptyName(t *testing.T) {
	reg := NewRegistry()

	err := reg.Register(CommandEntry{
		Name:    "",
		Handler: noopHandler,
		Source:  "core",
	})

	assert.ErrorIs(t, err, ErrEmptyCommandName)
}

func TestRegistry_Register_NilHandler(t *testing.T) {
	reg := NewRegistry()

	err := reg.Register(CommandEntry{
		Name:    "test",
		Handler: nil,
		Source:  "core",
	})

	assert.ErrorIs(t, err, ErrNilHandler)
}

func TestRegistry_Register_InvalidName(t *testing.T) {
	reg := NewRegistry()

	tests := []struct {
		name        string
		commandName string
		wantErr     bool
	}{
		{
			name:        "starts with digit",
			commandName: "1test",
			wantErr:     true,
		},
		{
			name:        "contains space",
			commandName: "te st",
			wantErr:     true,
		},
		{
			name:        "too long",
			commandName: "abcdefghijklmnopqrstuvwxyz",
			wantErr:     true,
		},
		{
			name:        "valid name",
			commandName: "test",
			wantErr:     false,
		},
		{
			name:        "valid with special chars",
			commandName: "test!",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := reg.Register(CommandEntry{
				Name:    tt.commandName,
				Handler: noopHandler,
				Source:  "test",
			})

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "name")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
