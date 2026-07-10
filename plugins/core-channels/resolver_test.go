// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// fakeChannelStore is an in-memory channelMembershipStorer for resolver unit
// tests — it avoids the testcontainer round trip the real store needs. err, when
// set, is returned verbatim so tests can exercise the not-found and generic
// error-translation paths.
type fakeChannelStore struct {
	channels map[string]*channelRow
	members  map[string][]string
	banned   map[string][]string
	muted    map[string][]string
	err      error
}

func newFakeChannelStore() *fakeChannelStore {
	return &fakeChannelStore{
		channels: map[string]*channelRow{},
		members:  map[string][]string{},
		banned:   map[string][]string{},
		muted:    map[string][]string{},
	}
}

func (f *fakeChannelStore) GetWithMembership(_ context.Context, id string) (*channelRow, []string, []string, []string, error) {
	if f.err != nil {
		return nil, nil, nil, nil, f.err
	}
	row, ok := f.channels[id]
	if !ok {
		return nil, nil, nil, nil, oops.Code("CHANNEL_NOT_FOUND").With("channel_id", id).Errorf("channel not found")
	}
	return row, f.members[id], f.banned[id], f.muted[id], nil
}

func TestChannelResolverGetSchemaAdvertisesChannelType(t *testing.T) {
	r := NewChannelResolver(newFakeChannelStore())

	resp, err := r.GetSchema(context.Background(), &pluginv1.GetSchemaRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp.GetResourceTypes())
	schema, ok := resp.GetResourceTypes()[resourceTypeChannel]
	require.True(t, ok, "schema must include 'channel' resource type")

	attrs := schema.GetAttributes()
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING, attrs["name"])
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING, attrs["type"])
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING, attrs["owner"])
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_BOOL, attrs["has_owner"],
		"has_owner MUST be declared as a BOOL witness")
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_BOOL, attrs["archived"])
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST, attrs["members"])
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST, attrs["banned"])
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST, attrs["muted"])
}

// TestResolverNeverExposesContentByForbiddenAttributeName is the regression lock
// that the channel resolver never advertises a message-content-bearing
// attribute. Channel history CONTENT lives behind the membership-gated
// QueryHistory RPC (01-06), never on the ABAC attribute path.
func TestResolverNeverExposesContentByForbiddenAttributeName(t *testing.T) {
	r := NewChannelResolver(newFakeChannelStore())

	resp, err := r.GetSchema(context.Background(), &pluginv1.GetSchemaRequest{})
	require.NoError(t, err)
	schema, ok := resp.GetResourceTypes()[resourceTypeChannel]
	require.True(t, ok)

	forbidden := map[string]struct{}{
		"content": {}, "message": {}, "messages": {}, "log": {}, "body": {}, "text": {}, "entries": {},
	}
	for name := range schema.GetAttributes() {
		_, bad := forbidden[name]
		assert.False(t, bad, "resolver exposes content-bearing attribute %q", name)
	}
}

func TestChannelResolverResolveResourceReturnsMembersList(t *testing.T) {
	store := newFakeChannelStore()
	store.channels["chan-01"] = &channelRow{
		ID: "chan-01", Name: "General", Type: "public", OwnerID: "char-alice",
	}
	store.members["chan-01"] = []string{"char-alice", "char-bob"}
	r := NewChannelResolver(store)

	resp, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: resourceTypeChannel,
		ResourceId:   "chan-01",
	})
	require.NoError(t, err)
	attrs := resp.GetAttributes()

	require.NotNil(t, attrs["name"])
	assert.Equal(t, "General", attrs["name"].GetStringValue())
	require.NotNil(t, attrs["type"])
	assert.Equal(t, "public", attrs["type"].GetStringValue())

	membersAttr := attrs["members"]
	require.NotNil(t, membersAttr)
	require.NotNil(t, membersAttr.GetStringListValue())
	vals := membersAttr.GetStringListValue().GetValues()
	assert.Contains(t, vals, "char-alice", "a member's id MUST be present")
	assert.NotContains(t, vals, "char-carol", "a non-member's id MUST be absent")
}

func TestChannelResolverResolvesBannedAndMutedLists(t *testing.T) {
	store := newFakeChannelStore()
	store.channels["chan-mod"] = &channelRow{
		ID: "chan-mod", Name: "Mod", Type: "private", OwnerID: "char-owner",
	}
	store.members["chan-mod"] = []string{"char-owner", "char-member"}
	store.banned["chan-mod"] = []string{"char-baddie"}
	store.muted["chan-mod"] = []string{"char-loud"}
	r := NewChannelResolver(store)

	resp, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: resourceTypeChannel,
		ResourceId:   "chan-mod",
	})
	require.NoError(t, err)
	attrs := resp.GetAttributes()

	require.NotNil(t, attrs["banned"])
	assert.ElementsMatch(t, []string{"char-baddie"}, attrs["banned"].GetStringListValue().GetValues())
	require.NotNil(t, attrs["muted"])
	assert.ElementsMatch(t, []string{"char-loud"}, attrs["muted"].GetStringListValue().GetValues())
}

