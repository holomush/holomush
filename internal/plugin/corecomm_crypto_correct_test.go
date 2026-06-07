// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// corecommManifestPath is the real shipped core-communication manifest.
// Relative to internal/plugin/ (the test's working directory).
const corecommManifestPath = "../../plugins/core-communication/plugin.yaml"

// loadCorecommManifest parses and crypto-validates the real core-communication
// plugin.yaml, returning the parsed manifest. ValidateCrypto trims/normalizes
// crypto.emits event_type so LookupEmitSensitivity sees the canonical form.
func loadCorecommManifest(t *testing.T) *plugins.Manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(corecommManifestPath))
	require.NoError(t, err, "read real core-communication manifest")
	m, err := plugins.ParseManifest(data)
	require.NoError(t, err, "parse core-communication manifest")
	require.NoError(t, plugins.ValidateCrypto(m), "core-communication crypto block must validate")
	return m
}

// TestCoreCommunicationSensitiveEventsAreEnforcedByManifest is the regression
// for holomush-50zqs: core-communication declares page/whisper/pemit as
// sensitivity:always, but the host-side sensitivity fence only enforces that
// when the manifest's crypto.emits event_type EXACTLY matches the wire event
// type the plugin emits. The plugin emits plugin-qualified types
// (core-communication:page — see verbs[].type and main.lua), so the manifest
// MUST declare the same qualified form. With the pre-fix bare event_type
// ("page"), LookupEmitSensitivity returns SensitivityNever and the fence is a
// silent no-op — page/whisper/pemit ship as plaintext despite the operator's
// always declaration.
//
// This asserts, against the REAL shipped manifest, that for each always-event
// the fence:
//   - resolves the wire type to SensitivityAlways (manifest matches wire), and
//   - REJECTS an emit that fails to claim Sensitive=true (fail-closed,
//     INV-PLUGIN-30), so a missing claim can never silently downgrade.
//
// Verifies: INV-PLUGIN-30
func TestCoreCommunicationSensitiveEventsAreEnforcedByManifest(t *testing.T) {
	m := loadCorecommManifest(t)

	// Wire event types are plugin-qualified (verbs[].type / main.lua emit type).
	alwaysWireTypes := []string{
		"core-communication:page",
		"core-communication:whisper",
		"core-communication:pemit",
	}

	for _, wireType := range alwaysWireTypes {
		t.Run(wireType, func(t *testing.T) {
			got := plugins.LookupEmitSensitivity(m, wireType)
			require.Equal(t, plugins.SensitivityAlways, got,
				"manifest crypto.emits event_type must match the qualified wire type %q so the fence enforces; got %q (a bare/mismatched declaration makes the fence a silent no-op)",
				wireType, got)

			// A correct claim is accepted and resolves to always (encrypt).
			eff, err := plugins.EnforceSensitivity(got, true)
			require.NoError(t, err, "always + claim=true must be accepted")
			require.Equal(t, plugins.SensitivityAlways, eff)

			// A missing claim is rejected (fail-closed) — never plaintext.
			_, err = plugins.EnforceSensitivity(got, false)
			require.Error(t, err,
				"always + claim=false MUST reject (EVENT_SENSITIVITY_REQUIRED) so a forgotten Sensitive=true cannot silently emit plaintext")
		})
	}
}

// TestCoreCommunicationNeverEventsResolveNeverThroughRealManifest pins the
// other half of the table against the REAL manifest: the plaintext events
// (say/pose/ooc/emit/whisper_notice) must resolve to SensitivityNever and
// reject an over-claim (INV-PLUGIN-29). This guards against an entry-ordering
// or composition regression silently flipping a plaintext event to always (or
// vice-versa). Wire types are plugin-qualified except `emit`, which is emitted
// bare (main.lua) — both forms must resolve via the same matcher.
//
// Verifies: INV-PLUGIN-29
func TestCoreCommunicationNeverEventsResolveNeverThroughRealManifest(t *testing.T) {
	m := loadCorecommManifest(t)

	neverWireTypes := []string{
		"core-communication:say",
		"core-communication:pose",
		"core-communication:ooc",
		"emit", // generic emit is emitted bare (main.lua)
		"core-communication:whisper_notice",
	}

	for _, wireType := range neverWireTypes {
		t.Run(wireType, func(t *testing.T) {
			got := plugins.LookupEmitSensitivity(m, wireType)
			require.Equal(t, plugins.SensitivityNever, got,
				"plaintext event %q must resolve to never, not be promoted to always", wireType)

			// An over-claim on a plaintext event is rejected (INV-PLUGIN-29).
			_, err := plugins.EnforceSensitivity(got, true)
			require.Error(t, err,
				"never + claim=true MUST reject (EVENT_SENSITIVITY_NOT_DECLARED)")
		})
	}
}
