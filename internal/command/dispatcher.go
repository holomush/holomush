// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"log/slog"

	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/observability"
)

var tracer = otel.Tracer("holomush/command")

// Dispatcher handles command parsing, capability checks, and execution.
type Dispatcher struct {
	registry    *Registry
	access      access.AccessControl
	aliasCache  *AliasCache  // optional, can be nil
	rateLimiter *RateLimiter // optional, can be nil
}

// DispatcherOption configures a Dispatcher during construction.
type DispatcherOption func(*Dispatcher)

// WithAliasCache configures the dispatcher to use the given alias cache for
// command resolution. If not provided, alias resolution is disabled.
func WithAliasCache(cache *AliasCache) DispatcherOption {
	return func(d *Dispatcher) {
		d.aliasCache = cache
	}
}

// WithRateLimiter configures the dispatcher to use rate limiting.
// If not provided, rate limiting is disabled.
func WithRateLimiter(rl *RateLimiter) DispatcherOption {
	return func(d *Dispatcher) {
		d.rateLimiter = rl
	}
}

// NewDispatcher creates a new command dispatcher with the given registry
// and access control. Returns an error if registry or ac is nil.
func NewDispatcher(registry *Registry, ac access.AccessControl, opts ...DispatcherOption) (*Dispatcher, error) {
	if registry == nil {
		return nil, ErrNilRegistry
	}
	if ac == nil {
		return nil, ErrNilAccessControl
	}
	d := &Dispatcher{
		registry: registry,
		access:   ac,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d, nil
}

// Dispatch parses and executes a command.
func (d *Dispatcher) Dispatch(ctx context.Context, input string, exec *CommandExecution) (err error) {
	// Validate execution context - commands require a character
	if exec.CharacterID.Compare(ulid.ULID{}) == 0 {
		return ErrNoCharacter()
	}

	// Validate Services is non-nil to prevent handler panics
	if exec.Services == nil {
		return ErrNilServices()
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

	// Apply rate limiting if configured (after alias resolution, before capability check)
	subject := "char:" + exec.CharacterID.String()
	if d.rateLimiter != nil {
		// Check if character has bypass capability
		hasBypass := d.access.Check(ctx, subject, "execute", CapabilityRateLimitBypass)
		if !hasBypass {
			allowed, cooldownMs := d.rateLimiter.Allow(exec.SessionID)
			if !allowed {
				span.SetAttributes(attribute.Bool("command.rate_limited", true))
				span.SetAttributes(attribute.Int64("command.cooldown_ms", cooldownMs))
				observability.RecordCommandRateLimited(parsed.Name)
				err = ErrRateLimited(cooldownMs)
				return err
			}
		}
	}

	// Look up command
	entry, ok := d.registry.Get(parsed.Name)
	if !ok {
		err = ErrUnknownCommand(parsed.Name)
		return err
	}

	span.SetAttributes(attribute.String("command.source", entry.Source))

	// Check capabilities using getter to ensure defensive copy
	for _, cap := range entry.GetCapabilities() {
		if !d.access.Check(ctx, subject, "execute", cap) {
			err = ErrPermissionDenied(parsed.Name, cap)
			return err
		}
	}

	// Execute
	exec.Args = parsed.Args
	exec.InvokedAs = invokedAs
	err = entry.Handler(ctx, exec)
	if err != nil {
		slog.WarnContext(ctx, "command execution failed",
			"command", parsed.Name,
			"character_id", exec.CharacterID.String(),
			"error", err,
		)
	}
	return err
}
