// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/holomush/holomush/internal/access"
)

var tracer = otel.Tracer("holomush/command")

// Dispatcher handles command parsing, capability checks, and execution.
type Dispatcher struct {
	registry *Registry
	access   access.AccessControl
}

// NewDispatcher creates a new command dispatcher.
func NewDispatcher(registry *Registry, ac access.AccessControl) *Dispatcher {
	return &Dispatcher{
		registry: registry,
		access:   ac,
	}
}

// Dispatch parses and executes a command.
func (d *Dispatcher) Dispatch(ctx context.Context, input string, exec *CommandExecution) (err error) {
	// Parse input
	parsed, err := Parse(input)
	if err != nil {
		return err
	}

	// Start trace span
	ctx, span := tracer.Start(ctx, "command.execute",
		trace.WithAttributes(
			attribute.String("command.name", parsed.Name),
			attribute.String("character.id", exec.CharacterID.String()),
		),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	// Look up command
	entry, ok := d.registry.Get(parsed.Name)
	if !ok {
		err = ErrUnknownCommand(parsed.Name)
		return err
	}

	span.SetAttributes(attribute.String("command.source", entry.Source))

	// Check capabilities
	subject := "char:" + exec.CharacterID.String()
	for _, cap := range entry.Capabilities {
		if !d.access.Check(ctx, subject, "execute", cap) {
			err = ErrPermissionDenied(parsed.Name, cap)
			return err
		}
	}

	// Execute
	exec.Args = parsed.Args
	err = entry.Handler(ctx, exec)
	return err
}
