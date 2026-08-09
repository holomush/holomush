// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package consumer holds the shared JetStream consumer-create retry helper.
//
// It is deliberately NEUTRAL: it imports nothing from internal/eventbus/audit
// (or any other consumer-side package), so every subsystem that creates a
// durable consumer on the EVENTS stream can share one retry schedule without
// a layering inversion. Exporting the helper in place (package audit) would
// have forced the retirement reactor and the character-activity listener to
// import the audit projector purely for a backoff loop.
package consumer

import (
	"context"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// CreateBackoffs is the retry schedule for CreateOrUpdateConsumer.
// Sized to absorb JetStream's brief warmup window where the meta-leader
// is elected and the events stream is queryable but the consumer-create
// RPC can still return "no responders" or context-deadline errors under
// load (observed flake holomush-l015 — admin/policy_chain BeforeEach
// surfaced AUDIT_CONSUMER_CREATE_FAILED once across ~3 task test:int
// runs while the host was at 1.5x normal wall time). Total worst-case
// wait before giving up: ~350ms — long enough to outlast typical warmup
// jitter, short enough that a real permanent failure (config mismatch,
// stream missing) still fails fast within the surrounding test timeout.
//
// It is the DEFAULT, and nothing mutates it. A test that needs a
// microsecond schedule passes WithBackoffs to the call it is driving
// rather than swapping this slice: a package-level swap had to be
// restored via t.Cleanup, which made "MUST NOT call t.Parallel()" a
// constraint every test in every package that reached for it had to
// honour — and it was stated in only one of them.
var CreateBackoffs = []time.Duration{
	100 * time.Millisecond,
	250 * time.Millisecond,
}

// CreateOption tunes a single CreateWithRetry call.
type CreateOption func(*createOptions)

type createOptions struct {
	backoffs []time.Duration
}

// WithBackoffs overrides the retry schedule for ONE call, leaving the
// shared CreateBackoffs default untouched. This is the seam tests use to
// avoid paying the production schedule's wall time.
func WithBackoffs(backoffs ...time.Duration) CreateOption {
	return func(o *createOptions) { o.backoffs = backoffs }
}

// CreateWithRetry invokes create with bounded retries from CreateBackoffs.
// Returns the first success, the last error after the budget is exhausted,
// or the last error if ctx is cancelled mid-backoff. Retries on any
// non-nil error — the cost of retrying a truly permanent error (config
// mismatch, missing stream) is bounded by the total backoff (~350ms) and
// the diagnostic cost of differentiating transient vs permanent error
// classes exceeds the savings.
//
// Callers (each wraps the returned error with its OWN oops Code — this
// helper deliberately codes nothing, so relocating it cannot change any
// caller's error surface):
//
//   - the host audit projector (AUDIT_CONSUMER_CREATE_FAILED)
//   - the plugin consumer manager (AUDIT_PLUGIN_CONSUMER_CREATE_FAILED)
//   - the retirement reactor (plan 03-04)
//   - the character-activity listener (plan 03-05)
//
// D-55 designates this wrapper as the intended stamp site for background-job
// event provenance. 02.2 landed its provenance triple on the world-side
// caller instead (internal/world/caller.go, world.Provenance /
// world.JobCaller), so NOTHING is stamped here today — this pointer exists
// so a future reader looking for the stamp site finds where it actually
// lives rather than concluding it was lost.
func CreateWithRetry(
	ctx context.Context,
	create func(context.Context) (jetstream.Consumer, error),
	opts ...CreateOption,
) (jetstream.Consumer, error) {
	o := createOptions{backoffs: CreateBackoffs}
	for _, opt := range opts {
		opt(&o)
	}

	var lastErr error
	for attempt := 0; attempt <= len(o.backoffs); attempt++ {
		cons, err := create(ctx)
		if err == nil {
			return cons, nil
		}
		lastErr = err
		if attempt == len(o.backoffs) {
			break
		}
		if ctx.Err() != nil {
			return nil, lastErr
		}
		select {
		case <-time.After(o.backoffs[attempt]):
		case <-ctx.Done():
			return nil, lastErr
		}
	}
	return nil, lastErr
}
