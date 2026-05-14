// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestParseAuditHeaders_Identity(t *testing.T) {
	h := nats.Header{}
	h.Set(headerCodec, "identity")
	h.Set(headerSchemaVersion, "1")

	got, err := ParseAuditHeaders(h)
	require.NoError(t, err)
	assert.Equal(t, "identity", got.Codec)
	assert.Equal(t, int32(1), got.SchemaVer)
	assert.Nil(t, got.DEKRef)
	assert.Nil(t, got.DEKVersion)
}

func TestParseAuditHeaders_Encrypted(t *testing.T) {
	h := nats.Header{}
	h.Set(headerCodec, "xchacha20poly1305-v1")
	h.Set(headerSchemaVersion, "2")
	h.Set(eventbus.HeaderDekRef, "42")
	h.Set(eventbus.HeaderDekVersion, "7")

	got, err := ParseAuditHeaders(h)
	require.NoError(t, err)
	assert.Equal(t, "xchacha20poly1305-v1", got.Codec)
	assert.Equal(t, int32(2), got.SchemaVer)
	require.NotNil(t, got.DEKRef)
	assert.Equal(t, int64(42), *got.DEKRef)
	require.NotNil(t, got.DEKVersion)
	assert.Equal(t, int32(7), *got.DEKVersion)
}

func TestParseAuditHeaders_MissingCodec(t *testing.T) {
	h := nats.Header{}
	h.Set(headerSchemaVersion, "1")

	_, err := ParseAuditHeaders(h)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_MISSING_HEADER")
}

func TestParseAuditHeaders_BadDekRef(t *testing.T) {
	h := nats.Header{}
	h.Set(headerCodec, "xchacha20poly1305-v1")
	h.Set(headerSchemaVersion, "1")
	h.Set(eventbus.HeaderDekRef, "not-a-number")

	_, err := ParseAuditHeaders(h)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_DEK_REF_PARSE_FAILED")
}

func TestParseAuditHeaders_SchemaVerOutOfRange(t *testing.T) {
	h := nats.Header{}
	h.Set(headerCodec, "identity")
	h.Set(headerSchemaVersion, "99999")

	_, err := ParseAuditHeaders(h)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_BAD_SCHEMA_VERSION")
}

func TestParseAuditHeaders_SchemaVerNegative(t *testing.T) {
	h := nats.Header{}
	h.Set(headerCodec, "identity")
	h.Set(headerSchemaVersion, "-1")

	_, err := ParseAuditHeaders(h)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_BAD_SCHEMA_VERSION")
}

func TestParseAuditHeaders_SchemaVerBoundary(t *testing.T) {
	cases := []struct {
		name      string
		value     string
		wantErr   bool
		wantValue int32
	}{
		{name: "max_accepted", value: "32767", wantErr: false, wantValue: 32767},
		{name: "above_max_rejected", value: "32768", wantErr: true},
		{name: "zero_accepted", value: "0", wantErr: false, wantValue: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := nats.Header{}
			h.Set(headerCodec, "identity")
			h.Set(headerSchemaVersion, tc.value)

			got, err := ParseAuditHeaders(h)
			if tc.wantErr {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, "AUDIT_BAD_SCHEMA_VERSION")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantValue, got.SchemaVer)
		})
	}
}
