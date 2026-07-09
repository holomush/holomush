// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// fakeSessionStreamStore is a controllable sessionStreamStore for QuerySessionStreams
// unit tests. banned keys on channel id — the queried character is banned from a
// channel iff banned[id] is true (a non-member / guest is simply absent → false).
type fakeSessionStreamStore struct {
	memberships []channelRow
	defaults    []channelRow
	banned      map[string]bool
	membersErr  error
	defaultsErr error
	bannedErr   error
}

func (f *fakeSessionStreamStore) ListForCharacter(_ context.Context, _ string) ([]channelRow, error) {
	if f.membersErr != nil {
		return nil, f.membersErr
	}
	return f.memberships, nil
}

func (f *fakeSessionStreamStore) ListDefaultChannels(_ context.Context) ([]channelRow, error) {
	if f.defaultsErr != nil {
		return nil, f.defaultsErr
	}
	return f.defaults, nil
}

func (f *fakeSessionStreamStore) IsBannedFrom(_ context.Context, channelID, _ string) (bool, error) {
	if f.bannedErr != nil {
		return false, f.bannedErr
	}
	return f.banned[channelID], nil
}

func newSessionStreamsPlugin(store sessionStreamStore) *channelPlugin {
	return &channelPlugin{streamStore: store}
}

func sessionReq() pluginsdk.SessionStreamsRequest {
	return pluginsdk.SessionStreamsRequest{
		CharacterID: testCharID,
		PlayerID:    testPlayerID,
		SessionID:   "sess-01",
	}
}

// ── QuerySessionStreams ─────────────────────────────────────────────────────

func TestQuerySessionStreamsReturnsMemberChannelsAsRelativeRefs(t *testing.T) {
	p := newSessionStreamsPlugin(&fakeSessionStreamStore{
		memberships: []channelRow{{ID: "ch-a"}, {ID: "ch-b"}},
	})
	got, err := p.QuerySessionStreams(context.Background(), sessionReq())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"channel.ch-a", "channel.ch-b"}, got,
		"member channels are contributed as domain-RELATIVE channel.<id> refs (R2-A)")
}

func TestQuerySessionStreamsGuestReceivesExactlyDefaultChannels(t *testing.T) {
	// A guest session has zero membership rows; it receives exactly the seeded
	// default channels via the ListDefaultChannels ∪ memberships union (D-01) —
	// no membership-row write at session establishment.
	p := newSessionStreamsPlugin(&fakeSessionStreamStore{
		memberships: nil,
		defaults:    []channelRow{{ID: "ch-public", IsDefault: true}},
	})
	got, err := p.QuerySessionStreams(context.Background(), sessionReq())
	require.NoError(t, err)
	assert.Equal(t, []string{"channel.ch-public"}, got,
		"a guest (no memberships) receives exactly the seeded default channels")
}

func TestQuerySessionStreamsDedupesMemberAndDefaultOverlap(t *testing.T) {
	// The character is an explicit member of a channel that is ALSO a default —
	// it must appear exactly once.
	p := newSessionStreamsPlugin(&fakeSessionStreamStore{
		memberships: []channelRow{{ID: "ch-public"}, {ID: "ch-private"}},
		defaults:    []channelRow{{ID: "ch-public", IsDefault: true}},
	})
	got, err := p.QuerySessionStreams(context.Background(), sessionReq())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"channel.ch-public", "channel.ch-private"}, got)
	assert.Len(t, got, 2, "a channel that is both a membership and a default is deduped to one")
}

func TestQuerySessionStreamsExcludesBannedDefault(t *testing.T) {
	// A character banned from a default channel does NOT receive that default's
	// stream — the ban filter applies to defaults too.
	p := newSessionStreamsPlugin(&fakeSessionStreamStore{
		memberships: nil,
		defaults:    []channelRow{{ID: "ch-public"}, {ID: "ch-lobby"}},
		banned:      map[string]bool{"ch-public": true},
	})
	got, err := p.QuerySessionStreams(context.Background(), sessionReq())
	require.NoError(t, err)
	assert.Equal(t, []string{"channel.ch-lobby"}, got,
		"a default the character is banned from is excluded")
}

func TestQuerySessionStreamsEmptyWhenNoMembershipsAndNoDefaults(t *testing.T) {
	p := newSessionStreamsPlugin(&fakeSessionStreamStore{})
	got, err := p.QuerySessionStreams(context.Background(), sessionReq())
	require.NoError(t, err)
	assert.Empty(t, got, "empty (not error) only when there are neither memberships nor seeded defaults")
}

func TestQuerySessionStreamsSurfacesMembershipQueryError(t *testing.T) {
	p := newSessionStreamsPlugin(&fakeSessionStreamStore{
		membersErr: oops.Code("CHANNEL_LIST_FAILED").Errorf("boom"),
	})
	_, err := p.QuerySessionStreams(context.Background(), sessionReq())
	require.Error(t, err)
}

func TestQuerySessionStreamsSurfacesDefaultsQueryError(t *testing.T) {
	p := newSessionStreamsPlugin(&fakeSessionStreamStore{
		defaultsErr: oops.Code("CHANNEL_LIST_DEFAULTS_FAILED").Errorf("boom"),
	})
	_, err := p.QuerySessionStreams(context.Background(), sessionReq())
	require.Error(t, err)
}

func TestQuerySessionStreamsFailsClosedWhenStoreUnwired(t *testing.T) {
	p := &channelPlugin{}
	_, err := p.QuerySessionStreams(context.Background(), sessionReq())
	require.Error(t, err, "an unwired store fails closed rather than contributing an empty set silently")
}

// TestRelativeChannelStreamQualifiesToEmitSubject proves the domain-RELATIVE ref
// the plugin contributes, once host-Qualified (events.<game>. + relative ref),
// resolves to the SAME subject content is emitted on via dotStyleChannelSubject —
// so establishment delivery lands on the emitted stream.
func TestRelativeChannelStreamQualifiesToEmitSubject(t *testing.T) {
	const id = "ch-42"
	rel := relativeChannelStream(id)
	assert.Equal(t, "channel."+id, rel, "relative ref is domain-relative, no events. prefix")
	qualified := "events." + testGameID + "." + rel // mirrors eventbus.Qualify(gameID, rel)
	assert.Equal(t, dotStyleChannelSubject(testGameID, id), qualified,
		"Qualify(gameID, channel.<id>) == the emit subject events.<game>.channel.<id>")
}
