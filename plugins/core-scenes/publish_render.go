// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"fmt"
	"strings"
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
