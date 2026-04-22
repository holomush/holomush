// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cursor

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
