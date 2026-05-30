// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package plugins provides plugin management and lifecycle control.
package plugins

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// ManifestLookup returns the manifest for a loaded plugin.
type ManifestLookup func(pluginName string) *Manifest

// ActorResolver returns the actor that should be stamped onto emitted events.
type ActorResolver func(ctx context.Context, pluginName string) (core.Actor, error)

// GameIDProvider returns the current game id, used to compose JetStream
// subjects during the F1 cutover (events.<game_id>.<ns>.<...>). The function
// MAY return "" to signal "no game id known yet", in which case a legacy
// default is used so the emitter still functions during early bootstrap.
type GameIDProvider func() string

// PluginEventEmitter publishes host-owned events on behalf of plugins to the
// JetStream EventBus. Post-F1 this is the single publish path for plugin
// emits; the prior core.EventStore.Append path is gone.
//
// Subject compatibility (F1 transitional):
//
// Plugins today emit subjects in the legacy colon-delimited shape
// ("scene:01ABC", "location:01XYZ"). JetStream requires dot-delimited tokens
// starting with "events.". The emitter accepts either form and translates
// legacy subjects to JetStream form by prepending `events.<game_id>.` and
// rewriting `:` to `.`. F5 migrates plugin code to emit JetStream-native
// subjects directly, at which point the translation becomes a no-op.
//
// Manifest emits validation stays colon-namespace based in F1 (manifests
// declare e.g. `emits: [scene]`). Post-translation, the check ensures the
// concrete subject's namespace token matches a declared namespace.
type PluginEventEmitter struct {
	publisher      eventbus.Publisher
	lookupManifest ManifestLookup
	resolveActor   ActorResolver
	gameID         GameIDProvider
	cryptoEnabled  bool
}

// EmitterOption customizes PluginEventEmitter construction.
type EmitterOption func(*PluginEventEmitter)

// WithGameID injects a game-id provider. When unset, the emitter uses
// "main" (matches eventbus.Config default game_id).
func WithGameID(p GameIDProvider) EmitterOption {
	return func(e *PluginEventEmitter) { e.gameID = p }
}

// WithCryptoEnabled toggles the Phase 3a sensitivity fence. When false
// (the default and the production state at Phase 3a merge time), the
// emitter skips the manifest sensitivity lookup + EnforceSensitivity
// check and stamps event.Sensitive=false unconditionally — matching
// pre-Phase-3a behavior. When true, the fence runs and INV-6/INV-7 are
// enforced. The flag is sourced from eventbus.Config.Crypto.Enabled in
// internal/bootstrap.
//
// This gate is required because production manifests already declare
// sensitivity: always for events like core-communication's page,
// whisper, and pemit (plugins/core-communication/plugin.yaml), and
// neither the Lua nor binary plugin SDK currently surfaces
// EmitIntent.Sensitive over the wire. Running the fence unconditionally
// would reject every page/whisper/pemit emit with
// EVENT_SENSITIVITY_REQUIRED until the plugin SDK gains the field.
func WithCryptoEnabled(enabled bool) EmitterOption {
	return func(e *PluginEventEmitter) { e.cryptoEnabled = enabled }
}

