// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package main implements the forgery_plugin: a test-only binary plugin
// that exercises the host's plugin actor-claim authentication boundary
// (spec §3.3.5 / §5.6, G1 forgery resistance).
//
// The plugin is driven by the test through the EVENT PAYLOAD rather than
// process env vars — go-plugin spawns the binary with a stripped env
// (PATH + cert paths only), so env-based scenario selection isn't viable.
// Instead the test sends a HandleEvent whose payload is a Mode JSON
// document; the plugin applies the requested forgery mode.
//
// Mode JSON schema (matches the Mode struct below):
//
//	{
//	  "subject": "location.01HFORGEY00LOCATIONULID0000",
//	  "type": "say",
//	  "payload": "{\"message\":\"...\"}",
//	  "forgery_override_kind": "0",     // optional ActorKind int
//	  "forgery_override_id":   "01...", // optional actor ID
//	  "fabricate_token":       "...",   // optional fake token
//	  "emit_from_background":  false,   // if true, emit from a goroutine
//	                                    // after HandleEvent returns
//	  "result_file":           ""       // for background mode: where to
//	                                    // write the goroutine's emit error
//	}
//
// Honest-mode dispatch leaves the four override fields blank — the
// plugin ferries the host-issued token verbatim and the published event
// is stamped with the host-vouched actor.
//
// The plugin returns the synchronous emit's error string back to the
// test via HandleEvent's error return; the test reads it from
// host.DeliverEvent's error chain. Background-mode results travel via
// the result_file path.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

const (
	tokenHeader = "x-holomush-emit-token"
	kindHeader  = "x-holomush-actor-kind"
	idHeader    = "x-holomush-actor-id"
)

// Mode encodes the test scenario the plugin should run for an incoming
// HandleEvent. Encoded into the event payload.
type Mode struct {
	Subject             string `json:"subject"`
	Type                string `json:"type"`
	Payload             string `json:"payload"`
	ForgeryOverrideKind string `json:"forgery_override_kind,omitempty"`
	ForgeryOverrideID   string `json:"forgery_override_id,omitempty"`
	FabricateToken      string `json:"fabricate_token,omitempty"`
	EmitFromBackground  bool   `json:"emit_from_background,omitempty"`
	ResultFile          string `json:"result_file,omitempty"`
}

type forgeryPlugin struct {
	sink pluginsdk.EventSink
}

// HandleEvent runs each test scenario. The host invokes HandleEvent with
// incoming metadata carrying the host-issued token + actor headers. The
// plugin parses the event payload as a Mode, builds a controlled
// outgoing context, and calls sink.Emit. The synchronous emit error is
// returned back to the host so the test can assert on it via
// host.DeliverEvent's error chain.
func (p *forgeryPlugin) HandleEvent(ctx context.Context, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	if p.sink == nil {
		return nil, errors.New("forgery_plugin: sink not injected")
	}

	var mode Mode
	if event.Payload != "" {
		if err := json.Unmarshal([]byte(event.Payload), &mode); err != nil {
			return nil, fmt.Errorf("forgery_plugin: malformed Mode payload: %w", err)
		}
	}

	emitType := mode.Type
	if emitType == "" {
		emitType = "core-communication:say"
	}
	emitPayload := mode.Payload
	if emitPayload == "" {
		emitPayload = `{"message":"forgery-test"}`
	}
	intent := pluginsdk.EmitIntent{
		Subject: mode.Subject,
		Type:    pluginsdk.EventType(emitType),
		Payload: emitPayload,
	}

	if mode.EmitFromBackground {
		// Spawn a goroutine that emits AFTER HandleEvent returns, using
		// a fresh background context with NO token. Recording the
		// result via a file lets the test poll it (the synchronous
		// DeliverEvent return cannot carry the goroutine's outcome).
		resultFile := mode.ResultFile
		go func() {
			bgErr := p.sink.Emit(context.Background(), intent)
			writeResult(resultFile, bgErr)
		}()
		return nil, nil
	}

	// Standard emit path: read incoming metadata, build outgoing,
	// optionally apply forgery overrides, call sink.
	outgoingCtx := buildOutgoingCtx(ctx, mode)
	if err := p.sink.Emit(outgoingCtx, intent); err != nil {
		return nil, fmt.Errorf("forgery_emit_failed: %s", err.Error())
	}
	return nil, nil
}

