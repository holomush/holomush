// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseAuditHeadersIsDeterministic asserts that the shared parser
// returns identical typed values for identical inputs. INV-CRYPTO-39's
// stronger claim (cross-branch byte-equality of typed values produced
// by projection.persist() and pluginConsumer.dispatch()) is structural —
// the same ParseAuditHeaders implementation feeds both branches — and
// the executable cross-branch assertion lands in Task B.1 once the
// dispatcher widens to invoke the parser on the same code path.
func TestParseAuditHeadersIsDeterministic(t *testing.T) {
	header := nats.Header{}
	header.Set("App-Codec", "xchacha20poly1305-v1")
	header.Set("App-Schema-Version", "2")
	header.Set("App-Dek-Ref", "12345")
	header.Set("App-Dek-Version", "3")

	meta1, err := ParseAuditHeaders(header)
	require.NoError(t, err)
	meta2, err := ParseAuditHeaders(header)
	require.NoError(t, err)

	assert.Equal(t, meta1.Codec, meta2.Codec)
	assert.Equal(t, meta1.SchemaVer, meta2.SchemaVer)
	require.NotNil(t, meta1.DEKRef)
	require.NotNil(t, meta2.DEKRef)
	assert.Equal(t, *meta1.DEKRef, *meta2.DEKRef)
	require.NotNil(t, meta1.DEKVersion)
	require.NotNil(t, meta2.DEKVersion)
	assert.Equal(t, *meta1.DEKVersion, *meta2.DEKVersion)
}
