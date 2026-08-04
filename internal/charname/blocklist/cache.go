// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package blocklist

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/store"
)

// DefaultKey is the settings key holding the character-name block list. It is
// seeded as the empty JSON array `[]` by migration 000054; the shipped list is
// what the operator wrote and nothing else.
const DefaultKey = "core.character.name.blocklist"

// RawGetter reads one settings value in its RAW stored form, distinguishing a
// missing row (store.ErrSystemInfoNotFound) from every other failure.
//
// It is deliberately the raw single-value read rather than the game-settings
// convenience layer — see Load for why that distinction is load-bearing.
// Satisfied by *store.PostgresEventStore.
type RawGetter interface {
	GetSystemInfo(ctx context.Context, key string) (string, error)
}

// Load reads and strictly decodes the block-list value stored under key.
//
// It distinguishes four cases, and the distinction is the whole point:
//
//   - no such row        → (nil, nil). An unconfigured list is a valid empty
//     list. It is not a failure and it is not "block everything".
//   - a JSON array of strings → that slice, nil error. `[]` yields an empty
//     list, which rejects nothing.
//   - invalid JSON, a JSON scalar, or an array with a non-string element →
//     BLOCKLIST_VALUE_MALFORMED naming the key. NEVER read as absent.
//   - a database failure → wrapped and returned, never flattened into
//     "absent".
//
// # Why this does NOT call settings.StringSliceN
//
// settings.StringSliceN (internal/settings/game.go) returns (nil, false) for a
// MISSING key AND for a json.Unmarshal failure, logging the parse error at
// Debug where nothing reads it. The two cases are indistinguishable to its
// caller. A loader built on it therefore reads a malformed value as "empty
// list", which silently disables the entire block list — and in v0.13 the only
// edit path for this key is direct SQL (01-SPEC §8.12 ships no editing
// surface), so a single fat-fingered UPDATE would do it, with no error, no
// warning at Debug-off, and every test still green.
//
// D-15 requires the opposite: a hard startup failure naming the offending
// entry. Hence the strict decode below. Do not "simplify" this back to
// StringSliceN.
//
// The two-stage decode — []json.RawMessage first, then each element as a
// string — is what makes `["ok", 7]` a malformed ELEMENT rather than a
// silently coerced one.
func Load(ctx context.Context, raw RawGetter, key string) ([]string, error) {
	value, err := raw.GetSystemInfo(ctx, key)
	if err != nil {
		if errors.Is(err, store.ErrSystemInfoNotFound) {
			return nil, nil
		}
		return nil, oops.Code("BLOCKLIST_VALUE_UNREADABLE").With("key", key).Wrap(err)
	}

	var elems []json.RawMessage
	if decErr := json.Unmarshal([]byte(value), &elems); decErr != nil {
		return nil, oops.Code("BLOCKLIST_VALUE_MALFORMED").With("key", key).Wrap(decErr)
	}

	out := make([]string, 0, len(elems))
	for i, e := range elems {
		var s string
		if elemErr := json.Unmarshal(e, &s); elemErr != nil {
			return nil, oops.
				Code("BLOCKLIST_VALUE_MALFORMED").
				With("key", key).
				With("index", i).
				Wrap(elemErr)
		}
		out = append(out, s)
	}
	return out, nil
}

// Cache holds the currently-installed block-list Snapshot and refreshes it on
// demand. It is safe for concurrent use.
//
// # Why the Cache, and not a Snapshot, is what a Gate holds
//
// Match reads the CURRENT snapshot on every call, so a charname.Gate
// constructed once at boot sees every later reload without being re-wired. A
// Gate handed a *Snapshot instead would freeze the list for the process
// lifetime while the poller refreshed a value nothing reads — silent, with no
// failing test and no log line.
//
// # Why there is no read barrier
//
// policy.Cache carries a one-shot read barrier so that Snapshot callers block
// during an Invalidate-driven reload. That machinery exists to serve an
// IN-PROCESS writer (the policy store's OnMutate hook). This list has no
// in-process writer by construction — v0.13's only edit path is direct SQL, on
// any replica, which is precisely why refresh is a poll rather than an
// invalidation.
//
// Engaging a barrier here would also be actively harmful rather than merely
// dead: a reload is a database round trip, the previously-compiled list stays
// fully valid throughout it (D-16's "last valid list stays in force"), and
// blocking readers would turn one poll's DB latency into character-name
// admission latency for no correctness gain. The RWMutex-guarded
// compile-then-swap below already gives readers the property that matters —
// every reader observes one whole immutable snapshot, never a torn one.
type Cache struct {
	// raw is resolved lazily on each reload: the subsystem is constructed
	// before the database subsystem has a pool, and Matcher() must be stable
	// from construction onward, so the getter cannot be captured eagerly.
	raw func() RawGetter
	key string

	mu       sync.RWMutex
	snapshot *Snapshot
}

// NewCache creates a Cache over the settings row at key. The cache starts
// EMPTY — matching nothing — until the first successful Reload. An empty cache
// is never read as "block everything"; the subsystem's Prepare is what turns a
// bad list into a startup failure.
func NewCache(raw func() RawGetter, key string) *Cache {
	if key == "" {
		key = DefaultKey
	}
	return &Cache{raw: raw, key: key, snapshot: &Snapshot{}}
}

// Key returns the settings key this cache reads.
func (c *Cache) Key() string { return c.key }

// Reload reads the configured value, compiles it, and swaps the snapshot —
// ONLY on success.
//
// Compile-then-swap is the ordering that matters: a malformed value or an
// uncompilable pattern returns the error and leaves the previously installed
// snapshot untouched and enforcing (D-16). A cache that swapped first would
// leave an EMPTY list in force after a bad edit, which reads as "no list
// configured" and silently admits every name the operator meant to block.
func (c *Cache) Reload(ctx context.Context) error {
	patterns, err := Load(ctx, c.raw(), c.key)
	if err != nil {
		return err
	}

	snap, err := Compile(patterns)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.snapshot = snap
	c.mu.Unlock()
	return nil
}

// Snapshot returns the currently-installed immutable snapshot. Callers hold
// the returned value for the whole of an evaluation, so a reload racing them
// can never expose a partially-compiled list.
func (c *Cache) Snapshot() *Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}

// Match satisfies charname.BlockList by delegating to the CURRENT snapshot.
// This is the method that makes a reload visible to an already-constructed
// Gate.
func (c *Cache) Match(normalized string) (blocked bool, index int) {
	return c.Snapshot().Match(normalized)
}
