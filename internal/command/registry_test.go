// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	_ = reg.Register(CommandEntry{Name: "look", Source: "core"})
	_ = reg.Register(CommandEntry{Name: "say", Source: "comms"})

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

	_ = reg.Register(CommandEntry{Name: "look", Source: "core"})
	err := reg.Register(CommandEntry{Name: "look", Source: "plugin-a"})

	// Should succeed but we can check it overwrote
	require.NoError(t, err)
	got, _ := reg.Get("look")
	assert.Equal(t, "plugin-a", got.Source)
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewRegistry()

	// Pre-populate with some commands
	for i := 0; i < 10; i++ {
		_ = reg.Register(CommandEntry{Name: "cmd" + string(rune('a'+i)), Source: "test"})
	}

	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 100

	// Concurrent reads and writes
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if j%2 == 0 {
					// Read operation
					_, _ = reg.Get("cmda")
					_ = reg.All()
				} else {
					// Write operation
					_ = reg.Register(CommandEntry{
						Name:   "concurrent",
						Source: "goroutine",
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
	_ = reg.Register(CommandEntry{Name: "look", Source: "core"})

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