func TestChannelResolverArchivedFlag(t *testing.T) {
	store := newFakeChannelStore()
	store.channels["chan-arch"] = &channelRow{
		ID: "chan-arch", Name: "Old", Type: "public", OwnerID: "char-alice", Archived: true,
	}
	r := NewChannelResolver(store)

	resp, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: resourceTypeChannel,
		ResourceId:   "chan-arch",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.GetAttributes()["archived"])
	assert.True(t, resp.GetAttributes()["archived"].GetBoolValue())
}

// TestChannelResolverOwnerWitness exercises the omit-optional-attrs contract
// (.claude/rules/abac-providers.md, ADR holomush-ti1b) for the channel owner
// attribute. A system-owned default channel (owner_id == systemOwnerID) has no
// character owner: the resolver MUST omit the "owner" key entirely and emit
// has_owner=false, so an owner-moderation policy (resource.channel.owner ==
// principal.id) fail-closes on system channels (T-01-14). A user-owned channel
// emits owner + has_owner=true. An empty-string sentinel would fail-OPEN in the
// DSL evaluator (a present "" is a value, not a missing key).
func TestChannelResolverOwnerWitness(t *testing.T) {
	tests := []struct {
		name        string
		ownerID     string
		wantPresent bool
		wantValue   string
	}{
		{"emits owner for a user-owned channel", "char-owner", true, "char-owner"},
		{"omits owner for a system-owned default channel", systemOwnerID, false, ""},
		{"omits owner when owner_id is empty", "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeChannelStore()
			store.channels["chan-w"] = &channelRow{
				ID: "chan-w", Name: "W", Type: "public", OwnerID: tt.ownerID,
			}
			r := NewChannelResolver(store)

			resp, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
				ResourceType: resourceTypeChannel,
				ResourceId:   "chan-w",
			})
			require.NoError(t, err)
			attrs := resp.GetAttributes()

			require.NotNil(t, attrs["has_owner"], "has_owner witness MUST always be present")
			assert.Equal(t, tt.wantPresent, attrs["has_owner"].GetBoolValue(),
				"has_owner MUST reflect whether a character owner resolved")
			if tt.wantPresent {
				require.NotNil(t, attrs["owner"], "owner key MUST be present for a character-owned channel")
				assert.Equal(t, tt.wantValue, attrs["owner"].GetStringValue())
			} else {
				assert.NotContains(t, attrs, "owner",
					"abac-providers.md: owner key MUST be absent (no empty-string / system sentinel)")
			}
		})
	}
}

func TestChannelResolverRejectsForeignResourceType(t *testing.T) {
	r := NewChannelResolver(newFakeChannelStore())

	_, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: "widget",
		ResourceId:   "widget-1",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.True(t, strings.Contains(st.Message(), "channel"), "error should mention 'channel'")
}

func TestChannelResolverReturnsNotFoundForMissingChannel(t *testing.T) {
	r := NewChannelResolver(newFakeChannelStore())

	_, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: resourceTypeChannel,
		ResourceId:   "chan-missing",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

// TestChannelResolverCreateSentinelResolvesToEmptyAttributes asserts the create
// sentinel id resolves to an empty attribute bag WITHOUT a store lookup, so the
// real seeded ABAC engine can evaluate the principal-only admin-create policy at
// create time (before any channel row exists) instead of fail-closing on a
// CHANNEL_NOT_FOUND. Regression guard for the "resolver is never called"
// assumption that broke channel creation under real ABAC (01-09).
func TestChannelResolverCreateSentinelResolvesToEmptyAttributes(t *testing.T) {
	store := newFakeChannelStore()
	store.err = oops.Code("CHANNEL_NOT_FOUND").Errorf("must not be consulted for the create sentinel")
	r := NewChannelResolver(store)

	resp, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: resourceTypeChannel,
		ResourceId:   createSentinelResourceID,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.GetAttributes(),
		"the create sentinel MUST resolve to an empty attribute bag (no resource.channel.* keys)")
}

// TestChannelResolverGenericErrorDoesNotLeak asserts a non-not-found store error
// surfaces as codes.Internal with a generic message (no inner detail on the
// wire) per .claude/rules/grpc-errors.md.
func TestChannelResolverGenericErrorDoesNotLeak(t *testing.T) {
	store := newFakeChannelStore()
	store.err = oops.Code("CHANNEL_GET_FAILED").With("secret_table", "channel_memberships").
		Errorf("connection refused on internal host 10.0.0.5")
	r := NewChannelResolver(store)

	_, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: resourceTypeChannel,
		ResourceId:   "chan-x",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.NotContains(t, st.Message(), "10.0.0.5", "internal host MUST NOT leak on the wire")
	assert.NotContains(t, st.Message(), "channel_memberships", "internal table MUST NOT leak on the wire")
}
