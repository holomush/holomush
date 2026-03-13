// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package plugins provides plugin management and lifecycle control.
package plugins

import (
	"context"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// Host manages a specific plugin runtime type.
type Host interface {
	// Load initializes a plugin from its manifest.
	Load(ctx context.Context, manifest *Manifest, dir string) error

	// Unload tears down a pluginsdk.
	Unload(ctx context.Context, name string) error

	// DeliverEvent sends an event to a plugin and returns response events.
	DeliverEvent(ctx context.Context, name string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error)

	// Plugins returns names of all loaded plugins.
	Plugins() []string

	// Close shuts down the host and all plugins.
	Close(ctx context.Context) error
}
