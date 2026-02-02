// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"

	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/holomush/holomush/internal/access"
)

var tracer = otel.Tracer("holomush/command")

// Dispatcher handles command parsing, capability checks, and execution.
type Dispatcher struct {
	registry   *Registry
	access     access.AccessControl
	aliasCache *AliasCache // optional, can be nil
}

// NewDispatcher creates a new command dispatcher.
func NewDispatcher(registry *Registry, ac access.AccessControl) *Dispatcher {
	return &Dispatcher{
		registry: registry,
		access:   ac,
	}
}

// SetAliasCache configures the dispatcher to resolve aliases.
// If nil, alias resolution is disabled.
func (d *Dispatcher) SetAliasCache(cache *AliasCache) {
	d.aliasCache = cache
}

// Dispatch parses and executes a command.
func (d *Dispatcher) Dispatch(ctx context.Context, input string, exec *CommandExecution) (err error) {
	// Validate execution context - commands require a character
	if exec.CharacterID.Compare(ulid.ULID{}) == 0 {
		return ErrNoCharacter()
	}

	// Parse original input to capture the invoked command name before alias resolution
	originalParsed, err := Parse(input)
	if err != nil {
		return err
	}
	invokedAs := originalParsed.Name

	// Resolve aliases if cache is configured
	resolvedInput := input
	aliasResult := AliasResult{}
	if d.aliasCache != nil {
		aliasResult = d.aliasCache.Resolve(exec.PlayerID, input, d.registry)
		resolvedInput = aliasResult.Resolved
		// If an alias was used, set InvokedAs to the actual alias (not the parsed word)
		if aliasResult.WasAlias && aliasResult.AliasUsed != "" {
			invokedAs = aliasResult.AliasUsed
		}
	}

	// Parse resolved input
	parsed, err := Parse(resolvedInput)
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

	// Add alias attribute to span if alias was expanded
	if aliasResult.WasAlias {
		span.SetAttributes(attribute.Bool("command.alias_expanded", true))
		span.SetAttributes(attribute.String("command.original_input", input))
		span.SetAttributes(attribute.String("command.alias_used", aliasResult.AliasUsed))
	}

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
	exec.InvokedAs = invokedAs
	err = entry.Handler(ctx, exec)
	return err
}
