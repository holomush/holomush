// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// export_test.go exposes Handler internals to the readstream_test package
// without widening the production API. testHandler wraps a real Handler and
// surfaces handleInternal so unit tests can drive it with a recording
// stream fake instead of a real connect.ServerStream. RecordingStream is
// the canonical send-capture fake used by all unit tests in this package.

package readstream

import (
	"context"
	"sync"

	"github.com/holomush/holomush/internal/eventbus"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// TestHandler wraps a Handler and exposes its package-private surface
// (handleInternal + streamSender) for in-package tests via the
// readstream_test package. Production code MUST NOT depend on this type.
type TestHandler struct {
	*Handler
}

// NewTestHandler constructs a TestHandler. Validation errors propagate
// from NewHandler.
func NewTestHandler(cfg Config) (*TestHandler, error) {
	h, err := NewHandler(cfg)
	if err != nil {
		return nil, err
	}
	return &TestHandler{Handler: h}, nil
}

// HandleInternalForTest drives the internal handler flow against a
// recording stream. Equivalent to HandleAdminReadStream minus the
// connect.ServerStream adapter.
func (t *TestHandler) HandleInternalForTest(
	ctx context.Context,
	req *adminv1.AdminReadStreamRequest,
	stream StreamSenderForTest,
) error {
	return t.handleInternal(ctx, req, stream)
}

// StreamSenderForTest is the exported alias of the package-private
// streamSender interface so the readstream_test package can declare fakes
// that satisfy it without exporting streamSender for production callers.
type StreamSenderForTest interface {
	Send(*adminv1.AdminReadStreamResponse) error
}

// RecordingStream captures every frame the handler sends. Thread-safe so
// tests can drive concurrent flows (e.g. dual-control approval arriving
// while the handler waits). SendErrAt, when set, makes the Nth Send
// (1-indexed) return SendErr; subsequent sends succeed as usual.
type RecordingStream struct {
	mu        sync.Mutex
	sends     []*adminv1.AdminReadStreamResponse
	SendErrAt int   // 0 = never err; 1 = first send errors; etc.
	SendErr   error // populated when SendErrAt fires
}

// Send records the frame and (optionally) returns SendErr at the
// configured index.
func (r *RecordingStream) Send(resp *adminv1.AdminReadStreamResponse) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sends = append(r.sends, resp)
	if r.SendErrAt > 0 && len(r.sends) == r.SendErrAt {
		return r.SendErr
	}
	return nil
}

// Frames returns a copy of the recorded frames. Safe to call concurrently
// with Send.
func (r *RecordingStream) Frames() []*adminv1.AdminReadStreamResponse {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*adminv1.AdminReadStreamResponse, len(r.sends))
	copy(out, r.sends)
	return out
}

// Len reports the number of recorded frames.
func (r *RecordingStream) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sends)
}

// BuildSQLForTest exposes the cold reader's package-private buildSQL for
// in-package tests. Lets a test assert dek_ref IS NOT NULL appears in the
// SQL without standing up a real pgxpool.
func (c *ColdReader) BuildSQLForTest(q ColdQuery) (string, []any, error) {
	return c.buildSQL(q)
}

// BuildMetadataOnlyEventFrameForTest exposes the package-private metadata-only
// frame builder. Used by handler_test.go to assert that metadata-only frames
// carry typed redaction fields (INV-CRYPTO-62).
func BuildMetadataOnlyEventFrameForTest(row ColdRow, reason eventbus.NoPlaintextReason) *adminv1.AdminReadStreamResponse {
	return buildMetadataOnlyEventFrame(row, reason)
}

// BuildPlaintextEventFrameForTest exposes the package-private plaintext frame
// builder. Used by handler_test.go to assert that the plaintext frame
// satisfies the metadata_only=false / no_plaintext_reason=UNSPECIFIED contract.
func BuildPlaintextEventFrameForTest(row ColdRow, plaintext []byte) *adminv1.AdminReadStreamResponse {
	return buildPlaintextEventFrame(row, plaintext)
}

// ClassifyTerminatorForTest exposes the package-private classifier so the
// priority order (oops codes win over context.* errors) can be tested
// without driving the full handler flow.
func ClassifyTerminatorForTest(err error) adminv1.ReadFinished_TerminatedBy {
	return classifyTerminator(err)
}

// ComputeReadStreamArgsHashForTest exposes the package-private hash helper
// so tests can assert the resolved-bounds semantics without driving the full
// handler flow.
func ComputeReadStreamArgsHashForTest(resolved Resolved) ([]byte, error) {
	return computeReadStreamArgsHash(resolved)
}
