// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holo

import (
	"fmt"
	"sort"
	"strings"
)

// ANSI escape code constants
const (
	ansiReset     = "\x1b[0m"
	ansiBold      = "\x1b[1m"
	ansiDim       = "\x1b[2m"
	ansiItalic    = "\x1b[3m"
	ansiUnderline = "\x1b[4m"
)

// ANSI color codes
const (
	ansiBlack   = "\x1b[30m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
	ansiCyan    = "\x1b[36m"
	ansiWhite   = "\x1b[37m"
)

// Bright ANSI color codes (used by %xR bright color codes)
const (
	ansiBrightBlack   = "\x1b[90m"
	ansiBrightRed     = "\x1b[91m"
	ansiBrightGreen   = "\x1b[92m"
	ansiBrightYellow  = "\x1b[93m"
	ansiBrightBlue    = "\x1b[94m"
	ansiBrightMagenta = "\x1b[95m"
	ansiBrightCyan    = "\x1b[96m"
	ansiBrightWhite   = "\x1b[97m"
)

// colorNameToANSI maps color names to ANSI codes
var colorNameToANSI = map[string]string{
	"black":   ansiBlack,
	"red":     ansiRed,
	"green":   ansiGreen,
	"yellow":  ansiYellow,
	"blue":    ansiBlue,
	"magenta": ansiMagenta,
	"cyan":    ansiCyan,
	"white":   ansiWhite,
}

// Fmt provides text formatting functions.
// Use these methods to create styled text that can render to multiple formats.
var Fmt formatter

// formatter provides the implementation for Fmt.
type formatter struct{}

// StyledText represents formatted text with styling information.
// It is an intermediate representation that can be rendered to different
// output formats (ANSI for telnet, HTML for web, etc.).
type StyledText struct {
	segments []segment
}

// segment represents a piece of text with optional styling.
type segment struct {
	text  string
	style style
}

// style represents styling options for a segment.
type style struct {
	bold      bool
	italic    bool
	dim       bool
	underline bool
	color     string // ANSI color code or empty
}

// Bold returns styled text with bold formatting.
func (f formatter) Bold(text string) StyledText {
	return StyledText{
		segments: []segment{
			{text: text, style: style{bold: true}},
		},
	}
}

// Italic returns styled text with italic formatting.
func (f formatter) Italic(text string) StyledText {
	return StyledText{
		segments: []segment{
			{text: text, style: style{italic: true}},
		},
	}
}

// Dim returns styled text with dimmed formatting.
func (f formatter) Dim(text string) StyledText {
	return StyledText{
		segments: []segment{
			{text: text, style: style{dim: true}},
		},
	}
}

// Underline returns styled text with underline formatting.
func (f formatter) Underline(text string) StyledText {
	return StyledText{
		segments: []segment{
			{text: text, style: style{underline: true}},
		},
	}
}

// Color returns styled text with the specified color.
// Supported colors: red, green, blue, cyan, magenta, yellow, white, black.
// Unknown colors result in plain unstyled text.
func (f formatter) Color(color, text string) StyledText {
	ansiColor, ok := colorNameToANSI[color]
	if !ok {
		return PlainText(text)
	}
	return StyledText{
		segments: []segment{
			{text: text, style: style{color: ansiColor}},
		},
	}
}

// List formats items as a bulleted list.
func (f formatter) List(items []string) StyledText {
	if len(items) == 0 {
		return StyledText{}
	}

	var lines []string
	for _, item := range items {
		lines = append(lines, "  - "+item)
	}
	return PlainText(strings.Join(lines, "\n"))
}

// Pairs formats a map as key-value pairs.
// Keys are sorted alphabetically for consistent output.
func (f formatter) Pairs(pairs map[string]any) StyledText {
	if len(pairs) == 0 {
		return StyledText{}
	}

	// Sort keys for consistent output
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var lines []string
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("%s: %v", k, pairs[k]))
	}
	return PlainText(strings.Join(lines, "\n"))
}

// TableOpts configures table formatting.
type TableOpts struct {
	Headers []string
	Rows    [][]string
}

// Table formats data as a table with headers and rows.
// Columns are automatically aligned based on content width.
func (f formatter) Table(opts TableOpts) StyledText {
	// Calculate column widths
	colCount := len(opts.Headers)
	if colCount == 0 && len(opts.Rows) > 0 {
		colCount = len(opts.Rows[0])
	}
	if colCount == 0 {
		return StyledText{}
	}

	widths := make([]int, colCount)

	// Account for header widths
	for i, h := range opts.Headers {
		if i < colCount && len(h) > widths[i] {
			widths[i] = len(h)
		}
	}

	// Account for row widths
	for _, row := range opts.Rows {
		for i, cell := range row {
			if i < colCount && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	var lines []string

	// Render headers
	if len(opts.Headers) > 0 {
		var headerCells []string
		for i, h := range opts.Headers {
			if i < colCount {
				headerCells = append(headerCells, padRight(h, widths[i]))
			}
		}
		lines = append(lines, strings.Join(headerCells, "  "))

		// Separator line
		var sepCells []string
		for i := 0; i < colCount; i++ {
			sepCells = append(sepCells, strings.Repeat("-", widths[i]))
		}
		lines = append(lines, strings.Join(sepCells, "  "))
	}

	// Render rows
	for _, row := range opts.Rows {
		var rowCells []string
		for i := 0; i < colCount; i++ {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			rowCells = append(rowCells, padRight(cell, widths[i]))
		}
		lines = append(lines, strings.Join(rowCells, "  "))
	}

	return PlainText(strings.Join(lines, "\n"))
}

// Separator returns a horizontal separator line.
func (f formatter) Separator() StyledText {
	return PlainText(strings.Repeat("-", 40))
}

// Header returns styled text formatted as a section header.
func (f formatter) Header(text string) StyledText {
	if text == "" {
		return StyledText{}
	}
	return StyledText{
		segments: []segment{
			{text: text, style: style{bold: true}},
		},
	}
}

// PlainText creates unstyled text.
func PlainText(text string) StyledText {
	return StyledText{
		segments: []segment{
			{text: text, style: style{}},
		},
	}
}

// RenderANSI renders the styled text to ANSI escape codes for telnet clients.
func (st StyledText) RenderANSI() string {
	var buf strings.Builder

	for _, seg := range st.segments {
		st.renderSegmentANSI(&buf, seg)
	}

	return buf.String()
}

// renderSegmentANSI renders a single segment to ANSI codes.
func (st StyledText) renderSegmentANSI(buf *strings.Builder, seg segment) {
	hasStyle := seg.style.bold || seg.style.italic || seg.style.dim ||
		seg.style.underline || seg.style.color != ""

	if !hasStyle {
		buf.WriteString(seg.text)
		return
	}

	// Write style codes
	if seg.style.bold {
		buf.WriteString(ansiBold)
	}
	if seg.style.dim {
		buf.WriteString(ansiDim)
	}
	if seg.style.italic {
		buf.WriteString(ansiItalic)
	}
	if seg.style.underline {
		buf.WriteString(ansiUnderline)
	}
	if seg.style.color != "" {
		buf.WriteString(seg.style.color)
	}

	buf.WriteString(seg.text)
	buf.WriteString(ansiReset)
}

// Append combines two StyledText values.
func (st StyledText) Append(other StyledText) StyledText {
	return StyledText{
		segments: append(st.segments, other.segments...),
	}
}

// AppendText appends plain text to the styled text.
func (st StyledText) AppendText(text string) StyledText {
	return StyledText{
		segments: append(st.segments, segment{text: text, style: style{}}),
	}
}

// padRight pads a string with spaces to the given width.
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
