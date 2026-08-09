// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charactivity

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/errutil"
)

// activityKV is the KV surface this package uses.
//
// DeleteRevision takes the revision EXPLICITLY rather than jetstream's
// variadic KVDeleteOpt because those options are OPAQUE (jetstream's
// deleteOpts type is unexported), so a test fake could not observe which
// revision the flusher guarded on — and that revision is precisely the
// property the guard exists to protect. The production adapter below is the
// only place the option is constructed.
type activityKV interface {
	Put(ctx context.Context, key string, value []byte) (uint64, error)
	Get(ctx context.Context, key string) (jetstream.KeyValueEntry, error)
	// Delete removes a key unconditionally. Reserved for keys that can NEVER
	// become flushable (see drainKey).
	Delete(ctx context.Context, key string) error
	// DeleteRevision removes a key only while its latest revision is still the
	// given one.
	DeleteRevision(ctx context.Context, key string, revision uint64) error
	ListKeys(ctx context.Context) (jetstream.KeyLister, error)
}

// jsKV adapts a live jetstream.KeyValue to activityKV.
type jsKV struct{ kv jetstream.KeyValue }

func (j jsKV) Put(ctx context.Context, key string, value []byte) (uint64, error) {
	return j.kv.Put(ctx, key, value) //nolint:wrapcheck // the caller codes it
}

func (j jsKV) Get(ctx context.Context, key string) (jetstream.KeyValueEntry, error) {
	return j.kv.Get(ctx, key) //nolint:wrapcheck // the caller codes it
}

func (j jsKV) Delete(ctx context.Context, key string) error {
	return j.kv.Delete(ctx, key) //nolint:wrapcheck // the caller codes it
}

// DeleteRevision is the R1 concurrency guarantee, in one line.
//
// jetstream.LastRevision stamps Nats-Expected-Last-Subject-Sequence, so the
// server refuses the delete marker when the latest revision is no longer the
// one the flusher read. That refusal is what makes a listener Put landing
// mid-flush survivable rather than destroyed.
func (j jsKV) DeleteRevision(ctx context.Context, key string, revision uint64) error {
	return j.kv.Delete(ctx, key, jetstream.LastRevision(revision)) //nolint:wrapcheck // the caller codes it
}

func (j jsKV) ListKeys(ctx context.Context) (jetstream.KeyLister, error) {
	return j.kv.ListKeys(ctx) //nolint:wrapcheck // the caller codes it
}

// flusher drains the buffer into characters.last_active_at on a fixed tick.
type flusher struct {
	cfg Config
	kv  activityKV
}

// run ticks until the context is cancelled. The first drain happens one full
// interval in — a drain at t=0 would race the listener's own startup for no
// benefit, since the buffer is durable and nothing is lost by waiting.
func (f *flusher) run(ctx context.Context) {
	ticker := time.NewTicker(f.cfg.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.drain(ctx)
		}
	}
}

// drain flushes every currently-buffered key.
//
// ListKeys (streaming), never Keys (which loads every key into memory). Its own
// doc warns that duplicate keys may be reported under churn; the second sighting
// simply finds the key gone, and the monotonic writer would absorb it even if it
// did not.
func (f *flusher) drain(ctx context.Context) {
	lister, err := f.kv.ListKeys(ctx)
	if err != nil {
		errutil.LogErrorContext(ctx, "character activity flush could not list the buffer",
			oops.Code("CHARACTER_ACTIVITY_LIST_FAILED").With("bucket", BucketName).Wrap(err))
		return
	}
	defer func() { _ = lister.Stop() }() //nolint:errcheck // a lister close failure changes nothing the caller can act on

	flushed := 0
	for key := range lister.Keys() {
		if ctx.Err() != nil {
			return
		}
		if f.drainKey(ctx, key) {
			flushed++
		}
	}
	if flushed > 0 {
		slog.DebugContext(ctx, "character activity flushed", "keys", flushed)
	}
}

// drainKey flushes one buffered key, reporting whether the column advanced.
//
// The read/write/delete triple is deliberately NOT atomic — it does not need to
// be. The delete is conditioned on the revision the read observed, so the only
// interleaving that matters (a listener Put arriving between them) makes the
// delete fail and LEAVES the key. The already-written older timestamp is
// harmless: the writer is monotonic, so the next tick's newer value wins.
func (f *flusher) drainKey(ctx context.Context, key string) bool {
	entry, err := f.kv.Get(ctx, key)
	switch {
	case errors.Is(err, jetstream.ErrKeyNotFound):
		return false // already flushed this pass (a duplicate ListKeys sighting)
	case err != nil:
		errutil.LogErrorContext(ctx, "character activity flush could not read a buffered key",
			oops.Code("CHARACTER_ACTIVITY_READ_FAILED").With("key", key).Wrap(err))
		return false
	}

	id, err := ulid.Parse(entry.Key())
	if err != nil {
		// A key NAME is fixed for the life of the key, so one that is not a
		// ULID can never become flushable. It is purged unconditionally —
		// there is no future Put a revision guard would be protecting.
		slog.WarnContext(ctx, "character activity flush purging a key that is not a character id", "key", key)
		if delErr := f.kv.Delete(ctx, key); delErr != nil {
			errutil.LogErrorContext(ctx, "character activity flush could not purge a malformed key",
				oops.Code("CHARACTER_ACTIVITY_DELETE_FAILED").With("key", key).Wrap(delErr))
		}
		return false
	}

	nanos, err := strconv.ParseInt(string(entry.Value()), 10, 64)
	if err != nil {
		// The VALUE, unlike the key name, could be cured by a later Put — so
		// this drop is still revision-conditional. It bounds the bucket
		// without ever destroying newer activity.
		slog.WarnContext(ctx, "character activity flush dropping an unparsable buffered value",
			"character_id", id.String())
		f.deleteAtRevision(ctx, key, entry.Revision())
		return false
	}

	if err := f.cfg.Writer(ctx, id, nanos); err != nil {
		// Leave the key: the next tick retries, and a stale re-flush is a
		// no-op against the monotonic guard.
		errutil.LogErrorContext(ctx, "character activity flush could not write a character's last-active time", err)
		return false
	}
	f.deleteAtRevision(ctx, key, entry.Revision())
	return true
}

// deleteAtRevision removes a flushed key, but only while it still holds the
// revision the flush read.
func (f *flusher) deleteAtRevision(ctx context.Context, key string, revision uint64) {
	if err := f.kv.DeleteRevision(ctx, key, revision); err != nil {
		// NOT a failure. The overwhelmingly likely cause is a listener Put that
		// landed mid-flush, which is exactly what the guard is for: the key
		// stays, carrying the NEWER timestamp, and the next tick flushes it.
		slog.DebugContext(ctx, "character activity flush left a key that changed mid-flush",
			"key", key, "revision", revision, "reason", err.Error())
	}
}
