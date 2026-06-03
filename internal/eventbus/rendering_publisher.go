// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"context"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/holomush/holomush/internal/core"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	"github.com/samber/oops"
)

// renderingJSONOpts is the canonical protojson form for the App-Rendering
// NATS header. UseProtoNames produces snake_case field names matching the
// proto; UseEnumNumbers=false emits enum names like "EVENT_CHANNEL_TERMINAL";
// EmitUnpopulated keeps the shape stable across producers.
var renderingJSONOpts = protojson.MarshalOptions{
	UseProtoNames:   true,
	UseEnumNumbers:  false,
	EmitUnpopulated: true,
}

// RenderingPublisher wraps an underlying eventbus.Publisher and is the
// single enrichment site for rendering metadata. At Publish time it:
//
//  1. Looks up event.Type in the verb registry.
//  2. Stamps event.Rendering from the registration.
//  3. Stamps event.Headers["App-Rendering"] with the protojson form. (Task 12)
//  4. Validates the proto projection against the manifest's protovalidate rules. (Task 14)
//  5. Delegates to the underlying publisher.
type RenderingPublisher struct {
	inner     Publisher
	registry  *core.VerbRegistry
	validator protovalidate.Validator
}

// NewRenderingPublisher constructs a wrapper. inner and registry MUST NOT be nil.
func NewRenderingPublisher(inner Publisher, registry *core.VerbRegistry) *RenderingPublisher {
	if inner == nil {
		panic("eventbus.NewRenderingPublisher: inner publisher is nil")
	}
	if registry == nil {
		panic("eventbus.NewRenderingPublisher: verb registry is nil")
	}
	v, err := protovalidate.New()
	if err != nil {
		panic("eventbus.NewRenderingPublisher: failed to construct protovalidate.Validator: " + err.Error())
	}
	return &RenderingPublisher{inner: inner, registry: registry, validator: v}
}

// Publish enriches event with rendering metadata and delegates to the
// underlying publisher.
func (p *RenderingPublisher) Publish(ctx context.Context, event Event) error {
	reg, ok := p.registry.Lookup(string(event.Type))
	if !ok {
		return oops.Code("EMIT_UNKNOWN_VERB").
			With("event_type", string(event.Type)).
			Errorf("verb registry has no entry for event type")
	}

	event.Rendering = &RenderingMetadata{
		Category:            reg.Category,
		Format:              reg.Format,
		Label:               reg.Label,
		DisplayTarget:       EventChannel(reg.DisplayTarget), //nolint:gosec // G115: EventChannel values are bounded 0-4 by proto enum; no overflow possible
		SourcePlugin:        reg.Source,
		SourcePluginVersion: p.registry.SourceVersion(reg.Source),
	}

	// Stamp the App-Rendering NATS header (protojson form) so the audit
	// projection can write events_audit.rendering without proto-decoding
	// the envelope. INV-EVENTBUS-15 enforces parity with event.Rendering.
	headerBytes, err := renderingJSONOpts.Marshal(RenderingToProto(event.Rendering))
	if err != nil {
		return oops.Code("EMIT_HEADER_MARSHAL_FAILED").
			With("event_type", string(event.Type)).
			Wrap(err)
	}
	// RenderingPublisher is the single writer of App-Rendering. If a caller
	// already populated it, surface the collision instead of silently
	// clobbering — that masks a contract violation. Copy the caller-owned
	// map before mutation so we don't write through their reference.
	if event.Headers == nil {
		event.Headers = make(map[string]string, 1)
	} else {
		if _, exists := event.Headers["App-Rendering"]; exists {
			return oops.Code("EMIT_RESERVED_HEADER").
				With("event_type", string(event.Type)).
				With("header", "App-Rendering").
				Errorf("caller header collides with reserved system header")
		}
		cloned := make(map[string]string, len(event.Headers)+1)
		for k, v := range event.Headers {
			cloned[k] = v
		}
		event.Headers = cloned
	}
	event.Headers["App-Rendering"] = string(headerBytes)

	// Validate the rendering proto against protovalidate rules (INV-EVENTBUS-5).
	if vErr := p.validateRendering(RenderingToProto(event.Rendering)); vErr != nil {
		return oops.Code("EMIT_VALIDATION_FAILED").
			With("event_type", string(event.Type)).
			Wrap(vErr)
	}

	if err := p.inner.Publish(ctx, event); err != nil {
		return oops.Code("EMIT_PUBLISH_FAILED").
			With("event_type", string(event.Type)).
			Wrap(err)
	}
	return nil
}

// validateRendering runs protovalidate against a RenderingMetadata proto.
func (p *RenderingPublisher) validateRendering(md *corev1.RenderingMetadata) error {
	if err := p.validator.Validate(md); err != nil {
		return oops.Wrap(err)
	}
	return nil
}
