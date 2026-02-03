// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holo

import (
	"fmt"
	"strconv"
	"strings"
)

// codeToANSI maps MU* format codes to ANSI escape sequences.
// Style codes: n (reset), h (bold), u (underline), i (italic), d (dim)
// Color codes: r/R (red), g/G (green), b/B (blue), c/C (cyan),
//
//	m/M (magenta), y/Y (yellow), w/W (white), x (black)
//
// Lowercase = normal, uppercase = bright
var codeToANSI = map[string]string{
	// Style codes
	"n": ansiReset,
	"h": ansiBold,
	"u": ansiUnderline,
	"i": ansiItalic,
	"d": ansiDim,

	// Normal color codes
	"r": ansiRed,
	"g": ansiGreen,
	"b": ansiBlue,
	"c": ansiCyan,
	"m": ansiMagenta,
	"y": ansiYellow,
	"w": ansiWhite,
	"x": ansiBlack,

	// Bright color codes
	"R": ansiBrightRed,
	"G": ansiBrightGreen,
	"B": ansiBrightBlue,
	"C": ansiBrightCyan,
	"M": ansiBrightMagenta,
	"Y": ansiBrightYellow,
	"W": ansiBrightWhite,
}

// whitespaceCodeToOutput maps whitespace codes to their output.
var whitespaceCodeToOutput = map[rune]string{
	'r': "\n",   // newline
	'b': " ",    // space
	't': "    ", // tab (4 spaces)
}

// Parse converts text containing MU* %x format codes to StyledText.
//
// Supported codes:
//   - Style: %xn (reset), %xh (bold), %xu (underline), %xi (italic), %xd (dim)
//   - Colors: %xr/%xR (red), %xg/%xG (green), %xb/%xB (blue), %xc/%xC (cyan),
//     %xm/%xM (magenta), %xy/%xY (yellow), %xw/%xW (white), %xx (black)
//   - 256-color: %x### where ### is a 3-digit color number (000-255)
//   - Whitespace: %r (newline), %b (space), %t (tab)
//
// Unknown codes are preserved as-is. Percent signs not followed by a valid
// code are also preserved.
func (f formatter) Parse(text string) StyledText {
	if text == "" {
		return StyledText{}
	}

	var result strings.Builder
	i := 0

	for i < len(text) {
		if text[i] == '%' && i+1 < len(text) {
			// Check for whitespace codes (%r, %b, %t)
			nextChar := rune(text[i+1])
			if ws, ok := whitespaceCodeToOutput[nextChar]; ok {
				result.WriteString(ws)
				i += 2
				continue
			}

			// Check for %x codes
			if text[i+1] == 'x' && i+2 < len(text) {
				// Try to parse 256-color code (%x###)
				if i+4 < len(text) && isDigit(text[i+2]) && isDigit(text[i+3]) && isDigit(text[i+4]) {
					colorNum := text[i+2 : i+5]
					num, err := strconv.Atoi(colorNum)
					if err == nil && num >= 0 && num <= 255 {
						result.WriteString(fmt.Sprintf("\x1b[38;5;%dm", num))
						i += 5
						continue
					}
				}

				// Try single-character code
				code := string(text[i+2])
				if ansi, ok := codeToANSI[code]; ok {
					result.WriteString(ansi)
					i += 3
					continue
				}
			}

			// Unknown code, preserve as-is
			result.WriteByte(text[i])
			i++
		} else {
			result.WriteByte(text[i])
			i++
		}
	}

	return PlainText(result.String())
}

// isDigit returns true if the byte is an ASCII digit.
func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
