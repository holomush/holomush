// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

var tracer = otel.Tracer("holomush/command")

// PluginCommandDeliverer routes commands to the correct plugin host.
// PluginManager implements this interface.
type PluginCommandDeliverer interface {
	DeliverCommand(ctx context.Context, pluginName string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error)
}

// Dispatcher handles command parsing, capability checks, and execution.
type Dispatcher struct {
	registry        *Registry
	engine          types.AccessPolicyEngine
	aliasCache      *AliasCache              // optional, can be nil
	rateLimiter     *RateLimitMiddleware      // optional, can be nil
	pluginDeliverer PluginCommandDeliverer   // optional, can be nil
	optErr          error                    // error from applying options
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

// WithPluginDeliverer configures the dispatcher to route plugin-backed commands
// through the given deliverer (typically the PluginManager).
func WithPluginDeliverer(pd PluginCommandDeliverer) DispatcherOption {
	return func(d *Dispatcher) {
		d.pluginDeliverer = pd
	}
}

// WithRateLimiter configures the dispatcher to use rate limiting.
// If not provided, rate limiting is disabled. Passing nil is an error —
// omit the option entirely to disable rate limiting.
func WithRateLimiter(rl *RateLimiter) DispatcherOption {
	return func(d *Dispatcher) {
		if rl == nil {
			d.optErr = ErrNilRateLimiter
			return
		}
		middleware, err := NewRateLimitMiddleware(rl, d.engine)
		if err != nil {
			d.optErr = err
			return
		}
		d.rateLimiter = middleware
	}
}

// NewDispatcher creates a new command dispatcher with the given registry
// and policy engine. Returns an error if registry or engine is nil.
func NewDispatcher(registry *Registry, engine types.AccessPolicyEngine, opts ...DispatcherOption) (*Dispatcher, error) {
	if registry == nil {
		return nil, ErrNilRegistry
	}
	if engine == nil {
		return nil, ErrNilDispatcherEngine
	}
	d := &Dispatcher{
		registry: registry,
		engine:   engine,
	}
	for _, opt := range opts {
		opt(d)
		if d.optErr != nil {
			return nil, d.optErr
		}
	}
	return d, nil
}

// Dispatch parses and executes a command.
func (d *Dispatcher) Dispatch(ctx context.Context, input string, exec *CommandExecution) (err error) {
	metrics := NewMetricsRecorder()
	defer metrics.Record()

	// Validate execution context - commands require a character
	if exec.CharacterID().Compare(ulid.ULID{}) == 0 {
		return ErrNoCharacter()
	}

	// Validate Services is non-nil to prevent handler panics
	if exec.Services() == nil {
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
	aliasResult := NoAliasResult(input)
	if d.aliasCache != nil {
		aliasResult = d.aliasCache.Resolve(exec.PlayerID(), input, d.registry)
		resolvedInput = aliasResult.Resolved
		// If an alias was used, set InvokedAs to the actual alias (not the parsed word)
		if aliasResult.WasAlias && aliasResult.AliasUsed != "" {
			invokedAs = aliasResult.AliasUsed
			// Record alias expansion metric
			RecordAliasExpansion(aliasResult.AliasUsed)
		}
	}

	// Parse resolved input
	parsed, err := Parse(resolvedInput)
	if err != nil {
		return err
	}

	// Set command name for metrics (now we know it)
	metrics.SetCommandName(parsed.Name)

	// Start trace span
	ctx, span := tracer.Start(ctx, "command.execute",
		trace.WithAttributes(
			attribute.String("command.name", parsed.Name),
			attribute.String("character.id", exec.CharacterID().String()),
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
	subject := access.CharacterSubject(exec.CharacterID().String())
	if d.rateLimiter != nil {
		if rateErr := d.rateLimiter.Enforce(ctx, exec, parsed.Name, span); rateErr != nil {
			metrics.SetStatus(StatusRateLimited)
			return rateErr
		}
	}

	// Look up command
	entry, ok := d.registry.Get(parsed.Name)
	if !ok {
		metrics.SetStatus(StatusNotFound)
		err = ErrUnknownCommand(parsed.Name)
		return err
	}

	// Set source for metrics (now we know it)
	metrics.SetCommandSource(entry.Source)
	span.SetAttributes(attribute.String("command.source", entry.Source))

	// Check capabilities using getter to ensure defensive copy
	for _, cap := range entry.GetCapabilities() {
		capErr := CheckCapability(ctx, d.engine, subject, cap, parsed.Name)
		if capErr == nil {
			continue
		}
		oopsErr, ok := oops.AsOops(capErr)
		code, isStr := oopsErr.Code().(string)
		if ok && isStr && code == CodePermissionDenied {
			metrics.SetStatus(StatusPermissionDenied)
		} else {
			metrics.SetStatus(StatusEngineFailure)
			span.RecordError(capErr)
			span.SetStatus(codes.Error, capErr.Error())
		}
		return capErr
	}

	// Execute
	exec.Args = parsed.Args
	exec.InvokedAs = invokedAs

	// Route: plugin-backed commands go through PluginManager, compiled-in commands call handler directly.
	isPlugin := entry.PluginName() != ""
	if isPlugin {
		// Plugin commands: dispatchToPlugin sets metrics and session activity
		// based on the CommandStatus returned by the handler.
		err = d.dispatchToPlugin(ctx, &entry, exec, invokedAs, metrics, span)
	} else {
		err = entry.Handler()(ctx, exec)
	}

	if err != nil {
		// ErrSessionEnded is a graceful signal, not a failure — preserve the
		// metrics status that dispatchToPlugin already set (success).
		if !errors.Is(err, ErrSessionEnded) {
			metrics.SetStatus(StatusError)
		}
		slog.WarnContext(ctx, "command execution failed",
			"command", parsed.Name,
			"character_id", exec.CharacterID().String(),
			"error", err,
		)
	} else if !isPlugin {
		// Non-plugin commands: set success metrics and bump activity.
		metrics.SetStatus(StatusSuccess)
		// Bump session activity timestamp so "who" idle time is accurate.
		if sid := exec.SessionID(); sid.Compare(ulid.ULID{}) != 0 {
			if actErr := exec.Services().Session().UpdateActivity(ctx, sid.String()); actErr != nil {
				slog.WarnContext(ctx, "session activity update failed",
					"session_id", sid.String(), "error", actErr)
			}
		}
	}
	return err
}

// dispatchToPlugin routes a command through the PluginManager and processes the response.
func (d *Dispatcher) dispatchToPlugin(ctx context.Context, entry *CommandEntry, exec *CommandExecution, invokedAs string, metrics *MetricsRecorder, span trace.Span) error {
	if d.pluginDeliverer == nil {
		return oops.Code("NO_PLUGIN_DELIVERER").
			With("command", entry.Name).
			With("plugin", entry.PluginName()).
			Errorf("command is plugin-backed but no PluginCommandDeliverer configured")
	}

	cmd := pluginsdk.CommandRequest{
		Command:       entry.Name,
		Args:          exec.Args,
		CharacterID:   exec.CharacterID().String(),
		CharacterName: exec.CharacterName(),
		LocationID:    exec.LocationID().String(),
		SessionID:     exec.SessionID().String(),
		InvokedAs:     invokedAs,
	}

	span.SetAttributes(
		attribute.String("command.plugin", entry.PluginName()),
		attribute.Bool("command.plugin_routed", true),
	)

	resp, err := d.pluginDeliverer.DeliverCommand(ctx, entry.PluginName(), cmd)
	if err != nil {
		return oops.In("dispatcher").With("command", entry.Name).With("plugin", entry.PluginName()).Wrap(err)
	}
	if resp == nil {
		return nil
	}

	// Handle CommandStatus: set metrics, activity, error flags based on outcome.
	switch resp.Status {
	case pluginsdk.CommandOK:
		metrics.SetStatus(StatusSuccess)
		// Bump session activity — normal successful command.
		if sid := exec.SessionID(); sid.Compare(ulid.ULID{}) != 0 {
			if actErr := exec.Services().Session().UpdateActivity(ctx, sid.String()); actErr != nil {
				slog.WarnContext(ctx, "session activity update failed",
					"session_id", sid.String(), "error", actErr)
			}
		}

	case pluginsdk.CommandError:
		// User errors are normal — count as success for metrics.
		metrics.SetStatus(StatusSuccess)
		exec.SetResponseIsError(true)
		// Bump session activity — user is still active.
		if sid := exec.SessionID(); sid.Compare(ulid.ULID{}) != 0 {
			if actErr := exec.Services().Session().UpdateActivity(ctx, sid.String()); actErr != nil {
				slog.WarnContext(ctx, "session activity update failed",
					"session_id", sid.String(), "error", actErr)
			}
		}

	case pluginsdk.CommandFailure:
		// Service degraded — count as error for metrics.
		metrics.SetStatus(StatusError)
		exec.SetResponseIsError(true)
		span.RecordError(oops.Errorf("plugin command service failure: %s", entry.Name))
		span.SetStatus(codes.Error, "service failure")
		// Do NOT update session activity — not a real user action.

	case pluginsdk.CommandFatal:
		metrics.SetStatus(StatusError)
		exec.SetResponseIsError(true)
		span.RecordError(oops.Errorf("plugin command fatal: %s", entry.Name))
		span.SetStatus(codes.Error, "fatal")
		return oops.In("dispatcher").With("command", entry.Name).With("plugin", entry.PluginName()).
			Errorf("plugin command returned fatal status")

	default:
		return oops.In("dispatcher").With("command", entry.Name).With("plugin", entry.PluginName()).
			With("status", resp.Status).Errorf("plugin command returned unknown status")
	}

	// Process response events: emit each to the event store.
	if exec.Services() != nil && exec.Services().Events() != nil {
		for _, evt := range resp.Events {
			emitEvent := core.Event{
				ID:        core.NewULID(),
				Stream:    evt.Stream,
				Type:      core.EventType(evt.Type),
				Timestamp: time.Now(),
				Actor: core.Actor{
					Kind: core.ActorCharacter,
					ID:   exec.CharacterID().String(),
				},
				Payload: []byte(evt.Payload),
			}
			if appendErr := exec.Services().Events().Append(ctx, emitEvent); appendErr != nil {
				return oops.In("dispatcher").
					With("command", entry.Name).
					With("stream", evt.Stream).
					Wrap(appendErr)
			}
		}
	}

	// Process synchronous output: write to exec.Output().
	if resp.Output != "" && exec.Output() != nil {
		if _, writeErr := exec.Output().Write([]byte(resp.Output)); writeErr != nil {
			slog.WarnContext(ctx, "failed to write plugin command output",
				"command", entry.Name,
				"error", writeErr,
			)
		}
	}

	// Process booted sessions: record each so the server layer can emit leave
	// events and run disconnect hooks.
	for _, sid := range resp.BootedSessions {
		sessID, parseErr := ulid.Parse(sid)
		if parseErr != nil {
			slog.WarnContext(ctx, "invalid booted session ID from plugin",
				"command", entry.Name, "session_id", sid, "error", parseErr)
			continue
		}
		exec.RecordBootedSession(BootedSession{
			CharacterRef: core.CharacterRef{},
			SessionInfo:  session.Info{ID: sessID.String()},
		})
	}

	// Process EndSession: signal that the invoking session should end.
	if resp.EndSession {
		exec.SetEndSession(true)
		return oops.Code("SESSION_ENDED").Wrap(ErrSessionEnded)
	}

	return nil
}
