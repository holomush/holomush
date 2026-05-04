// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package natsconn defines the narrow interface seam that production
// callers (cluster.Registry, invalidation.Coordinator) use in place of
// a concrete *nats.Conn. The seam exists so unit tests MAY substitute
// a lightweight mock instead of standing up an embedded NATS server.
//
// Scope (holomush-ojw1.3.23): the interface covers ONLY the methods
// HoloMUSH actually calls on *nats.Conn today. Adding a method to the
// interface MUST NOT happen until an in-tree caller needs it; the goal
// is to keep the mock-vs-real protocol surface as small as possible
// to minimize the risk of subtle behavioral divergence between mock
// and embedded server.
//
// Production wiring: every consumer continues to receive a *nats.Conn
// at construction (the interface is satisfied implicitly via
// structural typing). Test wiring: callers MAY pass a mock conforming
// to the interface; see internal/eventbus/natsconn/natsmock for one
// such mock geared to publish/subscribe-based unit tests.
//
// The eventbustest package's embedded-server philosophy
// ("in-process is OK; correctness over fakery") is unchanged. This
// seam complements eventbustest — it does not replace it. Integration
// tests under test/integration/ MUST keep using the real embedded
// server.
package natsconn

import (
	"context"

	"github.com/nats-io/nats.go"
)

// Conn is the narrow seam. It is satisfied implicitly by *nats.Conn
// (structural typing), so no production wiring change is required at
// the call site beyond converting a struct field's type from
// *nats.Conn to natsconn.Conn.
//
// Methods are grouped by call-site usage:
//
//   - Subscribe / SubscribeSync — receive-side wiring (cluster
//     heartbeat handlers, invalidation Coordinator wildcard listener,
//     invalidation Coordinator inbox).
//   - Publish / PublishRequest — fire-and-forget and request-reply
//     publish (heartbeat alive, bye, probe-reply, pill, invalidation
//     publish, invalidation reply).
//   - RequestWithContext — the cluster registry's probe path
//     (single-target request-reply with derived timeout).
//   - NewRespInbox — invalidation Coordinator's per-publish reply
//     inbox subject generator.
//   - Flush — heartbeat best-effort drain on Stop.
//
// CAVEAT: nats.go does not currently export an interface that captures
// these methods, so we define our own. If nats.go ever exposes one,
// this package SHOULD be deprecated in favor of the upstream interface.
type Conn interface {
	// Subscribe registers an asynchronous handler on a subject. Mirrors
	// (*nats.Conn).Subscribe.
	Subscribe(subject string, cb nats.MsgHandler) (*nats.Subscription, error)

	// SubscribeSync registers a synchronous (NextMsg-style) subscription.
	// Mirrors (*nats.Conn).SubscribeSync.
	SubscribeSync(subject string) (*nats.Subscription, error)

	// Publish sends a fire-and-forget message. Mirrors (*nats.Conn).Publish.
	Publish(subject string, data []byte) error

	// PublishRequest sends a request that expects replies on `reply`.
	// Mirrors (*nats.Conn).PublishRequest.
	PublishRequest(subject, reply string, data []byte) error

	// RequestWithContext sends a request and awaits one reply (or the
	// context's deadline / cancellation). Mirrors
	// (*nats.Conn).RequestWithContext.
	RequestWithContext(ctx context.Context, subject string, data []byte) (*nats.Msg, error)

	// NewRespInbox returns a globally-unique reply inbox subject.
	// Mirrors (*nats.Conn).NewRespInbox.
	NewRespInbox() string

	// Flush blocks until all pending messages on the connection have
	// been processed by the server. Mirrors (*nats.Conn).Flush.
	Flush() error
}

// Compile-time assertion: *nats.Conn satisfies Conn. If nats.go ever
// changes a method signature this assertion will fail at build time
// rather than at runtime.
var _ Conn = (*nats.Conn)(nil)
