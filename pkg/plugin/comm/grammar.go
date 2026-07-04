// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package comm holds the sigil/style grammar and JSON builders shared by both
// plugin runtimes for producing holomush.comm.v1.CommunicationContent
// payloads, so Lua and binary plugins parse ";"/":" pose and OOC sigils
// identically (see .claude/rules/plugin-runtime-symmetry.md).
package comm

import "strings"

// Author identifies the character whose action or speech is being recorded in
// a CommunicationContent payload.
type Author struct{ ID, Name string }

// PoseParse is the result of applying the ";"/":" pose grammar to a raw pose
// invocation.
type PoseParse struct {
	Text    string
	NoSpace bool
}

// OOCParse is the result of classifying an out-of-character message's surface
// style ("say" / "pose" / "semipose") from its leading sigil.
type OOCParse struct{ Text, Style string }

// ParsePose applies the ";"/":" pose grammar. invokedAs is the alias that fired
// (";" -> semipose/no-space, ":" -> pose); when empty, a leading sigil in raw is
// honored instead (mirrors main.lua handle_pose).
func ParsePose(invokedAs, raw string) PoseParse {
	a := strings.TrimSpace(raw)
	switch invokedAs {
	case ";":
		return PoseParse{Text: a, NoSpace: true}
	case ":":
		return PoseParse{Text: a, NoSpace: false}
	}
	if strings.HasPrefix(a, ";") {
		return PoseParse{Text: strings.TrimSpace(a[1:]), NoSpace: true}
	}
	if strings.HasPrefix(a, ":") {
		return PoseParse{Text: strings.TrimSpace(a[1:]), NoSpace: false}
	}
	return PoseParse{Text: a}
}

// ParseOOC classifies the OOC style from a leading sigil (mirrors handle_ooc).
func ParseOOC(raw string) OOCParse {
	m := strings.TrimSpace(raw)
	if strings.HasPrefix(m, ":") {
		return OOCParse{Text: strings.TrimSpace(m[1:]), Style: "pose"}
	}
	if strings.HasPrefix(m, ";") {
		return OOCParse{Text: strings.TrimSpace(m[1:]), Style: "semipose"}
	}
	return OOCParse{Text: m, Style: "say"}
}
