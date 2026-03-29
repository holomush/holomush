// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerbRegistry_Register(t *testing.T) {
	r := NewVerbRegistry()
	err := r.Register(VerbRegistration{
		Type:     "say",
		Category: "communication",
		Format:   "speech",
		Label:    "says",
	})
	require.NoError(t, err)

	reg, ok := r.Lookup("say")
	assert.True(t, ok)
	assert.Equal(t, "communication", reg.Category)
	assert.Equal(t, "speech", reg.Format)
	assert.Equal(t, "says", reg.Label)
}

func TestVerbRegistry_Register_DuplicateRejected(t *testing.T) {
	r := NewVerbRegistry()
	err := r.Register(VerbRegistration{Type: "say", Category: "communication", Format: "speech", Label: "says"})
	require.NoError(t, err)

	err = r.Register(VerbRegistration{Type: "say", Category: "communication", Format: "speech", Label: "says"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestVerbRegistry_Lookup_NotFound(t *testing.T) {
	r := NewVerbRegistry()
	_, ok := r.Lookup("nonexistent")
	assert.False(t, ok)
}

func TestVerbRegistry_Register_SpeechRequiresLabel(t *testing.T) {
	r := NewVerbRegistry()
	err := r.Register(VerbRegistration{Type: "say", Category: "communication", Format: "speech"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "label is required")
}

func TestVerbRegistry_Register_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		reg  VerbRegistration
		want string
	}{
		{"empty type", VerbRegistration{Category: "c", Format: "f"}, "type must not be empty"},
		{"empty category", VerbRegistration{Type: "t", Format: "f"}, "category must not be empty"},
		{"empty format", VerbRegistration{Type: "t", Category: "c"}, "format must not be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewVerbRegistry()
			err := r.Register(tt.reg)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestVerbRegistry_ConcurrentAccess(t *testing.T) {
	r := NewVerbRegistry()
	var wg sync.WaitGroup
	errs := make(chan error, 50)

	// Concurrent writes (different types)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if err := r.Register(VerbRegistration{
				Type:     fmt.Sprintf("type_%d", n),
				Category: "communication",
				Format:   "action",
			}); err != nil {
				errs <- err
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Lookup("type_a")
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("unexpected Register error: %v", err)
	}
}

func TestRegisterBuiltinTypes(t *testing.T) {
	r := NewVerbRegistry()
	err := RegisterBuiltinTypes(r)
	require.NoError(t, err)

	// Verify a few known types.
	reg, ok := r.Lookup("say")
	require.True(t, ok)
	assert.Equal(t, "communication", reg.Category)
	assert.Equal(t, "speech", reg.Format)
	assert.Equal(t, "says", reg.Label)

	reg, ok = r.Lookup("pose")
	require.True(t, ok)
	assert.Equal(t, "action", reg.Format)

	reg, ok = r.Lookup("command_error")
	require.True(t, ok)
	assert.Equal(t, "command", reg.Category)
	assert.Equal(t, "error", reg.Format)

	_, ok = r.Lookup("location_state")
	assert.True(t, ok)
}
