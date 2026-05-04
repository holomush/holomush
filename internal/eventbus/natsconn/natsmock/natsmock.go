// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package natsmock provides a mock implementation of natsconn.Conn for
// unit tests that exercise specific publish/subscribe error paths
// without standing up an embedded NATS server.
//
// SCOPE: this mock intentionally does NOT implement broker-level
// semantics (subject routing, queue groups, replay, request-reply
// delivery, JetStream, etc.). It exists to drive deterministic
// failure paths in callers — e.g. "what does the Coordinator do when
// PublishRequest returns ErrConnectionClosed?" — without the
// behavioral fidelity required for end-to-end tests. Tests that need
// real protocol behavior MUST keep using eventbustest's embedded
// server.
//
// BIASES, by design:
//   - Subscribe / SubscribeSync return a real *nats.Subscription
//     created against an embedded loopback server is OUT OF SCOPE.
//     Callers that require a non-nil *nats.Subscription should use
//     the embedded server. The mock returns nil + nil by default and
//     the caller will (rightly) blow up if it dereferences. To get a
//     pre-canned error from these methods, set the matching Hook.
//   - Publish / PublishRequest record the call and return the canned
//     error from the matching Hook.
//   - RequestWithContext returns the canned (msg, err) from
//     RequestWithContextHook.
//   - NewRespInbox returns a deterministic incrementing inbox so tests
//     can assert against it without hitting nats.NewInbox().
//   - Flush returns the canned error from FlushHook.
//
// CONCURRENCY: all hooks and recorded fields are guarded by a single
// mutex; the mock is safe for concurrent use across goroutines started
// by the unit-under-test, which matches how *nats.Conn is normally
// shared.
package natsmock

import (
	"context"
	"strconv"
	"sync"

	"github.com/nats-io/nats.go"

	"github.com/holomush/holomush/internal/eventbus/natsconn"
)

// Conn is a mock natsconn.Conn for unit tests. The zero value is
// usable; nil hooks short-circuit to safe defaults (return nil error,
// return nil subscription, etc.).
type Conn struct {
	mu sync.Mutex

	// SubscribeHook is invoked on Subscribe; if non-nil its return is
	// used directly, bypassing the default (nil, nil) shortcut.
	SubscribeHook func(subject string, cb nats.MsgHandler) (*nats.Subscription, error)
	// SubscribeSyncHook is invoked on SubscribeSync; same semantics.
	SubscribeSyncHook func(subject string) (*nats.Subscription, error)
	// PublishHook is invoked on Publish; non-nil hook return overrides
	// the default nil-error.
	PublishHook func(subject string, data []byte) error
	// PublishRequestHook is invoked on PublishRequest; same semantics.
	PublishRequestHook func(subject, reply string, data []byte) error
	// RequestWithContextHook is invoked on RequestWithContext; same.
	RequestWithContextHook func(ctx context.Context, subject string, data []byte) (*nats.Msg, error)
	// FlushHook is invoked on Flush; same.
	FlushHook func() error
	// NewRespInboxHook is invoked on NewRespInbox; if nil, a
	// deterministic incrementing string is returned ("_INBOX.mock.1",
	// "_INBOX.mock.2", ...) so test assertions are stable.
	NewRespInboxHook func() string

	// Recorded calls. Tests inspect these to assert call patterns.
	Subscribed         []SubscribeCall
	SubscribedSync     []string
	Published          []PublishCall
	PublishedRequests  []PublishRequestCall
	RequestedWithCtx   []RequestWithContextCall
	NewRespInboxCalls  int
	FlushCalls         int
	respInboxCounter   int
}

// SubscribeCall records the args passed to Subscribe.
type SubscribeCall struct {
	Subject string
	Handler nats.MsgHandler
}

// PublishCall records the args passed to Publish.
type PublishCall struct {
	Subject string
	Data    []byte
}

// PublishRequestCall records the args passed to PublishRequest.
type PublishRequestCall struct {
	Subject string
	Reply   string
	Data    []byte
}

// RequestWithContextCall records the args passed to RequestWithContext.
type RequestWithContextCall struct {
	Subject string
	Data    []byte
}

// Compile-time assertion.
var _ natsconn.Conn = (*Conn)(nil)

// Subscribe records the call and dispatches to SubscribeHook (if set).
func (c *Conn) Subscribe(subject string, cb nats.MsgHandler) (*nats.Subscription, error) {
	c.mu.Lock()
	c.Subscribed = append(c.Subscribed, SubscribeCall{Subject: subject, Handler: cb})
	hook := c.SubscribeHook
	c.mu.Unlock()
	if hook != nil {
		return hook(subject, cb)
	}
	return nil, nil
}

// SubscribeSync records the call and dispatches to SubscribeSyncHook.
func (c *Conn) SubscribeSync(subject string) (*nats.Subscription, error) {
	c.mu.Lock()
	c.SubscribedSync = append(c.SubscribedSync, subject)
	hook := c.SubscribeSyncHook
	c.mu.Unlock()
	if hook != nil {
		return hook(subject)
	}
	return nil, nil
}

// Publish records the call and dispatches to PublishHook.
func (c *Conn) Publish(subject string, data []byte) error {
	c.mu.Lock()
	// Copy data to avoid aliasing — callers may reuse the buffer.
	cp := append([]byte(nil), data...)
	c.Published = append(c.Published, PublishCall{Subject: subject, Data: cp})
	hook := c.PublishHook
	c.mu.Unlock()
	if hook != nil {
		return hook(subject, data)
	}
	return nil
}

// PublishRequest records the call and dispatches to PublishRequestHook.
func (c *Conn) PublishRequest(subject, reply string, data []byte) error {
	c.mu.Lock()
	cp := append([]byte(nil), data...)
	c.PublishedRequests = append(c.PublishedRequests, PublishRequestCall{Subject: subject, Reply: reply, Data: cp})
	hook := c.PublishRequestHook
	c.mu.Unlock()
	if hook != nil {
		return hook(subject, reply, data)
	}
	return nil
}

// RequestWithContext records the call and dispatches to
// RequestWithContextHook.
func (c *Conn) RequestWithContext(ctx context.Context, subject string, data []byte) (*nats.Msg, error) {
	c.mu.Lock()
	cp := append([]byte(nil), data...)
	c.RequestedWithCtx = append(c.RequestedWithCtx, RequestWithContextCall{Subject: subject, Data: cp})
	hook := c.RequestWithContextHook
	c.mu.Unlock()
	if hook != nil {
		return hook(ctx, subject, data)
	}
	return nil, nil
}

// NewRespInbox records the call and dispatches to NewRespInboxHook,
// or returns a deterministic incrementing inbox subject.
func (c *Conn) NewRespInbox() string {
	c.mu.Lock()
	c.NewRespInboxCalls++
	hook := c.NewRespInboxHook
	c.respInboxCounter++
	counter := c.respInboxCounter
	c.mu.Unlock()
	if hook != nil {
		return hook()
	}
	return "_INBOX.mock." + strconv.Itoa(counter)
}

// Flush records the call and dispatches to FlushHook.
func (c *Conn) Flush() error {
	c.mu.Lock()
	c.FlushCalls++
	hook := c.FlushHook
	c.mu.Unlock()
	if hook != nil {
		return hook()
	}
	return nil
}
