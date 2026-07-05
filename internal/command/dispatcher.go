// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/observability"
	"github.com/holomush/holomush/internal/session"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

var tracer = otel.Tracer("holomush/command")

// PluginCommandDeliverer routes commands to the correct plugin host.
// PluginManager implements this interface.
type PluginCommandDeliverer interface {
	DeliverCommand(ctx context.Context, pluginName string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error)
	EmitPluginEvent(ctx context.Context, pluginName string, event pluginsdk.EmitEvent) error
}

// Dispatcher handles command parsing, capability checks, and execution.
type Dispatcher struct {
	registry        *Registry
	engine          types.AccessPolicyEngine
	aliasCache      *AliasCache            // optional, can be nil
	rateLimiter     *RateLimitMiddleware   // optional, can be nil
	pluginDeliverer PluginCommandDeliverer // optional, can be nil
	focusReader     FocusReader            // optional, can be nil; enables focus-redirect
	focusRedirects  FocusRedirectTable     // optional, can be nil; verb→kind→target
	auditLogger     *audit.Logger          // optional, can be nil; when nil, plugin-audit flush is skipped
	optErr          error                  // error from applying options
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

// WithFocusReader configures the dispatcher to read a connection's focus kind
// for focus-routed command redirection. If not provided (or nil), the redirect
// is disabled and all commands route normally.
func WithFocusReader(fr FocusReader) DispatcherOption {
	return func(d *Dispatcher) {
		d.focusReader = fr
	}
}

// WithFocusRedirects configures the plugin-declared verb→focus-kind→target
// redirect table. If not provided (or empty), no verb is ever redirected.
func WithFocusRedirects(t FocusRedirectTable) DispatcherOption {
	return func(d *Dispatcher) {
		d.focusRedirects = t
	}
}

// WithAuditLogger configures the dispatcher to flush plugin-emitted audit
// events through the given audit logger. If not provided, plugin audit
// events are silently dropped — useful for tests that do not care about
// audit flow.
func WithAuditLogger(logger *audit.Logger) DispatcherOption {
	return func(d *Dispatcher) {
		d.auditLogger = logger
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
	// WithFocusReader and WithFocusRedirects are independently optional, but
	// maybeRedirectForFocus needs BOTH non-nil to do anything — wiring only
	// one silently disables the whole focus-redirect feature with no signal.
	if (d.focusReader != nil) != (d.focusRedirects != nil) {
		return nil, ErrFocusRedirectWiringIncomplete
	}
	return d, nil
}

// Dispatch parses and executes a command.
func (d *Dispatcher) Dispatch(ctx context.Context, input string, exec *CommandExecution) (err error) {
	metrics := NewMetricsRecorder()
	defer metrics.Record()

	// Attach an audit event slice to the dispatch context so plugin-emitted
	// hints can accumulate during command processing. Also attach the exec
	// so the flush path can stamp Subject/Action on events that lack them
	// (events pushed directly by the Lua capability, as opposed to those
	// that came through extractAuditHints which stamps them inline).
	ctx = audit.NewContextForDispatch(ctx)
	ctx = context.WithValue(ctx, execContextKey{}, exec)

	// Flush accumulated plugin audit events at the end of dispatch. Errors
	// are logged and metric-counted but never fail the user's operation
	// per the failure mode decision in the spec.
	defer d.flushPluginAuditEvents(ctx)

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

	// Focus-routed redirect (holomush-g1qcw). Rewrites a scene-focused ambient
	// verb (pose/say/ooc/emit) to the plugin-declared target command before any
	// telemetry/rate-limit/lookup read of parsed.Name, so all of them observe
	// the effective (routed) command. invokedAs is preserved by construction.
	redirectVerb, wasRedirected, focusReadErr := d.maybeRedirectForFocus(ctx, parsed, exec.ConnectionID())

	// Set command name for metrics (now we know it)
	metrics.SetCommandName(parsed.Name)

	// Start trace span
	ctx, span := tracer.Start(
		ctx, "command.execute",
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

	if wasRedirected {
		span.SetAttributes(
			attribute.Bool("command.focus_redirected", true),
			attribute.String("command.focus_redirect_verb", redirectVerb),
		)
	}

	// The focus-read fail-open path already logged a WARN and counted an
	// engine-failure metric inside maybeRedirectForFocus (before the span
	// existed); surface it on the span too so a live degradation is visible
	// in tracing, not just logs. The command still proceeds routed to
	// location per the fail-open contract (§4.5) — this only adds signal.
	if focusReadErr != nil {
		span.SetAttributes(
			attribute.Bool("command.focus_redirect_failed_open", true),
			attribute.String("command.focus_redirect_error", focusReadErr.Error()),
		)
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

	// Layer 1: Command execution check
	if execErr := CheckCommandExecution(ctx, d.engine, subject, parsed.Name); execErr != nil {
		oopsErr, ok := oops.AsOops(execErr)
		code, isStr := oopsErr.Code().(string)
		if ok && isStr && code == CodePermissionDenied {
			metrics.SetStatus(StatusPermissionDenied)
		} else {
			metrics.SetStatus(StatusEngineFailure)
			span.RecordError(execErr)
			span.SetStatus(codes.Error, execErr.Error())
		}
		return execErr
	}

	// Layer 2: Capability pre-flight
	if preflightErr := CheckCapabilityPreFlight(ctx, d.engine, subject, parsed.Name, entry.GetCapabilities()); preflightErr != nil {
		oopsErr, ok := oops.AsOops(preflightErr)
		code, isStr := oopsErr.Code().(string)
		if ok && isStr && code == CodePermissionDenied {
			metrics.SetStatus(StatusPermissionDenied)
		} else {
			metrics.SetStatus(StatusEngineFailure)
			span.RecordError(preflightErr)
			span.SetStatus(codes.Error, preflightErr.Error())
		}
		return preflightErr
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
		slog.WarnContext(
			ctx, "command execution failed",
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

// maybeRedirectForFocus rewrites parsed in place when the connection's focus
// kind maps parsed.Name to a target command. Returns the original verb and true
// when a redirect was applied (for span telemetry). It reads focus lazily —
// only when parsed.Name is a redirect candidate — and fails open (no rewrite,
// route to location) on any focus-read error, mapping to the spec's §4.5
// failure semantics. invokedAs is intentionally NOT touched here so no-space /
// OOC-style semantics carried on invokedAs survive (spec §4.4).
//
// focusReadErr is returned (rather than only logged) so the caller — which
// starts the trace span after this call returns — can also record the
// fail-open degradation as span telemetry, mirroring the fail-open
// observability pattern in RateLimitMiddleware.Enforce (metric + span
// attribute, not just a WARN log).
func (d *Dispatcher) maybeRedirectForFocus(
	ctx context.Context, parsed *ParsedCommand, connID ulid.ULID,
) (origVerb string, redirected bool, focusReadErr error) {
	if d.focusRedirects == nil || d.focusReader == nil {
		return "", false, nil
	}
	if connID == (ulid.ULID{}) || !d.focusRedirects.Redirects(parsed.Name) {
		return "", false, nil
	}
	kind, err := d.focusReader.ConnectionFocusKind(ctx, connID)
	if err != nil {
		slog.WarnContext(ctx, "focus-redirect lookup failed; routing to location",
			"command", parsed.Name, "connection_id", connID.String(), "error", err)
		observability.RecordEngineFailure("focus_redirect")
		return "", false, oops.Wrap(err)
	}
	target, ok := d.focusRedirects.Target(parsed.Name, string(kind))
	if !ok {
		return "", false, nil
	}
	verb := parsed.Name
	parsed.Name = target
	parsed.Args = strings.TrimSpace(verb + " " + parsed.Args)
	return verb, true, nil
}

// dispatchToPlugin routes a command through the PluginManager and processes the response.
func (d *Dispatcher) dispatchToPlugin(ctx context.Context, entry *CommandEntry, exec *CommandExecution, invokedAs string, metrics *MetricsRecorder, span trace.Span) error {
	if d.pluginDeliverer == nil {
		return oops.Code("NO_PLUGIN_DELIVERER").
			With("command", entry.Name).
			With("plugin", entry.PluginName()).
			Errorf("command is plugin-backed but no PluginCommandDeliverer configured")
	}

	// Phase 5 (5rh.14 T19): only propagate ConnectionID when set. A zero-value
	// ulid.ULID stringifies to 26 zeros, which downstream consumers (focus
	// client, plugin handlers) cannot distinguish from a real connection;
	// pass an empty string to mean "no connection context" (CodeRabbit PR
	// #4191).
	connectionIDStr := ""
	if cid := exec.ConnectionID(); cid != (ulid.ULID{}) {
		connectionIDStr = cid.String()
	}

	cmd := pluginsdk.CommandRequest{
		Command:       entry.Name,
		Args:          exec.Args,
		CharacterID:   exec.CharacterID().String(),
		CharacterName: exec.CharacterName(),
		LocationID:    exec.LocationID().String(),
		SessionID:     exec.SessionID().String(),
		PlayerID:      exec.PlayerID().String(),
		InvokedAs:     invokedAs,
		ConnectionID:  connectionIDStr,
	}

	span.SetAttributes(
		attribute.String("command.plugin", entry.PluginName()),
		attribute.Bool("command.plugin_routed", true),
	)

	// Stamp ActorCharacter on the dispatch ctx BEFORE DeliverCommand. This
	// activates the actor-metadata channel for the host's outgoing metadata
	// injection (host.go) and the binary-plugin token issuance (per spec G7).
	dispatchCtx := core.WithActor(ctx, core.Actor{
		Kind: core.ActorCharacter,
		ID:   exec.CharacterID().String(),
	})

	// Also stamp the host-vouched owning player of the acting character. This is
	// the SINGLE stamping site feeding BOTH the binary-plugin dispatch token
	// (host.go issues it onto the emit-token entry) and the in-process Lua ctx
	// (core.OwningPlayerFromContext). PLAYER-scope settings ownership compares the
	// request's principal_id against this value (holomush-iokti.19). The player ID
	// is the dispatcher's authenticated executor identity — never plugin-supplied.
	//
	// Only stamp when PlayerID is set. A zero-value ulid.ULID stringifies to 26
	// zeros — a syntactically valid, parseable ULID — and PlayerID is optional
	// (legacy sessions predating the players.player_session_id column leave it
	// zero; see store/session_store.go). Stamping the 26-zero string would make
	// CheckPrincipalOwnership compare against a real-looking anchor, letting any
	// caller that crafts principal_id="000…0" match the shared zero partition —
	// a fail-OPEN bypass of the PLAYER-scope gate. Leaving the owner unstamped
	// keeps expectedOwnerID empty, which CheckPrincipalOwnership fails closed on.
	// Mirrors the ConnectionID treatment above (holomush-sl0ir.3 / iokti.19).
	if pid := exec.PlayerID(); pid != (ulid.ULID{}) {
		dispatchCtx = core.WithOwningPlayer(dispatchCtx, pid.String())
	}

	resp, err := d.pluginDeliverer.DeliverCommand(dispatchCtx, entry.PluginName(), cmd)
	if err != nil {
		return oops.In("dispatcher").With("command", entry.Name).With("plugin", entry.PluginName()).Wrap(err)
	}
	if resp == nil {
		return nil
	}

	// Extract audit hints from the response, stamp host-controlled fields,
	// and push each onto the context-bound slice. The dispatcher's deferred
	// flush will route them through the audit logger.
	d.extractAuditHints(ctx, resp.AuditHints, entry, exec)

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

	// Process response events through the shared plugin emitter so manifest
	// validation and host-owned stamping stay consistent with subscriber flow.
	// Reuse dispatchCtx (already actor-stamped above) so emit and DeliverCommand
	// share identical actor context.
	for _, evt := range resp.Events {
		if emitErr := d.pluginDeliverer.EmitPluginEvent(dispatchCtx, entry.PluginName(), evt); emitErr != nil {
			return oops.In("dispatcher").
				With("command", entry.Name).
				With("stream", evt.Stream).
				Wrap(emitErr)
		}
	}

	// Process synchronous output: write to exec.Output().
	if resp.Output != "" && exec.Output() != nil {
		if _, writeErr := exec.Output().Write([]byte(resp.Output)); writeErr != nil {
			slog.WarnContext(
				ctx, "failed to write plugin command output",
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

// execContextKey is the context.WithValue key for the CommandExecution
// attached during Dispatch. The flush path uses this to stamp Subject
// and Action on events whose emit path did not fill them in (e.g., Lua
// capability events).
type execContextKey struct{}

// flushPluginAuditEvents drains any plugin-emitted audit events attached to
// ctx and writes them through the configured audit logger. Called from
// Dispatch's deferred path. Errors are logged, counted, and then dropped —
// audit flush failures MUST NOT fail the user's command per the spec.
//
// For each event, Subject and Action are stamped from the dispatch context
// if they are empty (Lua capability emit path leaves them blank; the binary
// path fills them in via extractAuditHints).
func (d *Dispatcher) flushPluginAuditEvents(ctx context.Context) {
	events := audit.EventsFromContext(ctx)
	if len(events) == 0 {
		return
	}

	if d.auditLogger == nil {
		// No logger configured — drop events silently. This is the
		// correct behavior for tests that don't wire an audit logger.
		return
	}

	// Retrieve the exec for host field stamping of any events that lack
	// Subject/Action (typically Lua-emitted events). When the key is
	// absent or of the wrong type, exec will be nil and the loop body
	// handles that gracefully.
	exec, ok := ctx.Value(execContextKey{}).(*CommandExecution)
	if !ok {
		exec = nil
	}

	for i := range events {
		// Fill in host-controlled fields that the emit path may have
		// left empty.
		if events[i].Subject == "" && exec != nil {
			events[i].Subject = access.CharacterSubject(exec.CharacterID().String())
		}
		if events[i].Action == "" && exec != nil {
			events[i].Action = exec.InvokedAs
		}
		if events[i].Timestamp.IsZero() {
			events[i].Timestamp = time.Now()
		}

		if logErr := d.auditLogger.Log(ctx, events[i]); logErr != nil {
			slog.WarnContext(
				ctx, "plugin audit event flush failed",
				"subject", events[i].Subject,
				"action", events[i].Action,
				"resource", events[i].Resource,
				"component", events[i].Component,
				"error", logErr,
			)
			audit.RecordPluginAuditFailure()
			// Continue with remaining events.
		}
	}
}

// extractAuditHints converts plugin-provided audit hints into audit.Event
// values, stamping host-controlled fields (subject, action base, source,
// component, timestamp, duration) from the dispatch context. The plugin
// cannot spoof these fields — the dispatcher overwrites them regardless
// of what the hint contains.
func (d *Dispatcher) extractAuditHints(ctx context.Context, hints []pluginsdk.AuditHint, entry *CommandEntry, exec *CommandExecution) {
	if len(hints) == 0 {
		return
	}

	hostSubject := access.CharacterSubject(exec.CharacterID().String())
	hostComponent := entry.PluginName()
	if hostComponent == "" {
		hostComponent = "unknown-plugin"
	}

	for _, hint := range hints {
		// Validate resource shape if provided. A malformed resource is
		// logged but does not abort the flush — the event is emitted
		// with the plugin-provided value (operators see the malformed
		// string and can investigate).
		if hint.Resource != "" && !isValidResourceRef(hint.Resource) {
			slog.WarnContext(
				ctx, "plugin audit hint has malformed resource",
				"plugin", hostComponent,
				"resource", hint.Resource,
				"hint_id", hint.ID,
			)
		}

		// Compose the final action: base from the dispatcher, qualifier
		// from the plugin. Joined with ':'.
		action := entry.Name
		if hint.ActionQualifier != "" {
			action = entry.Name + ":" + hint.ActionQualifier
		}

		// Convert hint effect to audit effect.
		var effect types.Effect
		switch hint.Effect {
		case pluginsdk.AuditEffectAllow:
			effect = types.EffectAllow
		case pluginsdk.AuditEffectDeny:
			effect = types.EffectDeny
		default:
			// Unknown effect from plugin — log and skip this hint.
			slog.WarnContext(
				ctx, "plugin audit hint has unknown effect",
				"plugin", hostComponent,
				"effect", hint.Effect,
				"hint_id", hint.ID,
			)
			continue
		}

		// Merge attributes: plugin-provided first, then host-overlay
		// (host keys win on collision).
		merged := make(map[string]any, len(hint.Attributes)+2)
		for k, v := range hint.Attributes {
			merged[k] = v
		}
		merged["command.invoked_as"] = exec.InvokedAs

		event := audit.Event{
			ID:         hint.ID,
			Name:       hint.Name,
			Message:    hint.Message,
			Source:     audit.SourcePlugin, // host-stamped, plugin cannot spoof
			Component:  hostComponent,      // host-stamped from entry.PluginName()
			Subject:    hostSubject,        // host-stamped from dispatch context
			Action:     action,             // composed: base + qualifier
			Resource:   hint.Resource,      // plugin-provided (shape validated above)
			Effect:     effect,
			Attributes: merged,
			DurationUS: 0, // per-hint duration is not meaningful for D-inline — hints accumulate during handler execution and flush atomically
			Timestamp:  time.Now(),
		}

		audit.AddEventToContext(ctx, event)
	}
}

// isValidResourceRef performs a minimal shape check on a plugin-provided
// resource reference. Valid form: "<type>:<id>" with at least one character
// on each side of the colon.
func isValidResourceRef(ref string) bool {
	colon := strings.Index(ref, ":")
	return colon > 0 && colon < len(ref)-1
}
