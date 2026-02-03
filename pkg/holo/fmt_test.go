// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFmt_Bold(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantANSI string
	}{
		{
			name:     "simple text",
			input:    "important",
			wantANSI: "\x1b[1mimportant\x1b[0m",
		},
		{
			name:     "empty text",
			input:    "",
			wantANSI: "\x1b[1m\x1b[0m",
		},
		{
			name:     "text with spaces",
			input:    "very important message",
			wantANSI: "\x1b[1mvery important message\x1b[0m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Fmt.Bold(tt.input)
			assert.Equal(t, tt.wantANSI, result.RenderANSI())
		})
	}
}

func TestFmt_Italic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantANSI string
	}{
		{
			name:     "simple text",
			input:    "whispered",
			wantANSI: "\x1b[3mwhispered\x1b[0m",
		},
		{
			name:     "empty text",
			input:    "",
			wantANSI: "\x1b[3m\x1b[0m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Fmt.Italic(tt.input)
			assert.Equal(t, tt.wantANSI, result.RenderANSI())
		})
	}
}

func TestFmt_Dim(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantANSI string
	}{
		{
			name:     "simple text",
			input:    "(quietly)",
			wantANSI: "\x1b[2m(quietly)\x1b[0m",
		},
		{
			name:     "empty text",
			input:    "",
			wantANSI: "\x1b[2m\x1b[0m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Fmt.Dim(tt.input)
			assert.Equal(t, tt.wantANSI, result.RenderANSI())
		})
	}
}

func TestFmt_Color(t *testing.T) {
	tests := []struct {
		name     string
		color    string
		text     string
		wantANSI string
	}{
		{
			name:     "red",
			color:    "red",
			text:     "danger!",
			wantANSI: "\x1b[31mdanger!\x1b[0m",
		},
		{
			name:     "green",
			color:    "green",
			text:     "success",
			wantANSI: "\x1b[32msuccess\x1b[0m",
		},
		{
			name:     "blue",
			color:    "blue",
			text:     "info",
			wantANSI: "\x1b[34minfo\x1b[0m",
		},
		{
			name:     "cyan",
			color:    "cyan",
			text:     "hint",
			wantANSI: "\x1b[36mhint\x1b[0m",
		},
		{
			name:     "magenta",
			color:    "magenta",
			text:     "special",
			wantANSI: "\x1b[35mspecial\x1b[0m",
		},
		{
			name:     "yellow",
			color:    "yellow",
			text:     "warning",
			wantANSI: "\x1b[33mwarning\x1b[0m",
		},
		{
			name:     "white",
			color:    "white",
			text:     "normal",
			wantANSI: "\x1b[37mnormal\x1b[0m",
		},
		{
			name:     "black",
			color:    "black",
			text:     "dark",
			wantANSI: "\x1b[30mdark\x1b[0m",
		},
		{
			name:     "unknown color defaults to no color",
			color:    "purple",
			text:     "text",
			wantANSI: "text",
		},
		{
			name:     "empty color defaults to no color",
			color:    "",
			text:     "text",
			wantANSI: "text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Fmt.Color(tt.color, tt.text)
			assert.Equal(t, tt.wantANSI, result.RenderANSI())
		})
	}
}

func TestFmt_Underline(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantANSI string
	}{
		{
			name:     "simple text",
			input:    "underlined",
			wantANSI: "\x1b[4munderlined\x1b[0m",
		},
		{
			name:     "empty text",
			input:    "",
			wantANSI: "\x1b[4m\x1b[0m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Fmt.Underline(tt.input)
			assert.Equal(t, tt.wantANSI, result.RenderANSI())
		})
	}
}

func TestFmt_List(t *testing.T) {
	tests := []struct {
		name     string
		items    []string
		wantANSI string
	}{
		{
			name:     "multiple items",
			items:    []string{"sword", "shield", "potion"},
			wantANSI: "  - sword\n  - shield\n  - potion",
		},
		{
			name:     "single item",
			items:    []string{"item"},
			wantANSI: "  - item",
		},
		{
			name:     "empty list",
			items:    []string{},
			wantANSI: "",
		},
		{
			name:     "nil list",
			items:    nil,
			wantANSI: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Fmt.List(tt.items)
			assert.Equal(t, tt.wantANSI, result.RenderANSI())
		})
	}
}

