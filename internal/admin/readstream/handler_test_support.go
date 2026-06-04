// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// handler_test_support.go exposes a narrow test-support API to callers
// outside the readstream package — specifically, E2E tests in
// cmd/holomush that need to drive Handler.handleInternal with an
// injected StreamSender (e.g. to inject a per-frame sleep for INV-CRYPTO-64
// write-deadline tests, or to drive idempotent dual-control reuse with
// a non-UDS session adapter).
//
// These types are NOT marked _test.go because Go's package model only
// makes _test.go symbols visible within the same package's test binary.
// External-package E2E tests can't see them without a public surface.
//
// Production code MUST NOT depend on these types. Linters guard against
// regression: a future production-code import of NewExternalTestHandler
// or RecordingStream is a code-review red flag.
//
// Equivalent in-package helpers live in export_test.go for unit tests
// inside readstream_test.

package readstream

import (
	"context"
	"sync"

	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// ExternalTestHandler wraps a Handler and exposes the package-private
// handleInternal surface to external-package tests. Mirrors the
// in-package TestHandler shape but lives in production-visible code so
// cmd/holomush integration tests can use it.
type ExternalTestHandler struct {
	*Handler
}

// NewExternalTestHandler constructs an ExternalTestHandler. Validation
// errors propagate from NewHandler.
//
// Production callers MUST NOT use this constructor. The wrapper exists
// solely to give external-package tests access to handleInternal with
// an injected StreamSender for fault-injection scenarios (F-E10, F-E16).
func NewExternalTestHandler(cfg Config) (*ExternalTestHandler, error) {
	h, err := NewHandler(cfg)
	if err != nil {
		return nil, err
	}
	return &ExternalTestHandler{Handler: h}, nil
}

// HandleInternalForExternalTest drives the internal handler flow against
// an injected stream sender. Equivalent to HandleAdminReadStream minus
// the connect.ServerStream adapter.
func (t *ExternalTestHandler) HandleInternalForExternalTest(
	ctx context.Context,
	req *adminv1.AdminReadStreamRequest,
	stream ExternalStreamSender,
) error {
	return t.handleInternal(ctx, req, stream)
}

// ExternalStreamSender is the public alias of the package-private
// streamSender interface so external-package tests can declare fakes
// that satisfy it without exporting streamSender for production callers.
type ExternalStreamSender interface {
	Send(*adminv1.AdminReadStreamResponse) error
}

// ExternalRecordingStream captures every frame the handler sends, exposed
// for external-package tests. Thread-safe so tests can drive concurrent
// flows (e.g. dual-control approval arriving while the handler waits).
//
// Mirrors RecordingStream in export_test.go but lives in production-
// visible code for external-package consumption.
type ExternalRecordingStream struct {
	mu    sync.Mutex
	sends []*adminv1.AdminReadStreamResponse
}

// Send records the frame. Thread-safe.
func (r *ExternalRecordingStream) Send(resp *adminv1.AdminReadStreamResponse) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sends = append(r.sends, resp)
	return nil
}

// Frames returns a copy of the recorded frames. Safe to call concurrently
// with Send.
func (r *ExternalRecordingStream) Frames() []*adminv1.AdminReadStreamResponse {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*adminv1.AdminReadStreamResponse, len(r.sends))
	copy(out, r.sends)
	return out
}

// Len reports the number of recorded frames.
func (r *ExternalRecordingStream) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sends)
}
