// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// fenceRowOf builds an AuditRow for the fenceCheckRow unit tests.
// dekRef may be nil (absent dek_ref) or non-nil.
func fenceRowOf(eventType, codec string, dekRef *uint64) *pluginauditpb.AuditRow {
	row := &pluginauditpb.AuditRow{
		Id:      []byte("0123456789ABCDEF"),
		Subject: "events.test.scene.01ABC.ic",
		Type:    eventType,
		Codec:   codec,
		Payload: []byte("payload"),
	}
	if dekRef != nil {
		row.DekRef = dekRef
	}
	return row
}

// fenceLookupStub is the in-package CryptoKeysLookup fake for the
// fenceCheckRow unit tests. exists controls the (bool, nil) answer; a
// non-nil err exercises the infrastructure-failure (stream-fatal) path.
type fenceLookupStub struct {
	exists bool
	err    error
}

func (f fenceLookupStub) Exists(_ context.Context, _ uint64) (bool, error) {
	return f.exists, f.err
}

// TestFenceCheckRowVerdictsMatchSpec verifies that fenceCheckRow returns the
// correct fenceVerdict for each spec-mandated branch (INV-P7-7 downgrade,
// INV-P7-15 DEK existence) without going through the full fencedStream path.
func TestFenceCheckRowVerdictsMatchSpec(t *testing.T) {
	t.Parallel()
	always := map[string]struct{}{"scene_pose": {}}
	dek := uint64(7)

	tests := []struct {
		name   string
		row    *pluginauditpb.AuditRow
		lookup CryptoKeysLookup
		want   fenceVerdict
	}{
		{
			name:   "identity codec on always-sensitive type is a downgrade refusal",
			row:    fenceRowOf("scene_pose", "identity", nil),
			lookup: fenceLookupStub{exists: true},
			want:   fenceRefuseDowngrade,
		},
		{
			name:   "identity codec on non-sensitive type is clean",
			row:    fenceRowOf("scene_say", "identity", nil),
			lookup: fenceLookupStub{exists: true},
			want:   fenceClean,
		},
		{
			name:   "non-identity codec with existing dek is clean",
			row:    fenceRowOf("scene_pose", "xchacha20poly1305-v1", &dek),
			lookup: fenceLookupStub{exists: true},
			want:   fenceClean,
		},
		{
			name:   "non-identity codec with absent dek_ref is DEK-missing refusal",
			row:    fenceRowOf("scene_pose", "xchacha20poly1305-v1", nil),
			lookup: fenceLookupStub{exists: true},
			want:   fenceRefuseDEKMissing,
		},
		{
			name:   "non-identity codec with lookup-miss dek is DEK-missing refusal",
			row:    fenceRowOf("scene_pose", "xchacha20poly1305-v1", &dek),
			lookup: fenceLookupStub{exists: false},
			want:   fenceRefuseDEKMissing,
		},
		{
			name:   "non-identity codec with nil lookup fails closed to Internal",
			row:    fenceRowOf("scene_pose", "xchacha20poly1305-v1", &dek),
			lookup: nil,
			want:   fenceRefuseInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := fenceCheckRow(context.Background(), tt.row, always, tt.lookup)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestFenceCheckRowForwardsLookupError verifies that an infrastructure
// failure from the crypto_keys lookup surfaces as a wrapped
// AUDIT_ROW_DEK_LOOKUP_FAILED error (stream-fatal), distinguishing
// "can't even check" from the per-row "DEK doesn't exist" refusal.
func TestFenceCheckRowForwardsLookupError(t *testing.T) {
	t.Parallel()
	always := map[string]struct{}{"scene_pose": {}}
	dek := uint64(7)

	_, err := fenceCheckRow(
		context.Background(),
		fenceRowOf("scene_pose", "xchacha20poly1305-v1", &dek),
		always,
		fenceLookupStub{err: errors.New("connection refused")},
	)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_ROW_DEK_LOOKUP_FAILED")
}