func TestFmt_Pairs(t *testing.T) {
	tests := []struct {
		name        string
		pairs       map[string]any
		wantContain []string
	}{
		{
			name: "multiple pairs",
			pairs: map[string]any{
				"HP":    100,
				"MP":    50,
				"Level": 5,
			},
			wantContain: []string{"HP: 100", "MP: 50", "Level: 5"},
		},
		{
			name:        "empty pairs",
			pairs:       map[string]any{},
			wantContain: []string{},
		},
		{
			name:        "nil pairs",
			pairs:       nil,
			wantContain: []string{},
		},
		{
			name: "string values",
			pairs: map[string]any{
				"Name":   "Alice",
				"Status": "Online",
			},
			wantContain: []string{"Name: Alice", "Status: Online"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Fmt.Pairs(tt.pairs)
			rendered := result.RenderANSI()
			for _, want := range tt.wantContain {
				assert.Contains(t, rendered, want)
			}
		})
	}
}

func TestFmt_Table(t *testing.T) {
	tests := []struct {
		name        string
		opts        TableOpts
		wantContain []string
	}{
		{
			name: "basic table",
			opts: TableOpts{
				Headers: []string{"Name", "Location", "Idle"},
				Rows: [][]string{
					{"Alice", "Town Square", "2m"},
					{"Bob", "Forest", "5m"},
				},
			},
			wantContain: []string{"Name", "Location", "Idle", "Alice", "Town Square", "2m", "Bob", "Forest", "5m"},
		},
		{
			name: "empty rows",
			opts: TableOpts{
				Headers: []string{"Name", "Location"},
				Rows:    [][]string{},
			},
			wantContain: []string{"Name", "Location"},
		},
		{
			name: "no headers",
			opts: TableOpts{
				Headers: nil,
				Rows: [][]string{
					{"Alice", "Town Square"},
				},
			},
			wantContain: []string{"Alice", "Town Square"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Fmt.Table(tt.opts)
			rendered := result.RenderANSI()
			for _, want := range tt.wantContain {
				assert.Contains(t, rendered, want)
			}
		})
	}
}

func TestFmt_Table_ColumnAlignment(t *testing.T) {
	opts := TableOpts{
		Headers: []string{"Name", "Score"},
		Rows: [][]string{
			{"Alice", "100"},
			{"Bob", "50"},
		},
	}

	result := Fmt.Table(opts)
	rendered := result.RenderANSI()

	// Should have proper padding for alignment
	// The exact format depends on implementation, but columns should be aligned
	require.NotEmpty(t, rendered)

	// Check that output contains expected structure (headers, separator, rows)
	lines := splitLines(rendered)
	require.GreaterOrEqual(t, len(lines), 3, "table should have at least header, separator, and rows")
}

func TestFmt_Separator(t *testing.T) {
	result := Fmt.Separator()
	rendered := result.RenderANSI()

	// Separator should be a visual horizontal line
	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "-")
}

func TestFmt_Header(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		wantANSI string
	}{
		{
			name: "simple header",
			text: "Inventory",
		},
		{
			name: "empty header",
			text: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Fmt.Header(tt.text)
			rendered := result.RenderANSI()

			// Header should contain the text and some decoration
			if tt.text != "" {
				assert.Contains(t, rendered, tt.text)
			}
		})
	}
}

func TestStyledText_Append(t *testing.T) {
	bold := Fmt.Bold("hello")
	italic := Fmt.Italic("world")

	combined := bold.Append(italic)
	rendered := combined.RenderANSI()

	assert.Contains(t, rendered, "hello")
	assert.Contains(t, rendered, "world")
}

func TestStyledText_AppendText(t *testing.T) {
	bold := Fmt.Bold("hello")
	combined := bold.AppendText(" world")
	rendered := combined.RenderANSI()

	assert.Contains(t, rendered, "hello")
	assert.Contains(t, rendered, " world")
}

func TestPlainText(t *testing.T) {
	text := PlainText("hello world")
	assert.Equal(t, "hello world", text.RenderANSI())
}

// Helper function to split rendered output into lines
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var lines []string
	var current string
	for _, c := range s {
		if c == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
