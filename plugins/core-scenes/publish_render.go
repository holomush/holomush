// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/samber/oops"
)

// renderMarkdown renders published scene content entries to well-formed
// Markdown (spec §12). Poses bold the speaker; says wrap the line in quotes;
// emits italicize the content. Speaker and content are escaped (escapeMarkdown)
// so user-authored text cannot inject Markdown syntax. An empty entry list
// yields a sentinel line so a published-but-empty scene still renders
// meaningfully rather than as a blank document.
func renderMarkdown(entries []PublishedSceneEntry) string {
	if len(entries) == 0 {
		return "_No content was recorded for this scene._\n"
	}
	var b strings.Builder
	for _, e := range entries {
		switch e.Kind {
		case EntryKindPose:
			fmt.Fprintf(&b, "**%s** %s\n\n", escapeMarkdown(e.Speaker), escapeMarkdown(e.Content))
		case EntryKindSay:
			fmt.Fprintf(&b, "**%s** says, \"%s\"\n\n", escapeMarkdown(e.Speaker), escapeMarkdown(e.Content))
		case EntryKindEmit:
			fmt.Fprintf(&b, "_%s_\n\n", escapeMarkdown(e.Content))
		default:
			// content_entries are pre-filtered to pose/say/emit at snapshot
			// time (C6 ReadSceneLogForSnapshot), so this is unreachable in
			// practice — but never silently drop an unexpected kind: render
			// its content as a plain paragraph so no data is lost.
			fmt.Fprintf(&b, "%s\n\n", escapeMarkdown(e.Content))
		}
	}
	return b.String()
}

// renderPlainText renders published scene content entries to plain text
// (spec §12) — the same per-kind phrasing as Markdown but with NO markup:
// poses and says are prefixed by the speaker, says wrap content in quotes,
// emits are speakerless. Content is emitted verbatim (plain text has no
// metacharacters to escape). An empty entry list yields a plain sentinel line.
func renderPlainText(entries []PublishedSceneEntry) string {
	if len(entries) == 0 {
		return "No content was recorded for this scene.\n"
	}
	var b strings.Builder
	for _, e := range entries {
		switch e.Kind {
		case EntryKindPose:
			fmt.Fprintf(&b, "%s %s\n", e.Speaker, e.Content)
		case EntryKindSay:
			fmt.Fprintf(&b, "%s says, \"%s\"\n", e.Speaker, e.Content)
		case EntryKindEmit:
			fmt.Fprintf(&b, "%s\n", e.Content)
		default:
			// content_entries are pre-filtered to pose/say/emit at snapshot
			// time (C6); never silently drop an unexpected kind.
			fmt.Fprintf(&b, "%s\n", e.Content)
		}
	}
	return b.String()
}

// renderJSONL renders published scene content entries to JSON Lines (spec §12)
// — one JSON object per line, `{"speaker":...,"kind":...,"content":...}`. Key
// order is stable (PublishedSceneEntry's field declaration order), so output is
// diff-friendly. Empty entries yield empty output: zero records is the correct
// JSONL representation of an empty scene (a sentinel object would be a
// machine-readable lie). json.Marshal of an all-string struct cannot fail in
// practice, but the error is propagated rather than swallowed.
func renderJSONL(entries []PublishedSceneEntry) ([]byte, error) {
	var b bytes.Buffer
	for i := range entries {
		line, err := json.Marshal(entries[i])
		if err != nil {
			return nil, oops.Code("SCENE_PUBLISH_JSONL_MARSHAL_FAILED").
				With("index", i).Wrap(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}

// escapeMarkdown backslash-escapes the Markdown metacharacters * _ [ ] ` \ so
// user content renders literally. It is rune-safe: multibyte and emoji runes
// pass through unchanged (only the listed ASCII metacharacters are escaped).
func escapeMarkdown(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '*', '_', '[', ']', '`', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
