// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package eventbus_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// TestRenderingMetadataGoProtoParity is INV-EVENTBUS-14. The Go struct and
// proto message MUST stay in sync — round-tripping through both
// conversion helpers MUST produce equal values.
func TestRenderingMetadataGoProtoParity(t *testing.T) {
	src := &eventbus.RenderingMetadata{
		Category:            "communication",
		Format:              "speech",
		Label:               "says",
		DisplayTarget:       eventbus.EventChannelTerminal,
		SourcePlugin:        "core-communication",
		SourcePluginVersion: "0.1.0",
	}

	proto := eventbus.RenderingToProto(src)
	require.NotNil(t, proto)
	assert.Equal(t, "communication", proto.GetCategory())
	assert.Equal(t, "speech", proto.GetFormat())
	assert.Equal(t, "says", proto.GetLabel())
	assert.Equal(t, corev1.EventChannel_EVENT_CHANNEL_TERMINAL, proto.GetDisplayTarget())
	assert.Equal(t, "core-communication", proto.GetSourcePlugin())
	assert.Equal(t, "0.1.0", proto.GetSourcePluginVersion())

	roundTrip := eventbus.RenderingFromProto(proto)
	assert.Equal(t, src, roundTrip)
}

func TestRenderingMetadataNilRoundTrip(t *testing.T) {
	assert.Nil(t, eventbus.RenderingToProto(nil))
	assert.Nil(t, eventbus.RenderingFromProto(nil))
}

// TestEventChannelEnumParity asserts the Go-side mirror values match
// the proto enum values. INV-EVENTBUS-14 (the parity dimension covering
// the EventChannel mirror specifically).
func TestEventChannelEnumParity(t *testing.T) {
	cases := []struct {
		goVal    eventbus.EventChannel
		protoVal corev1.EventChannel
	}{
		{eventbus.EventChannelUnspecified, corev1.EventChannel_EVENT_CHANNEL_UNSPECIFIED},
		{eventbus.EventChannelTerminal, corev1.EventChannel_EVENT_CHANNEL_TERMINAL},
		{eventbus.EventChannelState, corev1.EventChannel_EVENT_CHANNEL_STATE},
		{eventbus.EventChannelBoth, corev1.EventChannel_EVENT_CHANNEL_BOTH},
		{eventbus.EventChannelAuditOnly, corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY},
	}
	for _, c := range cases {
		assert.Equal(t, int32(c.goVal), int32(c.protoVal))
	}
}

// TestNoPlaintextReasonEnumParity asserts Go-side NoPlaintextReason mirror
// values match proto enum values (INV-EVENTBUS-14 — holomush-ojw1.6).
//
// The length check catches one-sided enum extension: if a new value is added
// to the proto without a Go-side mirror (or vice versa), the test fails
// before the per-value comparison would silently miss the new entry.
func TestNoPlaintextReasonEnumParity(t *testing.T) {
	cases := []struct {
		goVal    eventbus.NoPlaintextReason
		protoVal corev1.NoPlaintextReason
	}{
		{eventbus.NoPlaintextReasonUnspecified, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_UNSPECIFIED},
		{eventbus.NoPlaintextReasonAuthGuardDeny, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_AUTHGUARD_DENY},
		{eventbus.NoPlaintextReasonStaleDEK, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_STALE_DEK},
		{eventbus.NoPlaintextReasonAuditQueueFull, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_AUDIT_QUEUE_FULL},
		{eventbus.NoPlaintextReasonDEKMissing, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_DEK_MISSING},
		{eventbus.NoPlaintextReasonDEKBadColumns, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_DEK_BAD_COLUMNS},
		{eventbus.NoPlaintextReasonInternal, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_INTERNAL},
		{eventbus.NoPlaintextReasonDowngradeRefused, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_DOWNGRADE_REFUSED},
	}
	assert.Len(t, cases, len(corev1.NoPlaintextReason_name),
		"every proto NoPlaintextReason value MUST have a Go-side mirror in cases above; "+
			"if a new value was added on one side only, mirror it on the other side")
	for _, c := range cases {
		assert.Equal(t, int32(c.goVal), int32(c.protoVal),
			"Go NoPlaintextReason and proto NoPlaintextReason must have equal numeric values")
	}
}

