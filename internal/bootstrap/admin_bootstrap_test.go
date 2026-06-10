// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
)

func TestAdminBootstrapper_Priority(t *testing.T) {
	deps, _, _, _ := makeDeps()
	ab := NewAdminBootstrapper(deps)
	assert.Equal(t, plugins.BootstrapPriorityContent, ab.Priority())
}

func TestAdminBootstrapper_Bootstrap(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*SeedAdminDeps)
		wantErr bool
	}{
		{
			name: "delegates to SeedAdmin and creates admin user",
			setup: func(_ *SeedAdminDeps) {
				t.Setenv("HOLOMUSH_ADMIN_USERNAME", "")
				t.Setenv("HOLOMUSH_ADMIN_PASSWORD", "")
				t.Setenv("HOLOMUSH_ADMIN_CHARACTER", "")
			},
			wantErr: false,
		},
		{
			name: "skips when players already exist",
			setup: func(deps *SeedAdminDeps) {
				playerRepo := deps.PlayerRepo.(*fakePlayerRepo)
				playerRepo.count = 1
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, _, _, _ := makeDeps()
			tt.setup(&deps)

			ab := NewAdminBootstrapper(deps)
			err := ab.Bootstrap(context.Background(), nil, "")

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
