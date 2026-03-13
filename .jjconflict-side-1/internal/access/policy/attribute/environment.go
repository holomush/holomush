// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"strings"
	"time"

	"github.com/holomush/holomush/internal/access/policy/types"
)

// environmentProvider provides environment-level attributes for policy evaluation.
type environmentProvider struct {
	clock func() time.Time
}

// NewEnvironmentProvider creates a new environment attribute provider.
// If clock is nil, time.Now is used.
func NewEnvironmentProvider(clock func() time.Time) EnvironmentProvider {
	if clock == nil {
		clock = time.Now
	}
	return &environmentProvider{clock: clock}
}

// Namespace returns the namespace for environment attributes.
func (p *environmentProvider) Namespace() string {
	return "env"
}

// Resolve returns environment attributes for the current context.
func (p *environmentProvider) Resolve(_ context.Context) (map[string]any, error) {
	now := p.clock()

	return map[string]any{
		"time":        now.Format(time.RFC3339),
		"hour":        float64(now.Hour()),
		"minute":      float64(now.Minute()),
		"day_of_week": strings.ToLower(now.Weekday().String()),
		"maintenance": false,
	}, nil
}

// Schema returns the schema for environment attributes.
func (p *environmentProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"time":        types.AttrTypeString,
			"hour":        types.AttrTypeFloat,
			"minute":      types.AttrTypeFloat,
			"day_of_week": types.AttrTypeString,
			"maintenance": types.AttrTypeBool,
		},
	}
}
