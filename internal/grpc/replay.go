// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// taggedEvent pairs an event with its stream name for merge-sort ordering.
type taggedEvent struct {
	stream string
	event  core.Event
}

// replayRestorePlan fetches events per-stream per-mode from the RestorePlan,
// merge-sorts them by ULID (I-15: strict global ordering during replay),
// and delivers each via sendAndCommitEvent.
func (s *CoreServer) replayRestorePlan(
	ctx context.Context,
	info *session.Info,
	plan focus.RestorePlan,
	stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
	lf *locationFollower,
) error {
	var all []taggedEvent
	for _, sm := range plan.Streams {
		events, err := s.fetchForMode(ctx, info, sm)
		if err != nil {
			slog.WarnContext(ctx, "replay fetch failed",
				"stream", sm.Stream, "mode", sm.Mode.String(), "error", err)
			continue // non-fatal: skip this stream
		}
		for _, ev := range events {
			all = append(all, taggedEvent{stream: sm.Stream, event: ev})
		}
	}

	// Merge-sort by ULID — preserves global ordering (I-15).
	sort.Slice(all, func(i, j int) bool {
		return all[i].event.ID.Compare(all[j].event.ID) < 0
	})

	for i := range all {
		te := &all[i]
		if sendErr := s.sendAndCommitEvent(ctx, info, te.stream, te.event, stream, lf); sendErr != nil {
			return oops.Code("SEND_FAILED").With("session_id", info.ID).Wrap(sendErr)
		}
	}
	return nil
}

// fetchForMode dispatches to the appropriate EventStore read method based on
// the ReplayMode in the StreamWithMode.
func (s *CoreServer) fetchForMode(
	ctx context.Context,
	info *session.Info,
	sm focus.StreamWithMode,
) ([]core.Event, error) {
	switch sm.Mode {
	case focus.ReplayModeFromCursor:
		cursor := ulid.ULID{}
		if c, ok := info.EventCursors[sm.Stream]; ok {
			cursor = c
		}
		events, err := s.eventStore.Replay(ctx, sm.Stream, cursor, s.maxReplay())
		return events, oops.With("stream", sm.Stream).Wrap(err)

	case focus.ReplayModeBoundedTail:
		events, err := s.eventStore.ReplayTail(ctx, sm.Stream, sm.TailCount, sm.NotBefore)
		return events, oops.With("stream", sm.Stream).Wrap(err)

	case focus.ReplayModeLiveOnly:
		// Advance cursor to stream tail without replaying. LastEventID returns
		// the current tail; we commit it so subsequent live-loop replays start
		// from the right point.
		tailID, err := s.eventStore.LastEventID(ctx, sm.Stream)
		if err != nil {
			if errors.Is(err, core.ErrStreamEmpty) {
				return nil, nil
			}
			return nil, oops.With("stream", sm.Stream).Wrap(err)
		}
		commitCtx, cancel := context.WithTimeout(context.Background(), cursorCommitTimeout)
		defer cancel()
		if updateErr := s.sessionStore.UpdateCursors(commitCtx, info.ID,
			map[string]ulid.ULID{sm.Stream: tailID}); updateErr != nil {
			slog.ErrorContext(ctx, "live-only cursor advance failed",
				"session_id", info.ID, "stream", sm.Stream, "error", updateErr)
		}
		return nil, nil

	default:
		return nil, oops.Errorf("unknown replay mode %d", int(sm.Mode))
	}
}

// applyCtrlUpdate handles mid-session stream add/remove from the control channel.
func (s *CoreServer) applyCtrlUpdate(
	ctx context.Context,
	info *session.Info,
	sub core.Subscription,
	ctrl sessionStreamUpdate,
	stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
	lf *locationFollower,
) error {
	if ctrl.add {
		// Reject location streams from plugins — locationFollower owns those.
		if strings.HasPrefix(ctrl.stream, world.StreamPrefixLocation) {
			slog.WarnContext(ctx, "plugin attempted to add location stream — rejected",
				"session_id", info.ID, "stream", ctrl.stream)
			return nil
		}
		if addErr := sub.AddStream(ctx, ctrl.stream); addErr != nil {
			slog.WarnContext(ctx, "mid-session stream add failed",
				"session_id", info.ID, "stream", ctrl.stream, "error", addErr)
			return nil
		}
		// Replay from cursor for the newly added stream.
		sm := focus.StreamWithMode{
			Stream:    ctrl.stream,
			Mode:      ctrl.replayMode,
			TailCount: ctrl.tailCount,
			NotBefore: ctrl.notBefore,
		}
		events, fetchErr := s.fetchForMode(ctx, info, sm)
		if fetchErr != nil {
			slog.WarnContext(ctx, "mid-session stream replay failed",
				"session_id", info.ID, "stream", ctrl.stream, "error", fetchErr)
			return nil
		}
		for _, ev := range events {
			if sendErr := s.sendAndCommitEvent(ctx, info, ctrl.stream, ev, stream, lf); sendErr != nil {
				return sendErr
			}
		}
	} else {
		if removeErr := sub.RemoveStream(ctx, ctrl.stream); removeErr != nil {
			slog.WarnContext(ctx, "mid-session stream remove failed",
				"session_id", info.ID, "stream", ctrl.stream, "error", removeErr)
		}
	}
	return nil
}
