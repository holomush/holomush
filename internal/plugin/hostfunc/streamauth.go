// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// authorizingHistoryReader wraps a HistoryReader with the same instance-level
// ABAC gate the host.v1 StreamHistoryService handler runs
// (pluginauthz.AuthorizeStreamRead), so the ambient Lua
// holomush.query_stream_history path enforces identically to the brokered path
// (plugin-runtime-symmetry). Without this the ambient hostfunc reached
// ReplayTail with zero ABAC, letting a Lua plugin read events.<gid>.system.* /
// audit.* / crypto streams (holomush-xakba).
type authorizingHistoryReader struct {
	inner      HistoryReader
	engine     types.AccessPolicyEngine
	auditor    pluginauthz.Auditor
	gameID     string
	pluginName string
}

// newAuthorizingHistoryReader wraps inner with the stream-read gate. Returns nil
// when inner is nil so the caller's nil-reader no-op path is preserved.
func newAuthorizingHistoryReader(inner HistoryReader, engine types.AccessPolicyEngine, auditor pluginauthz.Auditor, gameID, pluginName string) HistoryReader {
	if inner == nil {
		return nil
	}
	return &authorizingHistoryReader{
		inner:      inner,
		engine:     engine,
		auditor:    auditor,
		gameID:     gameID,
		pluginName: pluginName,
	}
}

func (r *authorizingHistoryReader) ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]eventbus.Event, error) {
	dec, err := pluginauthz.AuthorizeStreamRead(ctx, pluginauthz.StreamReadInput{
		Engine:     r.engine,
		Auditor:    r.auditor,
		PluginName: r.pluginName,
		Subject:    access.PluginSubject(r.pluginName),
		GameID:     r.gameID,
		Stream:     stream,
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // pluginauthz already wraps with oops context
	}
	if !dec.Allowed {
		return nil, oops.Code("STREAM_ACCESS_DENIED").
			With("plugin", r.pluginName).With("stream", stream).
			Errorf("not authorized to read stream")
	}
	events, err := r.inner.ReplayTail(ctx, stream, count, notBefore, beforeID)
	if err != nil {
		return nil, oops.With("plugin", r.pluginName).With("stream", stream).Wrap(err)
	}
	return events, nil
}