// TestINV_CRYPTO_66_NoPlaintextReasonProtoGoParity is the INV-CRYPTO-66 parity test for
// the 4→7 expansion. Asserts all 7 values have matching proto-Go pairs in both
// directions. This is an alias for the extended TestNoPlaintextReasonEnumParity
// kept separately so the invariant name appears in test output.
func TestINV_CRYPTO_66_NoPlaintextReasonProtoGoParity(t *testing.T) {
	cases := []struct {
		name     string
		goVal    eventbus.NoPlaintextReason
		protoVal corev1.NoPlaintextReason
	}{
		{"UNSPECIFIED", eventbus.NoPlaintextReasonUnspecified, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_UNSPECIFIED},
		{"AUTHGUARD_DENY", eventbus.NoPlaintextReasonAuthGuardDeny, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_AUTHGUARD_DENY},
		{"STALE_DEK", eventbus.NoPlaintextReasonStaleDEK, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_STALE_DEK},
		{"AUDIT_QUEUE_FULL", eventbus.NoPlaintextReasonAuditQueueFull, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_AUDIT_QUEUE_FULL},
		{"DEK_MISSING", eventbus.NoPlaintextReasonDEKMissing, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_DEK_MISSING},
		{"DEK_BAD_COLUMNS", eventbus.NoPlaintextReasonDEKBadColumns, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_DEK_BAD_COLUMNS},
		{"INTERNAL", eventbus.NoPlaintextReasonInternal, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_INTERNAL},
		{"DOWNGRADE_REFUSED", eventbus.NoPlaintextReasonDowngradeRefused, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_DOWNGRADE_REFUSED},
	}
	require.Len(t, cases, 8, "expected exactly 8 NoPlaintextReason values after Phase 7 fence (DOWNGRADE_REFUSED) addition")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, int32(c.goVal), int32(c.protoVal),
				"Go and proto NoPlaintextReason numeric values must match")
		})
	}
}

// TestINV_CRYPTO_66_HotColdStampersDoNotEmitNewValues asserts that the three new
// NoPlaintextReason values (DEK_MISSING, DEK_BAD_COLUMNS, INTERNAL) are NOT
// referenced in the hot/cold tier stampers or subscriber. These values MUST be
// stamped exclusively by F's operator-read classifier (INV-CRYPTO-66).
//
// This is a static-analysis-style test: it reads the source files and fails if
// any of the new constant names appear. Because Go source is text, even a
// comment mentioning the constant would flag — intentional, since comments are
// also specifications.
func TestINV_CRYPTO_66_HotColdStampersDoNotEmitNewValues(t *testing.T) {
	// Resolve absolute paths via runtime.Caller so the test is stable
	// regardless of the working directory when invoked.
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must resolve current test file")
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	files := []string{
		filepath.Join(moduleRoot, "internal/eventbus/history/cold_postgres.go"),
		filepath.Join(moduleRoot, "internal/eventbus/history/dispatcher.go"),
		filepath.Join(moduleRoot, "internal/eventbus/subscriber.go"),
	}

	// The three new constant names in both Go and proto form.
	forbidden := []string{
		"NoPlaintextReasonDEKMissing",
		"NoPlaintextReasonDEKBadColumns",
		"NoPlaintextReasonInternal",
		"NO_PLAINTEXT_REASON_DEK_MISSING",
		"NO_PLAINTEXT_REASON_DEK_BAD_COLUMNS",
		"NO_PLAINTEXT_REASON_INTERNAL",
	}

	for _, relPath := range files {
		t.Run(relPath, func(t *testing.T) {
			data, err := os.ReadFile(relPath)
			require.NoError(t, err, "source file must be readable")
			src := string(data)
			for _, name := range forbidden {
				assert.NotContains(t, src, name,
					"hot/cold stamper %s MUST NOT reference new NoPlaintextReason value %s; "+
						"only F's classifier produces these (INV-CRYPTO-66)", relPath, name)
			}
		})
	}
}
