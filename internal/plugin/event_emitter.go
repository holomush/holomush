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
}

// EmitterOption customizes PluginEventEmitter construction.
type EmitterOption func(*PluginEventEmitter)

// WithGameID injects a game-id provider. When unset, the emitter uses
// "main" (matches eventbus.Config default game_id).
func WithGameID(p GameIDProvider) EmitterOption {
	return func(e *PluginEventEmitter) { e.gameID = p }
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
	natsSubject, err := translateSubject(subjectRaw, gameID)
	if err != nil {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).Wrap(err)
	}
	sub, err := eventbus.NewSubject(natsSubject)
	if err != nil {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).
			With("nats_subject", natsSubject).Wrap(err)
	}

	event := eventbus.Event{
		ID:        core.NewULID(),
		Subject:   sub,
		Type:      typ,
		Timestamp: time.Now().UTC(),
		Actor:     coreActorToEventbusActor(actor),
		Payload:   payload,
	}
	if err := e.publisher.Publish(ctx, event); err != nil {
		return oops.With("plugin", pluginName).With("subject", subjectRaw).
			With("nats_subject", natsSubject).Wrap(err)
	}
	return nil
}

// subjectNamespace extracts the plugin-level namespace token from a raw
// EmitIntent.Subject. Accepts legacy colon-delimited subjects (the F1
// status-quo for every in-tree plugin) as well as JetStream-native
// dot-delimited subjects that start with `events.<game_id>.`.
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

	// Legacy colon-delimited: <namespace>[:<suffix>]
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

// translateSubject maps a legacy colon-delimited subject to the JetStream
// form required by `events.>`. No-op for subjects already in JS form.
//
// Mapping: `<ns>[:<a>[:<b>[:...]]]` → `events.<game_id>.<ns>[.<a>[.<b>[...]]]`.
// Tokens must satisfy eventbus.NewSubject rules (letters, digits, `_`, `-`).
func translateSubject(subject, gameID string) (string, error) {
	if strings.HasPrefix(subject, "events.") {
		return subject, nil
	}
	if gameID == "" {
		return "", oops.New("game id required to translate legacy subject")
	}
	parts := strings.Split(subject, ":")
	for i, p := range parts {
		if p == "" {
			return "", oops.With("subject", subject).
				Errorf("legacy subject has empty token at position %d", i)
		}
	}
	out := make([]string, 0, len(parts)+2)
	out = append(out, "events", gameID)
	out = append(out, parts...)
	return strings.Join(out, "."), nil
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

// coreActorToEventbusActor bridges the legacy core.Actor (ID is a string,
// sometimes a ULID and sometimes a plugin name) to the JetStream-side
// Actor (ID is a ULID; zero for anonymous/system). If the core id parses
// as a ULID we carry it through; otherwise we leave it zero and retain the
// Kind alone. App-Actor-ID is then omitted from the header (see publisher).
//
// F7 replaces core.Actor with eventbus.Actor across the codebase; this
// translation is transitional and called out in the plan.
func coreActorToEventbusActor(a core.Actor) eventbus.Actor {
	out := eventbus.Actor{Kind: bridgeActorKind(a.Kind)}
	if a.ID != "" {
		if parsed, err := ulid.Parse(a.ID); err == nil {
			out.ID = parsed
		}
	}
	return out
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
