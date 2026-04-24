// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cursor

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	cursorv1 "github.com/holomush/holomush/internal/eventbus/cursor/cursorv1"
)

func TestEncodeDecodeRoundTripsHostCursor(t *testing.T) {
	t.Parallel()
	id := ulid.MustParse("01HYXYZEVT0000000000000001")
	in := Cursor{
		Version: 1,
		Epoch:   0,
		Owner:   Owner{Kind: OwnerHost},
		Host:    &HostCursor{Seq: 42, ID: id},
	}
	bytes, err := Encode(in)
	require.NoError(t, err)
	out, err := Decode(bytes)
	require.NoError(t, err)
	assert.Equal(t, in.Version, out.Version)
	assert.Equal(t, in.Epoch, out.Epoch)
	assert.Equal(t, OwnerHost, out.Owner.Kind)
	require.NotNil(t, out.Host)
	assert.Equal(t, uint64(42), out.Host.Seq)
	assert.Equal(t, id, out.Host.ID)
	assert.Nil(t, out.Plugin)
}

func TestEncodeDecodeRoundTripsPluginCursor(t *testing.T) {
	t.Parallel()
	in := Cursor{
		Version: 1,
		Epoch:   0,
		Owner:   Owner{Kind: OwnerPlugin, PluginName: "core-scenes"},
		Plugin:  []byte("opaque-from-plugin"),
	}
	bytes, err := Encode(in)
	require.NoError(t, err)
	out, err := Decode(bytes)
	require.NoError(t, err)
	assert.Equal(t, OwnerPlugin, out.Owner.Kind)
	assert.Equal(t, "core-scenes", out.Owner.PluginName)
	assert.Equal(t, []byte("opaque-from-plugin"), out.Plugin)
	assert.Nil(t, out.Host)
}

func TestDecodeRejectsEmptyBytes(t *testing.T) {
	t.Parallel()
	_, err := Decode(nil)
	require.Error(t, err)
	_, err = Decode([]byte{})
	require.Error(t, err)
}

func TestDecodeRejectsUnknownVersion(t *testing.T) {
	t.Parallel()
	in := Cursor{
		Version: 99,
		Owner:   Owner{Kind: OwnerHost},
		Host:    &HostCursor{Seq: 1, ID: ulid.ULID{}},
	}
	bytes, err := Encode(in)
	require.NoError(t, err)
	_, err = Decode(bytes)
	require.Error(t, err)
}

func TestEncodeRejectsHostOwnerWithoutBody(t *testing.T) {
	t.Parallel()
	_, err := Encode(Cursor{Owner: Owner{Kind: OwnerHost}})
	require.Error(t, err)
}

func TestEncodeRejectsPluginOwnerWithoutName(t *testing.T) {
	t.Parallel()
	_, err := Encode(Cursor{Owner: Owner{Kind: OwnerPlugin}})
	require.Error(t, err)
}

func TestCurrentEpochIsZero(t *testing.T) {
	t.Parallel()
	assert.Equal(t, uint64(0), CurrentEpoch())
}

// TestOwnerKindStringReturnsCorrectLabels covers the String() method on all
// three OwnerKind values.
func TestOwnerKindStringReturnsCorrectLabels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		kind OwnerKind
		want string
	}{
		{"host label for OwnerHost", OwnerHost, "host"},
		{"plugin label for OwnerPlugin", OwnerPlugin, "plugin"},
		{"unspecified label for OwnerUnspecified", OwnerUnspecified, "unspecified"},
		{"unspecified label for unknown value 99", OwnerKind(99), "unspecified"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.kind.String())
		})
	}
}

// TestEncodeRejectsOwnerUnspecified covers the default: branch in Encode.
func TestEncodeRejectsOwnerUnspecified(t *testing.T) {
	t.Parallel()
	_, err := Encode(Cursor{
		Version: CurrentVersion,
		Owner:   Owner{Kind: OwnerUnspecified},
	})
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error")
	assert.Equal(t, "EVENTBUS_CURSOR_INVALID", oopsErr.Code())
}

// TestDecodeWithOwnerKindUnspecifiedInProto exercises the default: branch in
// Decode by constructing wire bytes with OWNER_KIND_UNSPECIFIED directly via
// proto.Marshal, bypassing Encode (which also rejects OwnerUnspecified).
func TestDecodeWithOwnerKindUnspecifiedInProto(t *testing.T) {
	t.Parallel()
	pb := &cursorv1.Cursor{
		Version: CurrentVersion,
		Epoch:   0,
		Owner: &cursorv1.Owner{
			Kind: cursorv1.OwnerKind_OWNER_KIND_UNSPECIFIED,
		},
	}
	wire, err := proto.Marshal(pb)
	require.NoError(t, err)

	_, decErr := Decode(wire)
	require.Error(t, decErr, "OwnerUnspecified in proto must be rejected by Decode")
	oopsErr, ok := oops.AsOops(decErr)
	require.True(t, ok, "expected oops error")
	assert.Equal(t, "EVENTBUS_CURSOR_INVALID", oopsErr.Code())
}

// TestEncodeWithVersionZeroUpgradesToCurrentVersion verifies that when
// c.Version == 0, Encode upgrades it to CurrentVersion before marshalling,
// so the decoded cursor carries CurrentVersion.
func TestEncodeWithVersionZeroUpgradesToCurrentVersion(t *testing.T) {
	t.Parallel()
	id := ulid.MustParse("01HYXYZEVT0000000000000001")
	in := Cursor{
		Version: 0, // trigger upgrade
		Owner:   Owner{Kind: OwnerHost},
		Host:    &HostCursor{Seq: 7, ID: id},
	}
	b, err := Encode(in)
	require.NoError(t, err)

	out, err := Decode(b)
	require.NoError(t, err)
	assert.Equal(t, CurrentVersion, out.Version, "version 0 must be upgraded to CurrentVersion on Encode")
	assert.Equal(t, uint64(7), out.Host.Seq)
}

// TestOwnerRoundTripForAllKinds exercises ownerToProto / ownerFromProto for
// every OwnerKind so the helper branches are covered.
func TestOwnerRoundTripForAllKinds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		owner Owner
	}{
		{"host", Owner{Kind: OwnerHost}},
		{"plugin with name", Owner{Kind: OwnerPlugin, PluginName: "core-scenes"}},
		{"unspecified", Owner{Kind: OwnerUnspecified}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pb := ownerToProto(tc.owner)
			require.NotNil(t, pb)
			got := ownerFromProto(pb)
			assert.Equal(t, tc.owner.Kind, got.Kind)
			assert.Equal(t, tc.owner.PluginName, got.PluginName)
		})
	}
}

// TestOwnerFromProtoWithNilOwner covers the nil-pb guard in ownerFromProto.
func TestOwnerFromProtoWithNilOwner(t *testing.T) {
	t.Parallel()
	got := ownerFromProto(nil)
	assert.Equal(t, OwnerUnspecified, got.Kind)
	assert.Empty(t, got.PluginName)
}
