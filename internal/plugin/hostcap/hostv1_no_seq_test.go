// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package hostcap_test

// D-08 guard: hostv1.Event MUST NOT grow a plugin-facing sequence field.
//
// internal/eventbus/types.go's Event.Seq doc says it is "Host-internal —
// never serialized in any public proto envelope." A plugin-facing seq would
// also silently change meaning across tiers (internal/eventbus/history/
// tier.go: a cold event's js_seq is an aged-out JS seq no longer in JS
// retention; seq is only meaningful within a tier's sequence space). The
// cursor field (= 8) is the opaque pagination token, encoded by
// internal/eventbus/cursor — an internal/ package an external plugin cannot
// import — so it carries the real Seq (per this plan's Task 2 fix) without
// exposing it.
//
// A future plugin-facing seq requires an explicit invariant amendment + ADR,
// not a silent field addition; this census guard fails loudly on ANY new
// field so that decision cannot be made silently.
//
// No `// Verifies:` annotation: there is no registry invariant for D-08 yet.
// Annotating a nonexistent or unrelated id would be a fabricated binding
// (the documented INV-RB-3 bug). Propose a registry entry separately if
// wanted, per .claude/rules/invariants.md.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// TestHostV1EventFieldCensusExcludesSequence walks hostv1.Event's proto
// descriptor and asserts set equality against the exact 8 declared fields —
// not merely "no seq present". Set equality is what makes this a real guard:
// it fails on ANY new field, forcing whoever adds one to justify it.
func TestHostV1EventFieldCensusExcludesSequence(t *testing.T) {
	fields := (&hostv1.Event{}).ProtoReflect().Descriptor().Fields()

	got := make(map[string]bool, fields.Len())
	for i := range fields.Len() {
		got[string(fields.Get(i).Name())] = true
	}

	want := map[string]bool{
		"id":         true,
		"stream":     true,
		"type":       true,
		"timestamp":  true,
		"actor_kind": true,
		"actor_id":   true,
		"payload":    true,
		"cursor":     true,
	}

	require.Lenf(t, got, len(want),
		"hostv1.Event MUST have exactly %d fields; a new field requires an explicit "+
			"invariant amendment + ADR (D-08), not a silent addition — got: %v", len(want), got)
	assert.Equal(t, want, got, "hostv1.Event's field set drifted from the D-08 census")

	// Belt-and-braces: name the specific prohibition when that is what
	// happened, so the failure message is unambiguous even if the set
	// equality above is ever loosened.
	for name := range got {
		assert.NotContainsf(t, name, "seq", "hostv1.Event MUST NOT carry a sequence field (D-08): %q", name)
		assert.NotContainsf(t, name, "sequence", "hostv1.Event MUST NOT carry a sequence field (D-08): %q", name)
	}
}
