// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
)

func TestPolicyBootstrapper_Priority(t *testing.T) {
	pb := NewPolicyBootstrapper(func(_ context.Context, _ bool) error {
		return nil
	}, false)
	assert.Equal(t, plugins.BootstrapPriorityPolicy, pb.Priority())
}

func TestPolicyBootstrapper_Bootstrap(t *testing.T) {
	tests := []struct {
		name               string
		bootstrapFn        func(ctx context.Context, skipSeedMigrations bool) error
		skipSeedMigrations bool
		wantErr            bool
		errMsg             string
	}{
		{
			name: "delegates to wrapped function with skipSeedMigrations=false",
			bootstrapFn: func(_ context.Context, skipSeedMigrations bool) error {
				assert.False(t, skipSeedMigrations)
				return nil
			},
			skipSeedMigrations: false,
			wantErr:            false,
		},
		{
			name: "delegates to wrapped function with skipSeedMigrations=true",
			bootstrapFn: func(_ context.Context, skipSeedMigrations bool) error {
				assert.True(t, skipSeedMigrations)
				return nil
			},
			skipSeedMigrations: true,
			wantErr:            false,
		},
		{
			name: "propagates error from wrapped function",
			bootstrapFn: func(_ context.Context, _ bool) error {
				return errors.New("policy bootstrap failed")
			},
			skipSeedMigrations: false,
			wantErr:            true,
			errMsg:             "policy bootstrap failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := NewPolicyBootstrapper(tt.bootstrapFn, tt.skipSeedMigrations)

			err := pb.Bootstrap(context.Background(), nil, "")
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

