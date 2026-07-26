// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// This file guards test/session-matrix.yaml — the committed session-lifecycle
// matrix registry. It lands WITH the registry rather than after it because
// three later plans consume the registry as their division of labour: without a
// shape test they would be reading a table whose basic integrity rests on a
// human having counted 48 cells correctly once.
//
// Scope note: this file asserts SHAPE only (row count, id uniqueness,
// disposition exclusivity, the not-applicable count). The marker bijection —
// every `disposition: spec` row has a matching `// matrix-row: <id>` comment
// and vice versa — is added to this same file by a later plan, once specs
// carrying markers exist. Asserting it now would fail on an empty marker set.

// sessionMatrixTotalPositions is 12 transitions x 4 transport columns. Every
// position gets a row, including the ones the source matrix marks n/a, so that
// a dropped cell is a count failure rather than a silent omission.
const sessionMatrixTotalPositions = 48

// sessionMatrixNotApplicable is the number of positions the source matrix
// (holomush-izk0, reproduced verbatim in 09-RESEARCH.md) marks `n/a`:
// multi-session on the fresh-select, detach-all, reaper-sweep,
// post-ttl-relogin and quit-command rows (5); web-guest on the admin-boot and
// move-arrival rows (2); and web-guest, web-char and multi-session on the
// telnet-only tmux-reattach row (3).
//
// Pinning the number is what stops a future edit from quietly reclassifying an
// awkward-to-cover cell as "not applicable" — the cheapest way to turn a
// coverage gap into an apparent non-question.
const sessionMatrixNotApplicable = 10

// sessionMatrixPayloadKeys maps each disposition to the single payload key it
// owns. A row MUST carry its own disposition's key and MUST NOT carry any of
// the other four. `notes` and `issue` are deliberately absent from this map:
// they are free-form annotations permitted on any row, so they cannot imply a
// disposition.
var sessionMatrixPayloadKeys = map[string]string{
	"spec":              "spec",
	"covered-elsewhere": "covered_by",
	"planned":           "owed_by",
	"not-applicable":    "reason",
	"not-implementable-from-harness-defaults": "gap_notes",
}

// sessionMatrixRow is decoded permissively: payload presence is checked against
// the raw map so a typo'd key is a missing-payload failure rather than being
// silently dropped into a struct field that was never populated.
type sessionMatrixRow struct {
	ID          string `yaml:"id"`
	Transition  string `yaml:"transition"`
	Column      string `yaml:"column"`
	Disposition string `yaml:"disposition"`
}

type sessionMatrixRegistry struct {
	Rows []map[string]any `yaml:"rows"`
}

// TestSessionMatrixRegistryShape asserts the structural properties a reader of
// test/session-matrix.yaml would otherwise have to take on trust.
func TestSessionMatrixRegistryShape(t *testing.T) {
	rows := loadSessionMatrixRows(t)

	// Compare lengths rather than using require.Len, which renders the whole
	// 48-row collection into the failure message and buries the count.
	require.Equal(t, sessionMatrixTotalPositions, len(rows),
		"test/session-matrix.yaml MUST carry one row per position in the 12x4 matrix; "+
			"a differing count means a cell was dropped or invented")

	seen := make(map[string]int, len(rows))
	notApplicable := 0

	for i, raw := range rows {
		row := decodeSessionMatrixRow(t, i, raw)

		require.NotEmpty(t, row.ID, "row %d MUST carry an id (the bijection key)", i)
		require.NotEmpty(t, row.Transition, "row %q MUST name its transition", row.ID)
		require.NotEmpty(t, row.Column, "row %q MUST name its transport column", row.ID)

		if prev, dup := seen[row.ID]; dup {
			t.Fatalf("duplicate row id %q at rows[%d]; already used at rows[%d]. "+
				"The id is the bijection key, so a duplicate would let two cells "+
				"share one spec marker", row.ID, i, prev)
		}
		seen[row.ID] = i

		ownKey, known := sessionMatrixPayloadKeys[row.Disposition]
		require.Truef(t, known,
			"row %q has unknown disposition %q; permitted values are %v",
			row.ID, row.Disposition, sessionMatrixDispositions())

		require.Containsf(t, raw, ownKey,
			"row %q declares disposition %q but is missing its required payload key %q",
			row.ID, row.Disposition, ownKey)

		for disposition, key := range sessionMatrixPayloadKeys {
			if key == ownKey {
				continue
			}
			require.NotContainsf(t, raw, key,
				"row %q declares disposition %q but also carries %q, the payload of %q. "+
					"A row carries exactly one disposition; two payloads means the cell's "+
					"real status is ambiguous",
				row.ID, row.Disposition, key, disposition)
		}

		if _, hasBlockedOn := raw["blocked_on"]; hasBlockedOn {
			require.Equalf(t, "planned", row.Disposition,
				"row %q carries blocked_on, which names a missing seam and is meaningful "+
					"only on a planned row", row.ID)
		}

		if row.Disposition == "not-applicable" {
			notApplicable++
		}
	}

	require.Equal(t, sessionMatrixNotApplicable, notApplicable,
		"the registry MUST mark exactly the %d positions the source matrix marks n/a; "+
			"a higher count means a coverage gap was reclassified as a non-question",
		sessionMatrixNotApplicable)
}

// loadSessionMatrixRows reads and parses the registry. Every failure is fatal:
// a read or parse error MUST NOT degrade into an empty row set, which would
// trivially satisfy a "no bad rows" loop and report success on an unreadable
// registry.
func loadSessionMatrixRows(t *testing.T) []map[string]any {
	t.Helper()

	path := filepath.Join(findRepoRoot(t), "test", "session-matrix.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err, "read test/session-matrix.yaml")

	var registry sessionMatrixRegistry
	require.NoError(t, yaml.Unmarshal(data, &registry),
		"test/session-matrix.yaml MUST parse as YAML")
	require.NotEmpty(t, registry.Rows,
		"test/session-matrix.yaml parsed to zero rows; the top-level key MUST be `rows`")

	return registry.Rows
}

// decodeSessionMatrixRow re-encodes one raw row so the typed fields are read by
// the same YAML decoder that produced the raw map, keeping the two views in
// agreement.
func decodeSessionMatrixRow(t *testing.T, i int, raw map[string]any) sessionMatrixRow {
	t.Helper()

	encoded, err := yaml.Marshal(raw)
	require.NoErrorf(t, err, "re-encode rows[%d]", i)

	var row sessionMatrixRow
	require.NoErrorf(t, yaml.Unmarshal(encoded, &row), "decode rows[%d]", i)
	return row
}

func sessionMatrixDispositions() []string {
	out := make([]string, 0, len(sessionMatrixPayloadKeys))
	for d := range sessionMatrixPayloadKeys {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}
