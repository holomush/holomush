// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package approval_test

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/admin/approval"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// TestOpArgsHashAlgorithmStableAgainstGolden locks the SHA-256 over
// proto-deterministic-marshal output for representative messages. INV-CRYPTO-75.
// Updates require an INV-CRYPTO-75 review per the master-spec amendment in T23.
func TestOpArgsHashAlgorithmStableAgainstGolden(t *testing.T) {
	tests := []struct {
		name    string
		msg     proto.Message
		wantHex string // captured on first green run; do NOT change without an INV-CRYPTO-75 review
	}{
		{name: "empty AuthenticateRequest", msg: &adminv1.AuthenticateRequest{}, wantHex: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{name: "AuthenticateRequest with creds", msg: &adminv1.AuthenticateRequest{Username: "alice", Password: "p", TotpCode: "123456"}, wantHex: "0a9d3a4e69fc47797af5c93a0ea86665508c0b5d8e5db26b3637daa000f06708"},
		{name: "ApproveRequest with binary request_id", msg: &adminv1.ApproveRequest{SessionToken: "01HZA0000000", RequestId: []byte{0x01, 0x02, 0x03, 0x04}}, wantHex: "dcc803a68f3ff316b538b19458b201bf46d18689d889ac3c4f54a35332fa42f6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := approval.ComputeOpArgsHash(tt.msg)
			require.NoError(t, err)
			gotHex := hex.EncodeToString(got)
			t.Logf("%s -> %s", tt.name, gotHex)
			assert.Equal(t, tt.wantHex, gotHex)
		})
	}
}
