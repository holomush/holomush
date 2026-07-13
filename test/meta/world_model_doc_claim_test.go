// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// The F1 archaeology (issue #4784, ADR holomush-i4784) established that world
// state was never rebuilt by replaying events — locations, exits, characters,
// and objects have always been direct-write CRUD in PostgreSQL. MODEL-02
// downgraded the doc sites that stated the false principle to the decided
// model: event-driven with an append-only audit log. This meta-test guards
// that correction so a future edit cannot silently re-introduce the false
// world-state-reconstruction claim at the guarded sites.
//
// The exact failure mode this guards is a doc claim silently diverging from
// the code's reality (the same class as depguard_config_test.go). It matches a
// SET of semantically-equivalent phrasings, not one exact old sentence, so a
// reworded-but-still-false claim also trips it.

// falseWorldStateClaimPatterns match a claim that WORLD / CURRENT / GAME state
// is produced by replaying / deriving from the event log. Each is scoped to the
// "state ... from events" shape so legitimate client-catch-up / Subscribe
// reconnect replay language (e.g. "Reconnecting clients catch up from their
// last seen event", "session persistence and event replay") does not trip.
var falseWorldStateClaimPatterns = []*regexp.Regexp{
	// "state derives from replay", "state derives from the event log"
	regexp.MustCompile(`(?i)\bstate\s+derives\s+from\s+(the\s+)?(event\s+)?(replay|log)`),
	// "state is derived from event replay", "current state is derived from the event log"
	regexp.MustCompile(`(?i)\bstate\s+is\s+derived\s+from\s+(the\s+)?event`),
	// "state is (re)constructed / rebuilt from events / the event log / replay"
	regexp.MustCompile(`(?i)\bstate\s+is\s+(re-?constructed|rebuilt|reconstituted)\s+from\s+(the\s+)?(event|replay)`),
	// "event-sourced architecture / world state / current state / state"
	regexp.MustCompile(`(?i)event-sourced\s+(architecture|world|current\s+state|state)`),
	// A "### Event Sourcing" section heading stating it as a principle.
	regexp.MustCompile(`(?im)^#{1,6}\s+event\s+sourcing\s*$`),
}

// guardedDocSites are the files MODEL-02 corrected. The whole file is scanned:
// the corrected text intentionally carries none of the false phrasings above
// (it describes the positive model), so a full-file scan is a strict guard.
func guardedDocSites() []struct {
	path    string
	require []string // substrings the corrected site MUST contain
} {
	return []struct {
		path    string
		require []string
	}{
		{
			path:    "CLAUDE.md",
			require: []string{"append-only", "holomush-i4784"},
		},
		{
			path:    "README.md",
			require: []string{"event-driven"},
		},
		{
			path:    filepath.Join("site", "src", "content", "docs", "contributing", "reference", "coding-standards.md"),
			require: []string{"append-only", "holomush-i4784"},
		},
		{
			path:    filepath.Join("site", "src", "content", "docs", "contributing", "explanation", "architecture.md"),
			require: []string{"append-only", "holomush-i4784"},
		},
	}
}

// TestWorldModelDocsDoNotClaimReplayDerivedState asserts none of the guarded
// doc sites state that world/current state is produced by replaying or deriving
// from the event log (MODEL-02, ADR holomush-i4784). It fails if a future edit
// re-introduces the false claim at a guarded site.
func TestWorldModelDocsDoNotClaimReplayDerivedState(t *testing.T) {
	root := findRepoRoot(t)
	for _, site := range guardedDocSites() {
		data, err := os.ReadFile(filepath.Join(root, site.path))
		require.NoErrorf(t, err, "read guarded doc site %q", site.path)
		body := string(data)

		for _, pat := range falseWorldStateClaimPatterns {
			require.NotRegexpf(t, pat, body,
				"guarded doc site %q re-introduced a replay-derived-world-state claim matching %q — "+
					"the decided model (ADR holomush-i4784) is event-driven with an append-only audit log; "+
					"world state is canonical in PostgreSQL, not rebuilt from events",
				site.path, pat.String())
		}
	}
}

// TestWorldModelDocsStateDecidedModel asserts each corrected site describes the
// decided model (event-driven / append-only audit log) and that the
// architectural sites reference the authoritative ADR so a future editor sees
// the decision and does not re-introduce the false principle.
func TestWorldModelDocsStateDecidedModel(t *testing.T) {
	root := findRepoRoot(t)
	for _, site := range guardedDocSites() {
		data, err := os.ReadFile(filepath.Join(root, site.path))
		require.NoErrorf(t, err, "read guarded doc site %q", site.path)
		body := string(data)

		for _, want := range site.require {
			require.Containsf(t, body, want,
				"guarded doc site %q must describe the decided world-state model (missing %q) — "+
					"see ADR holomush-i4784", site.path, want)
		}
	}
}

// TestLegitimateClientCatchupReplayIsPreserved asserts the correction did NOT
// over-reach: the genuinely-real client-catch-up / Subscribe reconnect replay
// language (which IS true) is left intact. This pins the scope discipline so a
// mechanical "delete every 'replay'" future edit is caught.
func TestLegitimateClientCatchupReplayIsPreserved(t *testing.T) {
	root := findRepoRoot(t)

	arch := filepath.Join("site", "src", "content", "docs", "contributing", "explanation", "architecture.md")
	data, err := os.ReadFile(filepath.Join(root, arch))
	require.NoError(t, err, "read architecture.md")
	require.Regexp(t, regexp.MustCompile(`(?i)reconnecting\s+clients\s+catch\s+up`), string(data),
		"the real client-catch-up replay language MUST be preserved (Codex finding 17)")

	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	require.NoError(t, err, "read README.md")
	require.Regexp(t, regexp.MustCompile(`(?i)session\s+persistence\s+and\s+event\s+replay`), string(readme),
		"README's legitimate reconnect/catch-up replay language MUST be preserved (Codex finding 17)")
}
