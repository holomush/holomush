// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
