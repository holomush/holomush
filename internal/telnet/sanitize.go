// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"strings"
	"unicode/utf8"
)

// sanitizeTelnetOutput removes terminal control sequences and control
// characters from a string before it is written to a telnet connection.
//
// User-derived content (character names, chat messages, plugin output)
// is flattened through this at the send boundary so a crafted payload
// cannot clear the terminal, reposition the cursor, rewrite history, or
// spoof system messages via ANSI escape sequences.
//
// Stripped:
//   - ANSI CSI sequences: ESC '[' ... final-byte (0x40-0x7E).
//   - ANSI OSC sequences: ESC ']' ... terminated by BEL or ESC '\' (ST).
//   - Other ESC-introduced sequences (ESC + single byte).
//   - 8-bit C1 introducers for CSI (0x9B) and OSC (0x9D).
//   - C0 control characters (0x00-0x1F) except newline, carriage return, and tab.
//   - DEL (0x7F).
//   - C1 control characters (0x80-0x9F).
//
// Valid UTF-8 code points outside the control ranges are preserved.
func sanitizeTelnetOutput(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == 0x1B: // ESC: start of an ANSI escape sequence.
			i = skipEscapeSequence(s, i+size)
		case r == 0x9B: // C1 CSI introducer (single-byte form).
			i = skipCSIParams(s, i+size)
		case r == 0x9D: // C1 OSC introducer (single-byte form).
			i = skipOSCBody(s, i+size)
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteRune(r)
			i += size
		case r < 0x20, r == 0x7F, r >= 0x80 && r <= 0x9F:
			i += size
		case r == utf8.RuneError && size == 1:
			// Invalid UTF-8 byte; drop it rather than emit replacement noise.
			i += size
		default:
			b.WriteRune(r)
			i += size
		}
	}
	return b.String()
}

// skipEscapeSequence consumes an ANSI escape sequence that starts with
// ESC at position i-1 and returns the index past the end of the sequence.
// A bare ESC with no following byte is consumed on its own.
func skipEscapeSequence(s string, i int) int {
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '[':
		return skipCSIParams(s, i+1)
	case ']':
		return skipOSCBody(s, i+1)
	default:
		// Two-byte ESC sequence (e.g. ESC 7, ESC M); drop the second byte.
		return i + 1
	}
}

// skipCSIParams consumes the parameter, intermediate, and final bytes of
// an ANSI CSI sequence starting at i and returns the index past the final
// byte. An unterminated CSI runs to end-of-string.
func skipCSIParams(s string, i int) int {
	for i < len(s) {
		c := s[i]
		i++
		// CSI terminates on a byte in the range 0x40-0x7E.
		if c >= 0x40 && c <= 0x7E {
			return i
		}
	}
	return i
}

// skipOSCBody consumes an ANSI OSC sequence body starting at i, stopping
// after BEL (0x07) or a String Terminator (ESC '\' or the C1 0x9C).
// An unterminated OSC runs to end-of-string.
func skipOSCBody(s string, i int) int {
	for i < len(s) {
		c := s[i]
		if c == 0x07 { // BEL terminator.
			return i + 1
		}
		if c == 0x1B && i+1 < len(s) && s[i+1] == '\\' { // ESC \ (ST).
			return i + 2
		}
		if c == 0x9C { // C1 ST.
			return i + 1
		}
		i++
	}
	return i
}
