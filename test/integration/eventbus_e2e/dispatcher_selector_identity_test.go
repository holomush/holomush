// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/history"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeKeySelectorForIdentityTest is a no-op selector — the test only
// asserts pointer identity, never invokes Select methods.
type fakeKeySelectorForIdentityTest struct{}

func (fakeKeySelectorForIdentityTest) SelectForEncrypt(_ context.Context, _ string) (codec.Name, codec.KeyLabel, error) {
	return codec.NameIdentity, "", nil
}
func (fakeKeySelectorForIdentityTest) SelectForDecrypt(_ context.Context, _ codec.Name, _ codec.KeyID) (codec.Key, error) {
	return codec.NoKey, nil
}

// TestDispatcherAndHotTierShareSelector — INV-P7-9. Drives the
// production wiring path at cmd/holomush/core.go:488. Until Task E.3
// threads the same codec.KeySelector instance into both
// PluginConsumerManager and history.NewReader, this test fails with the
// PluginConsumerManager carrying a nil selector.
//
// The test is deliberately PARKED-FAIL per the bead acceptance criteria:
// it asserts production wiring that lands in 1r0v.5 (Phase E.3). The
// substrate (WithKeySelector option + KeySelectorForTest accessor) ships
// in 1r0v.2.
func TestDispatcherAndHotTierShareSelector(t *testing.T) {
	t.Parallel()

	// The single shared selector instance — INV-P7-9 requires both
	// substrates to hold this exact pointer.
	selector := fakeKeySelectorForIdentityTest{}

	// Mirror cmd/holomush/core.go:488 — Phase 7 substrate accepts the
	// option but production wiring does NOT pass it yet.
	pcm := audit.NewPluginConsumerManager(nil /* js — not exercised here */)

	// Mirror cmd/holomush/sub_grpc.go's history.NewReader call. This
	// branch DOES wire the selector via WithCodecSelector today; it's
	// the dispatcher half (PluginConsumerManager) that's missing the
	// option until E.3.
	reader := history.NewReader(nil, nil, time.Hour, time.Now,
		history.WithCodecSelector(selector))

	pcmSel := pcm.KeySelectorForTest()
	readerSel := reader.KeySelectorForTest()

	require.NotNil(t, readerSel,
		"history.Reader MUST hold the shared selector (substrate already wired)")
	assert.NotNil(t, pcmSel,
		"INV-P7-9: PluginConsumerManager MUST hold the shared selector — wiring lands in Task E.3 (1r0v.5)")
	assert.True(t, pcmSel != nil && reflect.ValueOf(pcmSel).Pointer() == reflect.ValueOf(readerSel).Pointer(),
		"INV-P7-9: PluginConsumerManager and history.Reader MUST share the same KeySelector instance")
}