// NewPluginEventEmitter wires a new shared host event emitter.
//
// publisher is the eventbus Publisher (typically obtained from
// Subsystem.Publisher()); post-F1 this is a JetStream publisher.
//
// lookup and resolve are the same manifest / actor shims used pre-F1.
func NewPluginEventEmitter(publisher eventbus.Publisher, lookup ManifestLookup, resolve ActorResolver, opts ...EmitterOption) *PluginEventEmitter {
	e := &PluginEventEmitter{
		publisher:      publisher,
		lookupManifest: lookup,
		resolveActor:   resolve,
		gameID:         func() string { return "main" },
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Emit validates the target subject namespace against the plugin manifest,
// stamps host-owned fields (event id, timestamp, actor), and publishes the
// event through the EventBus Publisher.
//
// Errors are surfaced to callers unchanged (oops-wrapped with plugin/subject
// context). Dedup-window exhaustion surfaces as eventbus.ErrPublishExpired
// via errors.Is.
func (e *PluginEventEmitter) Emit(ctx context.Context, pluginName string, intent pluginsdk.EmitIntent) error {
	subjectRaw := intent.Subject
	if strings.TrimSpace(subjectRaw) != subjectRaw || subjectRaw == "" {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).
			Errorf("subject must not be empty or padded with whitespace")
	}

	namespace, err := subjectNamespace(subjectRaw)
	if err != nil {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).Wrap(err)
	}
	if e.lookupManifest == nil {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).
			New("plugin manifest lookup is not configured")
	}
	manifest := e.lookupManifest(pluginName)
	if manifest == nil || !declaresEmitNamespace(manifest.Emits, namespace) {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).
			Errorf("plugin may not emit to namespace %q", namespace)
	}

	if e.resolveActor == nil {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).
			New("plugin actor resolver is not configured")
	}
	actor, err := e.resolveActor(ctx, pluginName)
	if err != nil {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).Wrap(err)
	}
	if vErr := validateResolvedActor(actor); vErr != nil {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).Wrap(vErr)
	}

	// Manifest gate (universal — applies to Lua and binary plugins). Asserts
	// the actor's Kind is in the plugin's actor_kinds_claimable list.
	// Per spec §3.4 + project invariant on plugin runtime symmetry: this
	// gate fires for both Lua and binary plugins because both runtimes
	// flow through this Emit codepath.
	if !manifest.DeclaresActorKindClaimable(actor.Kind) {
		return oops.Code("EMIT_ACTOR_KIND_NOT_CLAIMABLE").
			With("plugin", pluginName).
			With("subject", subjectRaw).
			With("actor_kind", actor.Kind.String()).
			With("declared_kinds", manifest.ActorKindsClaimable).
			Errorf("plugin manifest does not declare %q as a claimable actor kind", actor.Kind.String())
	}

	// Phase 3a: resolve manifest sensitivity + run the host-side fence.
	// Result is stamped on event.Sensitive; the publisher acts on it.
	//
	// The fence is gated behind WithCryptoEnabled because production
	// manifests already declare sensitivity: always for events whose
	// emit path cannot yet set EmitIntent.Sensitive=true (the proto
	// EmitEventRequest has no sensitive field). Running the fence
	// unconditionally would break those emits. When crypto is off the
	// emitter behaves as it did pre-Phase-3a: event.Sensitive=false
	// regardless of manifest declaration.
	sensitive := false
	if e.cryptoEnabled {
		manifestSensitivity := LookupEmitSensitivity(manifest, string(intent.Type))
		effective, fenceErr := EnforceSensitivity(manifestSensitivity, intent.Sensitive)
		if fenceErr != nil {
			return oops.With("plugin", pluginName).
				With("subject", subjectRaw).
				With("event_type", string(intent.Type)).
				Wrap(fenceErr)
		}
		sensitive = effective == SensitivityAlways
	}

	if e.publisher == nil {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).
			New("plugin event publisher is not configured")
	}

	payload := []byte(intent.Payload)
	if pErr := core.ValidatePayload(payload); pErr != nil {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).Wrap(pErr)
	}
	if !json.Valid(payload) {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).
			New("event payload must be valid JSON")
	}

	typ, err := eventbus.NewType(string(intent.Type))
	if err != nil {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).Wrap(err)
	}

	gameID := "main"
	if e.gameID != nil {
		if g := e.gameID(); g != "" {
			gameID = g
		}
	}
	sub, err := eventbus.Qualify(gameID, subjectRaw)
	if err != nil {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).Wrap(err)
	}

	busActor, err := coreActorToEventbusActor(actor)
	if err != nil {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).Wrap(err)
	}
	event := eventbus.Event{
		ID:        core.NewULID(),
		Subject:   sub,
		Type:      typ,
		Timestamp: time.Now().UTC(),
		Actor:     busActor,
		Payload:   payload,
		Sensitive: sensitive,
	}
	if err := e.publisher.Publish(ctx, event); err != nil {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).
			With("qualified_subject", string(sub)).Wrap(err)
	}
	return nil
}

