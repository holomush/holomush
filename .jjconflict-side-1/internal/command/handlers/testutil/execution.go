// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package testutil

import (
	"bytes"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/world"
)

// ExecutionBuilder builds CommandExecution instances with an attached output buffer.
type ExecutionBuilder struct {
	cfg    command.CommandExecutionConfig
	output *bytes.Buffer
}

// NewExecutionBuilder creates a new execution builder.
func NewExecutionBuilder() *ExecutionBuilder {
	return &ExecutionBuilder{}
}

// WithCharacter sets the character identity fields.
func (b *ExecutionBuilder) WithCharacter(player PlayerContext) *ExecutionBuilder {
	b.cfg.CharacterID = player.CharacterID
	b.cfg.CharacterName = player.Name
	b.cfg.PlayerID = player.PlayerID
	return b
}

// WithLocation sets the location from a Location struct.
func (b *ExecutionBuilder) WithLocation(location *world.Location) *ExecutionBuilder {
	if location != nil {
		b.cfg.LocationID = location.ID
	}
	return b
}

// WithLocationID sets the location ID directly.
func (b *ExecutionBuilder) WithLocationID(locationID ulid.ULID) *ExecutionBuilder {
	b.cfg.LocationID = locationID
	return b
}

// WithArgs sets the command arguments.
func (b *ExecutionBuilder) WithArgs(args string) *ExecutionBuilder {
	b.cfg.Args = args
	return b
}

// WithServices sets the services dependency.
func (b *ExecutionBuilder) WithServices(services *command.Services) *ExecutionBuilder {
	b.cfg.Services = services
	return b
}

// WithOutput sets the output buffer.
func (b *ExecutionBuilder) WithOutput(output *bytes.Buffer) *ExecutionBuilder {
	b.output = output
	b.cfg.Output = output
	return b
}

// Build creates the configured CommandExecution and output buffer.
func (b *ExecutionBuilder) Build() (*command.CommandExecution, *bytes.Buffer) {
	if b.cfg.Output == nil {
		b.output = &bytes.Buffer{}
		b.cfg.Output = b.output
	}
	return command.NewTestExecution(b.cfg), b.output
}
