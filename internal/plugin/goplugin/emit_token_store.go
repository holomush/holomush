// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"sync"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// emitTokenStore authenticates the actor input on the binary-plugin gRPC
// EmitEvent boundary. The host issues a per-dispatch random token at every
// outgoing call to a binary plugin (Host.DeliverEvent / DeliverCommand),
// stores the host-vouched actor against the token, and the plugin's SDK
// auto-ferries the token back when the plugin emits. The host's EmitEvent
// looks up the token and uses the stored actor verbatim, ignoring any
// kind/id values the plugin's metadata claims.
//
// Per spec §3.3: defense-in-depth pluginName tagging on top of 128-bit
// token entropy guards against cross-plugin token leakage. TTL =
// 60 × DefaultEventTimeout (5 min) is a generous safety margin against
// crash-without-defer paths; the deferred Revoke at the issuance site
// is the happy-path cleanup.
type emitTokenStore struct {
	mu      sync.RWMutex
	items   map[string]emitTokenEntry
	now     func() time.Time
	rand    io.Reader
	ttl     time.Duration
	sweep   time.Duration
	stop    chan struct{}
	stopped bool
}

type emitTokenEntry struct {
	pluginName string
	actor      core.Actor
	// ownerPlayerID is the host-vouched owning player ULID of the acting
	// character, carried from the command dispatch ctx (core.OwningPlayerFromContext)
	// at issuance. It is the trust anchor PLAYER-scope settings ownership compares
	// the request's principal_id against (holomush-iokti.19). Empty when the
	// dispatch had no player context (e.g. DeliverEvent), in which case PLAYER-scope
	// ownership fails closed.
	ownerPlayerID string
	// dispatch is the host-vouched DispatchContext (acting-character subject +
	// resolved scope attributes) bound to this token at issuance. The binary
	// plugin→host scope interceptor recovers it from the validated token via
	// LookupDispatch — NEVER from plugin-controlled gRPC metadata, which an
	// untrusted out-of-process plugin can forge (INV-PLUGIN-51). Zero-value when
	// the dispatch had no character context (DeliverEvent without a character
	// actor, self-tokens), which fails closed at scope-enforcement time.
	dispatch  pluginauthz.DispatchContext
	expiresAt time.Time
}

func newEmitTokenStore() *emitTokenStore {
	return &emitTokenStore{
		items: make(map[string]emitTokenEntry),
		now:   time.Now,
		rand:  rand.Reader,
		ttl:   5 * time.Minute, // 60 × DefaultEventTimeout
		sweep: 30 * time.Second,
		stop:  make(chan struct{}),
	}
}

// Issue creates a new token for an outgoing dispatch. Caller MUST defer
// Revoke or the entry will rely on TTL expiry for cleanup.
//
// ownerPlayerID is the host-vouched owning player ULID of the acting character
// (from the dispatch ctx); pass "" when there is no player context (DeliverEvent,
// self-tokens). It is stored verbatim and returned by Lookup so PLAYER-scope
// settings ownership can compare it against the request's principal_id.
//
// Returns EMIT_TOKEN_STORE_CLOSED if Close() has fired — the store is
// terminal-on-close so a host shutting down cannot keep minting tokens
// that survive into a successor's lifetime.
func (s *emitTokenStore) Issue(pluginName string, actor core.Actor, ownerPlayerID string) (string, error) {
	return s.IssueWithDispatch(pluginName, actor, ownerPlayerID, pluginauthz.DispatchContext{})
}