// subjectNamespace extracts the plugin-level namespace token from a raw
// EmitIntent.Subject. Accepts:
//   - JetStream-native: "events.<game_id>.<namespace>.<...>"
//   - Dot-relative:     "<namespace>.<id>[.<facet>...]"  (canonical post-rops form)
//   - Legacy colon:     "<namespace>[:<suffix>]"         (rejected by Qualify — kept for early validation)
func subjectNamespace(subject string) (string, error) {
	// JetStream-native: events.<game_id>.<namespace>.<...>
	if strings.HasPrefix(subject, "events.") {
		parts := strings.SplitN(subject, ".", 4)
		if len(parts) < 3 {
			return "", oops.New("JetStream subject must include namespace token after game id")
		}
		ns := parts[2]
		if ns == "" {
			return "", oops.New("JetStream subject namespace must not be empty")
		}
		if !namePattern.MatchString(ns) {
			return "", oops.With("namespace", ns).
				New("subject namespace must match plugin naming pattern")
		}
		return ns, nil
	}

	// Dot-relative: <namespace>.<id>[.<facet>...] — canonical post-rops form.
	// Colon subjects fall through to the legacy branch below.
	if !strings.ContainsRune(subject, ':') {
		if idx := strings.IndexByte(subject, '.'); idx >= 0 {
			ns := subject[:idx]
			if !namePattern.MatchString(ns) {
				return "", oops.With("namespace", ns).
					New("subject namespace must match plugin naming pattern")
			}
			return ns, nil
		}
		// Bare single-token (e.g. "global"): the token IS the namespace. This is
		// a live emit stream per the rops design (Emitter.Global → "global"); the
		// manifest gate at the call site remains the authority on whether a plugin
		// may emit it. Reject only malformed tokens.
		if !namePattern.MatchString(subject) {
			return "", oops.With("namespace", subject).
				New("subject namespace must match plugin naming pattern")
		}
		return subject, nil
	}

	// Legacy colon-delimited: <namespace>[:<suffix>] — still validated for
	// early error quality, but Qualify will reject the colon form before publish.
	suffix := ""
	hasSeparator := false
	head := subject
	if idx := strings.IndexByte(subject, ':'); idx >= 0 {
		hasSeparator = true
		suffix = subject[idx+1:]
		head = subject[:idx]
	}
	if head == "" {
		return "", oops.New("subject namespace must not be empty")
	}
	if suffix != "" && strings.TrimSpace(suffix) != suffix {
		return "", oops.New("subject suffix must not be padded with whitespace")
	}
	if hasSeparator && suffix == "" {
		return "", oops.New("subject suffix must not be empty")
	}
	if !namePattern.MatchString(head) {
		return "", oops.With("namespace", head).
			New("subject namespace must match plugin naming pattern")
	}
	return head, nil
}

func validateResolvedActor(actor core.Actor) error {
	if actor.ID == "" {
		return oops.New("plugin actor resolver returned actor with empty ID")
	}
	switch actor.Kind {
	case core.ActorCharacter, core.ActorSystem, core.ActorPlugin:
		return nil
	default:
		return oops.With("actor_kind", actor.Kind).
			New("plugin actor resolver returned unknown actor kind")
	}
}

func declaresEmitNamespace(namespaces []string, namespace string) bool {
	for _, declared := range namespaces {
		if declared == namespace {
			return true
		}
	}
	return false
}

// coreActorToEventbusActor bridges core.Actor (ID is a string) to the
// JetStream-side Actor (ID is a ULID). Post-w9ml every stamp site MUST
// produce a parseable ULID; non-ULID input here is a contract violation
// surfaced as ACTOR_ID_NOT_ULID with full context.
//
// Empty ID is permitted and maps to a zero ULID (valid for system/unknown
// kinds); callers that require a non-zero ID enforce that separately.
func coreActorToEventbusActor(a core.Actor) (eventbus.Actor, error) {
	out := eventbus.Actor{Kind: bridgeActorKind(a.Kind)}
	if a.ID == "" {
		return out, nil
	}
	parsed, err := ulid.Parse(a.ID)
	if err != nil {
		return eventbus.Actor{}, oops.Code("ACTOR_ID_NOT_ULID").
			With("kind", a.Kind.String()).
			With("id", a.ID).
			Wrap(err)
	}
	out.ID = parsed
	return out, nil
}

func bridgeActorKind(k core.ActorKind) eventbus.ActorKind {
	switch k {
	case core.ActorCharacter:
		return eventbus.ActorKindCharacter
	case core.ActorSystem:
		return eventbus.ActorKindSystem
	case core.ActorPlugin:
		return eventbus.ActorKindPlugin
	default:
		return eventbus.ActorKindUnknown
	}
}
