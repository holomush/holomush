// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderMarkdownRendersEntriesByKind verifies each entry kind gets its
// distinct Markdown shape: pose bolds the speaker, say wraps in quotes, emit
// italicizes (spec §12).
func TestRenderMarkdownRendersEntriesByKind(t *testing.T) {
	t.Parallel()
	entries := []PublishedSceneEntry{
		{Speaker: "Alice", Kind: EntryKindPose, Content: "smiles warmly."},
		{Speaker: "Bob", Kind: EntryKindSay, Content: "Hello there."},
		{Speaker: "Cara", Kind: EntryKindEmit, Content: "A bell rings in the distance."},
	}

	got := renderMarkdown(entries)

	assert.Contains(t, got, "**Alice** smiles warmly.\n\n")
	assert.Contains(t, got, "**Bob** says, \"Hello there.\"\n\n")
	assert.Contains(t, got, "_A bell rings in the distance._\n\n")
}

// TestRenderMarkdownReturnsSentinelForEmptyEntries verifies an empty scene
// renders the human-readable sentinel rather than an empty document.
func TestRenderMarkdownReturnsSentinelForEmptyEntries(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "_No content was recorded for this scene._\n", renderMarkdown(nil))
	assert.Equal(t, "_No content was recorded for this scene._\n", renderMarkdown([]PublishedSceneEntry{}))
}

// TestRenderMarkdownEscapesMetacharacters verifies user-authored text cannot
// inject Markdown syntax: * _ [ ] ` and \ are backslash-escaped in both
// speaker and content.
func TestRenderMarkdownEscapesMetacharacters(t *testing.T) {
	t.Parallel()
	entries := []PublishedSceneEntry{
		{Speaker: "A*b_c", Kind: EntryKindPose, Content: "x[y]`z`\\w *bold*"},
	}

	got := renderMarkdown(entries)

	assert.Contains(t, got, "A\\*b\\_c", "speaker metacharacters must be escaped")
	assert.Contains(t, got, "x\\[y\\]\\`z\\`\\\\w \\*bold\\*", "content metacharacters must be escaped")
}

// TestRenderMarkdownPreservesUnicodeAndEmoji verifies multibyte runes and emoji
// pass through unchanged — escapeMarkdown only touches the ASCII metacharacters.
func TestRenderMarkdownPreservesUnicodeAndEmoji(t *testing.T) {
	t.Parallel()
	entries := []PublishedSceneEntry{
		{Speaker: "Élodie 😀", Kind: EntryKindSay, Content: "café ☕ 日本語 — naïve"},
	}

	got := renderMarkdown(entries)

	assert.Contains(t, got, "Élodie 😀")
	assert.Contains(t, got, "café ☕ 日本語 — naïve")
}

// TestRenderPlainTextMatchesGolden pins the plain-text rendering byte-for-byte
// against the checked-in golden file: per-kind phrasing with no Markdown markup.
func TestRenderPlainTextMatchesGolden(t *testing.T) {
	t.Parallel()
	entries := []PublishedSceneEntry{
		{Speaker: "Alice", Kind: EntryKindPose, Content: "smiles warmly."},
		{Speaker: "Bob", Kind: EntryKindSay, Content: "Hello there."},
		{Speaker: "Cara", Kind: EntryKindEmit, Content: "A bell rings in the distance."},
	}

	got := renderPlainText(entries)

	want, err := os.ReadFile(filepath.Join("testdata", "publish_render_plain_text.golden"))
	require.NoError(t, err)
	assert.Equal(t, string(want), got)
}

// TestRenderPlainTextReturnsSentinelForEmptyEntries verifies an empty scene
// renders the plain sentinel (no Markdown italics, unlike renderMarkdown).
func TestRenderPlainTextReturnsSentinelForEmptyEntries(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "No content was recorded for this scene.\n", renderPlainText(nil))
	assert.Equal(t, "No content was recorded for this scene.\n", renderPlainText([]PublishedSceneEntry{}))
}

// TestRenderPlainTextEmitsNoMarkdownMarkup verifies the plain-text renderer
// adds no markup: even content that itself contains Markdown metacharacters is
// passed through verbatim, and no bold/italic markers are introduced.
func TestRenderPlainTextEmitsNoMarkdownMarkup(t *testing.T) {
	t.Parallel()
	entries := []PublishedSceneEntry{
		{Speaker: "Alice", Kind: EntryKindPose, Content: "waves."},
	}

	got := renderPlainText(entries)

	assert.Equal(t, "Alice waves.\n", got)
	assert.NotContains(t, got, "**", "plain text must not introduce Markdown bold markup")
	assert.NotContains(t, got, "_", "plain text must not introduce Markdown italic markup")
}

// TestRenderJSONLRoundTrips verifies one JSON object per line that survives a
// marshal → split → unmarshal cycle back to the original slice (including
// content with an embedded quote, which JSON escaping must handle).
func TestRenderJSONLRoundTrips(t *testing.T) {
	t.Parallel()
	entries := []PublishedSceneEntry{
		{Speaker: "Alice", Kind: EntryKindPose, Content: "smiles warmly."},
		{Speaker: "Bob", Kind: EntryKindSay, Content: `Hello "there".`},
		{Speaker: "Cara", Kind: EntryKindEmit, Content: "A bell rings."},
	}

	got, err := renderJSONL(entries)
	require.NoError(t, err)

	lines := bytes.Split(bytes.TrimRight(got, "\n"), []byte("\n"))
	require.Len(t, lines, len(entries))
	round := make([]PublishedSceneEntry, 0, len(lines))
	for _, ln := range lines {
		var e PublishedSceneEntry
		require.NoError(t, json.Unmarshal(ln, &e))
		round = append(round, e)
	}
	assert.Equal(t, entries, round)
}

// TestRenderJSONLEmitsStableKeyOrder pins the key order (speaker, kind, content)
// for diff-friendliness — the contract spec §12 calls out explicitly.
func TestRenderJSONLEmitsStableKeyOrder(t *testing.T) {
	t.Parallel()
	got, err := renderJSONL([]PublishedSceneEntry{
		{Speaker: "Alice", Kind: EntryKindSay, Content: "Hi."},
	})
	require.NoError(t, err)
	assert.Equal(t, `{"speaker":"Alice","kind":"say","content":"Hi."}`+"\n", string(got))
}

// TestRenderJSONLEmptyEntriesYieldsEmptyOutput verifies an empty scene renders
// zero records (not a sentinel object) — the correct JSONL representation.
func TestRenderJSONLEmptyEntriesYieldsEmptyOutput(t *testing.T) {
	t.Parallel()
	got, err := renderJSONL(nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}