// IssueWithDispatch is Issue plus a host-vouched DispatchContext bound to the
// token, recovered by LookupDispatch. The binary delivery path uses it so the
// acting-character scope crosses to the plugin→host capability call via the
// unforgeable token rather than plugin-controlled metadata (INV-PLUGIN-51). Pass
// a zero DispatchContext when there is no character dispatch — fail-closed.
func (s *emitTokenStore) IssueWithDispatch(pluginName string, actor core.Actor, ownerPlayerID string, dispatch pluginauthz.DispatchContext) (string, error) {
	var buf [16]byte
	if _, err := io.ReadFull(s.rand, buf[:]); err != nil {
		return "", oops.Code("EMIT_TOKEN_ISSUE_FAILED").
			With("plugin", pluginName).
			Wrap(err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf[:])
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return "", oops.Code("EMIT_TOKEN_STORE_CLOSED").
			With("plugin", pluginName).
			Errorf("emit token store is closed")
	}
	s.items[token] = emitTokenEntry{
		pluginName:    pluginName,
		actor:         actor,
		ownerPlayerID: ownerPlayerID,
		dispatch:      dispatch,
		expiresAt:     s.now().Add(s.ttl),
	}
	s.mu.Unlock()
	return token, nil
}

// Lookup retrieves the actor and host-vouched owning player ULID stored for a
// token. Returns ok=false if the token is missing, expired, OR if the stored
// entry's pluginName does not match the caller's. All three failure modes are
// indistinguishable to callers (the security log records the specific reason at
// the call site).
//
// The returned ownerPlayerID is the value supplied at Issue ("" when the
// dispatch had no player context); PLAYER-scope settings ownership compares it
// against the request's principal_id. Callers that don't need it (Evaluate /
// EmitEvent) ignore it.
//
// pluginName tagging is defense-in-depth on top of 128-bit token entropy:
// if a future host bug ever lets plugin A's gRPC client invoke plugin B's
// server, the mismatch trips EMIT_TOKEN_REJECTED rather than allowing
// actor escalation.
func (s *emitTokenStore) Lookup(pluginName, token string) (core.Actor, string, bool) {
	s.mu.RLock()
	entry, ok := s.items[token]
	s.mu.RUnlock()
	if !ok {
		return core.Actor{}, "", false
	}
	if entry.pluginName != pluginName {
		return core.Actor{}, "", false
	}
	if !s.now().Before(entry.expiresAt) {
		return core.Actor{}, "", false
	}
	return entry.actor, entry.ownerPlayerID, true
}

// LookupDispatch retrieves the host-vouched DispatchContext bound to a token at
// issuance. Same fail-closed semantics as Lookup (missing/expired/plugin
// mismatch all return ok=false). This is the unforgeable recovery path for the
// binary plugin→host scope interceptor: the dispatch comes from the host's own
// token store keyed by the host-minted token, so an untrusted plugin cannot forge
// the acting-character subject or scope attributes by setting gRPC metadata
// (INV-PLUGIN-51). ok=true with a zero-value DispatchContext.Subject means the
// token carries no character dispatch — fail-closed downstream.
func (s *emitTokenStore) LookupDispatch(pluginName, token string) (pluginauthz.DispatchContext, bool) {
	s.mu.RLock()
	entry, ok := s.items[token]
	s.mu.RUnlock()
	if !ok || entry.pluginName != pluginName || !s.now().Before(entry.expiresAt) {
		return pluginauthz.DispatchContext{}, false
	}
	return entry.dispatch, true
}

// Revoke removes a token entry. Idempotent.
func (s *emitTokenStore) Revoke(token string) {
	s.mu.Lock()
	delete(s.items, token)
	s.mu.Unlock()
}

// Run starts the background sweeper goroutine. Terminates when ctx is
// canceled OR Close is called.
func (s *emitTokenStore) Run(ctx context.Context) {
	t := time.NewTicker(s.sweep)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-t.C:
			s.sweepExpired()
		}
	}
}

// Close stops the sweeper goroutine and clears all entries.
func (s *emitTokenStore) Close() error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	close(s.stop)
	s.items = make(map[string]emitTokenEntry)
	s.mu.Unlock()
	return nil
}

func (s *emitTokenStore) sweepExpired() {
	now := s.now()
	s.mu.Lock()
	for tok, entry := range s.items {
		if !now.Before(entry.expiresAt) {
			delete(s.items, tok)
		}
	}
	s.mu.Unlock()
}
