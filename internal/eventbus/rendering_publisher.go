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
		DisplayTarget:       EventChannel(reg.DisplayTarget),
		SourcePlugin:        reg.Source,
		SourcePluginVersion: p.registry.SourceVersion(reg.Source),
	}

	// Stamp the App-Rendering NATS header (protojson form) so the audit
	// projection can write events_audit.rendering without proto-decoding
	// the envelope. INV-GW-15 enforces parity with event.Rendering.
	headerBytes, err := renderingJSONOpts.Marshal(RenderingToProto(event.Rendering))
	if err != nil {
		return oops.Code("EMIT_HEADER_MARSHAL_FAILED").
			With("event_type", string(event.Type)).
			Wrap(err)
	}
	if event.Headers == nil {
		event.Headers = make(map[string]string, 1)
	}
	event.Headers["App-Rendering"] = string(headerBytes)

	// Validate the rendering proto against protovalidate rules (INV-GW-4).
	if vErr := p.validateRendering(RenderingToProto(event.Rendering)); vErr != nil {
		return oops.Code("EMIT_VALIDATION_FAILED").
			With("event_type", string(event.Type)).
			Wrap(vErr)
	}

	return p.inner.Publish(ctx, event)
}

// validateRendering runs protovalidate against a RenderingMetadata proto.
func (p *RenderingPublisher) validateRendering(md *corev1.RenderingMetadata) error {
	return p.validator.Validate(md)
}
