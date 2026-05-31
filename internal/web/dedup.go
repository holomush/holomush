// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"fmt"

	lru "github.com/hashicorp/golang-lru/v2"
)

// reconnectDedupSize bounds the recent-event-id window used to suppress the
// JetStream redelivery overlap after a core-stream reconnect. The overlap is
// only the sent-but-unacked frames — core acks each delivery synchronously
// after Send (internal/grpc/server.go dispatchDelivery), so the window is
// single-digit frames in practice. 1024 is a generous bound that keeps
// per-connection memory fixed regardless of stream lifetime (holomush-rsoe6.21).
const reconnectDedupSize = 1024

// reconnectDedup is a bounded recent-id set that guards against double-forwarding
// the redelivery overlap when a core Subscribe stream reconnects. It replaces an
// unbounded session-lifetime map so a long-lived, high-traffic stream cannot grow
// per-connection memory without bound on the hot path.
//
// Not safe for concurrent use: each streamEvents call owns one instance, consulted
// only on that call's single frame-forwarding goroutine.
type reconnectDedup struct {
	seen *lru.Cache[string, struct{}]
}

// newReconnectDedup returns a dedup window holding the most recent
// reconnectDedupSize event ids.
func newReconnectDedup() *reconnectDedup {
	// lru.New only errors on size <= 0, and reconnectDedupSize is a positive
	// constant, so this cannot fail. Panic (the regexp.MustCompile idiom) guards
	// against a future edit that makes the size non-positive rather than handing
	// back a nil cache that would nil-panic on first use.
	seen, err := lru.New[string, struct{}](reconnectDedupSize)
	if err != nil {
		panic(fmt.Sprintf("web: reconnect dedup cache size %d invalid: %v", reconnectDedupSize, err))
	}
	return &reconnectDedup{seen: seen}
}

// seenOrRecord reports whether id was already forwarded within the recent window.
// A new id is recorded (evicting the oldest once the window is full) and reported
// unseen (false); an id already present is reported seen (true) so the caller skips
// the redelivery. ContainsOrAdd does the check-then-record atomically and does not
// refresh recency, so eviction stays insertion-ordered — correct here because the
// overlap is always the most recent frames. The evicted bool is irrelevant to
// dedup (the evicted id is, by construction, far older than any redelivery).
func (d *reconnectDedup) seenOrRecord(id string) bool {
	contained, _ := d.seen.ContainsOrAdd(id, struct{}{})
	return contained
}
