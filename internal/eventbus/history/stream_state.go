// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"sync"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
)

// StreamStateSnapshot caches Stream.Info().State for the lifetime of a
// single Reader.QueryHistory call. Per spec §8.5.
//
// Used by:
//   - selectStartTier (route the first page based on cursor.Seq vs FirstSeq)
//   - cold tier validation (distinguish LAG from STALE when cursor seq
//     is missing from cold)
//   - hot tier (detect retention age-out)
//
// Concurrency: a single crossoverStream instance is not invoked
// concurrently (loadNextPage is sequential), so a sync.Once is sufficient
// to memoize the first fetch.
type StreamStateSnapshot struct {
	js   jetstream.JetStream
	once sync.Once
	// Populated by once.Do.
	firstSeq uint64
	lastSeq  uint64
	err      error
}

func newStreamStateSnapshot(js jetstream.JetStream) *StreamStateSnapshot {
	return &StreamStateSnapshot{js: js}
}

// Get returns the cached state, fetching it on first call. Safe to call
// concurrently within a single QueryHistory call.
//
// Returns (0, 0, nil) if the snapshot has no JS client (e.g., in unit
// tests that bypass the hot tier) — callers should treat "no snapshot"
// as "can't distinguish LAG from STALE" and fall back to STALE.
func (s *StreamStateSnapshot) Get(ctx context.Context) (firstSeq, lastSeq uint64, err error) {
	if s == nil {
		return 0, 0, nil
	}
	s.once.Do(func() {
		// js is nil in tests that pre-populate via newSnapshotForTest; those
		// tests call once.Do to mark as done before calling Get, so this
		// body won't execute for them.
		if s.js == nil {
			return
		}
		stream, fErr := s.js.Stream(ctx, eventbus.StreamName)
		if fErr != nil {
			s.err = oops.Code("EVENTBUS_HISTORY_STREAM_LOOKUP_FAILED").
				With("stream", eventbus.StreamName).
				Wrap(fErr)
			return
		}
		info, fErr := stream.Info(ctx)
		if fErr != nil {
			s.err = oops.Code("EVENTBUS_HISTORY_STREAM_INFO_FAILED").
				With("stream", eventbus.StreamName).
				Wrap(fErr)
			return
		}
		s.firstSeq = info.State.FirstSeq
		s.lastSeq = info.State.LastSeq
	})
	return s.firstSeq, s.lastSeq, s.err
}
