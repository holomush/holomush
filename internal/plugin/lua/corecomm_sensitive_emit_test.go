// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// corecommAlwaysVerbs are the core-communication wire event types declared
// sensitivity:always in plugins/core-communication/plugin.yaml crypto.emits.
// Each is a private message (page/whisper/pemit) that MUST be emitted with a
// per-event Sensitive=true claim, or the host fence rejects it fail-closed
// (INV-PLUGIN-30). See holomush-50zqs.
var corecommAlwaysVerbs = []string{
	"core-communication:page",
	"core-communication:whisper",
	"core-communication:pemit",
}

// sensitiveClaimPattern matches a `sensitive = true` emit-table key write,
// tolerant of surrounding whitespace.
var sensitiveClaimPattern = regexp.MustCompile(`sensitive\s*=\s*true`)

// TestCoreCommunicationAlwaysEmitsClaimSensitive is the Lua-side half of the
// holomush-50zqs regression. The host fence only encrypts a sensitivity:always
// event when the plugin emits it with Sensitive=true; absent the claim the
// emit is rejected (EVENT_SENSITIVITY_REQUIRED). core-communication is a Lua
// plugin, so the claim is the `sensitive = true` key on the emit table in
// main.lua. This asserts each page/whisper/pemit emit table carries that claim.
//
// The check is brace-scoped, not line-scoped: it isolates the innermost emit
// table { ... } enclosing each `type = "<verb>"` key and asserts the claim
// appears within that table. This is robust to the table being reformatted
// across multiple lines (the prior same-line heuristic would have produced
// false failures on a reflow). A behavioral end-to-end assertion through the
// real command handler is tracked separately (holomush-50zqs follow-up).
func TestCoreCommunicationAlwaysEmitsClaimSensitive(t *testing.T) {
	root := repoRoot(t)
	mainLua := filepath.Join(root, "plugins", "core-communication", "main.lua")
	raw, err := os.ReadFile(mainLua)
	require.NoError(t, err, "read core-communication main.lua")
	src := string(raw)

	for _, verb := range corecommAlwaysVerbs {
		t.Run(verb, func(t *testing.T) {
			typeKey := `type = "` + verb + `"`
			tables := enclosingEmitTables(src, typeKey)
			require.NotEmpty(t, tables,
				"expected at least one emit table with %s in main.lua", typeKey)
			for _, table := range tables {
				require.True(t, sensitiveClaimPattern.MatchString(table),
					"emit table for %s MUST set `sensitive = true` (sensitivity:always per crypto.emits; "+
						"without it the host fence rejects the emit fail-closed). Offending table:\n  %s",
					verb, strings.TrimSpace(table))
			}
		})
	}
}

// TestCoreCommunicationEmitUsesQualifiedWireType pins holomush-aneim: the
// generic `emit` command in main.lua MUST emit the plugin-qualified wire type
// `core-communication:emit`, matching its verbs[].type declaration. A bare
// `type = "emit"` fails RenderingPublisher.Lookup with EMIT_UNKNOWN_VERB in
// production (the verb registry is keyed on the qualified type).
func TestCoreCommunicationEmitUsesQualifiedWireType(t *testing.T) {
	root := repoRoot(t)
	mainLua := filepath.Join(root, "plugins", "core-communication", "main.lua")
	raw, err := os.ReadFile(mainLua)
	require.NoError(t, err, "read core-communication main.lua")
	src := string(raw)

	require.Contains(t, src, `type = "core-communication:emit"`,
		"the generic emit command MUST emit the qualified wire type (matches verbs[].type; holomush-aneim)")
	require.NotContains(t, src, `type = "emit"`,
		"bare emit wire type must be gone (EMIT_UNKNOWN_VERB in production)")
}

// enclosingEmitTables returns, for every occurrence of needle in src, the text
// of the innermost brace-delimited table { ... } that encloses it. Brace
// matching is scoped to the nearest unbalanced "{" before the needle and its
// matching "}" after — sufficient for core-communication's emit tables, whose
// values are plain string concatenations and variables with no nested braces
// between the type key and the table's closing brace.
func enclosingEmitTables(src, needle string) []string {
	var out []string
	for off := 0; ; {
		idx := strings.Index(src[off:], needle)
		if idx < 0 {
			break
		}
		pos := off + idx
		open := strings.LastIndexByte(src[:pos], '{')
		closeRel := strings.IndexByte(src[pos:], '}')
		if open >= 0 && closeRel >= 0 {
			out = append(out, src[open:pos+closeRel+1])
		}
		off = pos + len(needle)
	}
	return out
}
