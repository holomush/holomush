// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"context"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/encoding/protojson"

	commv1 "github.com/holomush/holomush/pkg/proto/holomush/comm/v1"
	"github.com/samber/oops"
)

// ContentValidationPublisher validates the payload of category:communication
// events against holomush.comm.v1.CommunicationContent at emit. It decodes
// the UNTRUSTED plugin JSON (snake_case, unknown-tolerant) then
// protovalidates it — strictly more than RenderingPublisher.validateRendering,
// which validates a host-constructed proto rather than plugin-supplied JSON.
//
// Built this slice; NOT wired into the live publisher chain until the whole
// category:communication event family conforms (spec §8, Slice 2).
type ContentValidationPublisher struct {
	inner     Publisher
	validator protovalidate.Validator
	unmarshal protojson.UnmarshalOptions
}

// NewContentValidationPublisher constructs a wrapper. inner MUST NOT be nil.
func NewContentValidationPublisher(inner Publisher) *ContentValidationPublisher {
	if inner == nil {
		panic("eventbus.NewContentValidationPublisher: inner publisher is nil")
	}
	v, err := protovalidate.New()
	if err != nil {
		panic("eventbus.NewContentValidationPublisher: failed to construct protovalidate.Validator: " + err.Error())
	}
	return &ContentValidationPublisher{
		inner:     inner,
		validator: v,
		unmarshal: protojson.UnmarshalOptions{DiscardUnknown: true},
	}
}

// Publish validates event against holomush.comm.v1.CommunicationContent when
// its rendering category is "communication", then delegates to the inner
// publisher. Events with no rendering metadata or a non-communication
// category pass through unvalidated.
func (p *ContentValidationPublisher) Publish(ctx context.Context, event Event) error {
	if event.Rendering == nil || event.Rendering.Category != "communication" {
		return p.forward(ctx, event)
	}

	var msg commv1.CommunicationContent
	if err := p.unmarshal.Unmarshal(event.Payload, &msg); err != nil {
		return oops.Code("EMIT_CONTENT_INVALID").
			With("event_type", string(event.Type)).
			Wrap(err)
	}
	if err := p.validator.Validate(&msg); err != nil {
		return oops.Code("EMIT_CONTENT_INVALID").
			With("event_type", string(event.Type)).
			Wrap(err)
	}

	return p.forward(ctx, event)
}

// forward delegates to the inner publisher, wrapping any error so it carries
// an oops code (wrapcheck requires wrapping errors from interface methods).
func (p *ContentValidationPublisher) forward(ctx context.Context, event Event) error {
	if err := p.inner.Publish(ctx, event); err != nil {
		return oops.Code("EMIT_PUBLISH_FAILED").
			With("event_type", string(event.Type)).
			Wrap(err)
	}
	return nil
}