// buildOutgoingCtx constructs a fresh outgoing-metadata context for the
// EmitEvent call. The plugin reads token + actor headers from the
// HandleEvent INCOMING metadata, then builds OUTGOING metadata on a
// background context — using a fresh ctx (not derived from the SDK's
// value-keyed actor stamp) so the SDK's actor-metadata override path
// does not silently rewrite the plugin's forged headers. This is the
// realistic attack model: a malicious plugin can ALWAYS construct
// arbitrary outgoing metadata; the host must defend at its own boundary.
//
// When mode.FabricateToken is set, the fabricated token replaces the
// genuine one (test asserts EMIT_TOKEN_REJECTED). When
// mode.ForgeryOverrideKind / .ForgeryOverrideID are set, the plugin
// substitutes those into the outgoing actor headers — this is the G1
// attack surface; the host MUST ignore the headers and use the actor
// stored at token-issue time.
func buildOutgoingCtx(ctx context.Context, mode Mode) context.Context {
	incoming, _ := metadata.FromIncomingContext(ctx)

	outgoing := metadata.MD{}
	if tokens := incoming.Get(tokenHeader); len(tokens) > 0 {
		outgoing.Set(tokenHeader, tokens[0])
	}
	if kinds := incoming.Get(kindHeader); len(kinds) > 0 {
		outgoing.Set(kindHeader, kinds[0])
	}
	if ids := incoming.Get(idHeader); len(ids) > 0 {
		outgoing.Set(idHeader, ids[0])
	}

	// Optionally replace the genuine token with a fabricated one.
	if mode.FabricateToken != "" {
		outgoing.Set(tokenHeader, mode.FabricateToken)
	}

	// Optionally forge the actor metadata (G1 attack).
	if mode.ForgeryOverrideKind != "" {
		outgoing.Set(kindHeader, mode.ForgeryOverrideKind)
	}
	if mode.ForgeryOverrideID != "" {
		outgoing.Set(idHeader, mode.ForgeryOverrideID)
	}

	// IMPORTANT: derive from context.Background() rather than the
	// HandleEvent ctx. The SDK's pkg/plugin/sdk.go injects an actor-
	// metadata value into the HandleEvent ctx at line 195
	// (contextWithIncomingActorMetadata). pluginHostEventSink.Emit
	// then reads that value via actorMetadataFromContext and re-stamps
	// it onto outgoing metadata via WithOutgoingActorMetadata —
	// silently OVERWRITING our forged headers. By using a fresh
	// background ctx, our outgoing metadata stays intact through the
	// SDK; the SDK's actorMetadataFromContext falls through to the
	// outgoing-metadata reader and returns whatever WE set, so its
	// "re-stamp" is a no-op. This models the real attack: a
	// fully malicious plugin can ALWAYS replace the SDK or construct
	// arbitrary metadata; the security boundary lives at the host.
	_ = ctx
	return metadata.NewOutgoingContext(context.Background(), outgoing)
}

// writeResult writes a string representation of err (or "ok") to path.
// Best-effort; failures are silent (the test will time out reading and
// surface the missing-file condition itself).
func writeResult(path string, err error) {
	if path == "" {
		return
	}
	contents := "ok"
	if err != nil {
		contents = err.Error()
	}
	_ = os.WriteFile(path, []byte(contents), 0o600) //nolint:gosec // test artifact path supplied by the test process
}

// SetEventSink captures the SDK-injected EventSink (EventSinkAware
// contract in pkg/plugin/sdk.go).
func (p *forgeryPlugin) SetEventSink(sink pluginsdk.EventSink) {
	p.sink = sink
}

// RegisterServices is a no-op — forgery_plugin provides no gRPC
// services beyond HandleEvent + EventSink injection.
func (p *forgeryPlugin) RegisterServices(_ grpc.ServiceRegistrar) {}

// Init is a no-op; forgery_plugin needs no DB or external service wiring.
func (p *forgeryPlugin) Init(_ context.Context, _ *pluginv1.ServiceConfig) error {
	return nil
}

func main() {
	plugin := &forgeryPlugin{}
	pluginsdk.ServeWithServices(
		&pluginsdk.ServeConfig{Handler: plugin},
		plugin,
	)
}
