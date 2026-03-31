// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseContentFile(t *testing.T) {
	tests := []struct {
		name         string
		filename     string
		input        string
		wantKey      string
		wantCT       string
		wantBody     string
		wantMetadata map[string]string
		wantErr      string
	}{
		{
			name:     "valid frontmatter with body",
			filename: "hero.md",
			input: "---\nkey: landing.hero\ncontent_type: text/markdown\ntitle: \"The Crossroads\"\n---\nBody content here.\n",
			wantKey:      "landing.hero",
			wantCT:       "text/markdown",
			wantBody:     "Body content here.\n",
			wantMetadata: map[string]string{"title": "The Crossroads"},
		},
		{
			name:     "frontmatter only no body",
			filename: "empty.md",
			input:    "---\nkey: content.empty\ncontent_type: text/plain\n---\n",
			wantKey:  "content.empty",
			wantCT:   "text/plain",
			wantBody: "",
			wantMetadata: map[string]string{},
		},
		{
			name:         "no frontmatter entire content as body",
			filename:     "readme.md",
			input:        "# Hello World\n\nSome prose.\n",
			wantKey:      "readme",
			wantCT:       defaultContentType,
			wantBody:     "# Hello World\n\nSome prose.\n",
			wantMetadata: map[string]string{},
		},
		{
			name:     "malformed YAML frontmatter",
			filename: "bad.md",
			input:    "---\nkey: [unclosed\n---\n",
			wantErr:  "frontmatter: invalid YAML",
		},
		{
			name:     "missing key in frontmatter",
			filename: "nokey.md",
			input:    "---\ncontent_type: text/plain\n---\nbody\n",
			wantErr:  `frontmatter: missing required field "key"`,
		},
		{
			name:         "content_type defaults to text/markdown",
			filename:     "noct.md",
			input:        "---\nkey: some.item\n---\ncontent\n",
			wantKey:      "some.item",
			wantCT:       defaultContentType,
			wantBody:     "content\n",
			wantMetadata: map[string]string{},
		},
		{
			name:     "extra frontmatter fields become metadata",
			filename: "meta.md",
			input: "---\nkey: page.about\ncontent_type: text/markdown\nauthor: Alice\norder: \"3\"\n---\nContent.\n",
			wantKey:      "page.about",
			wantCT:       "text/markdown",
			wantBody:     "Content.\n",
			wantMetadata: map[string]string{"author": "Alice", "order": "3"},
		},
		{
			name:     "missing closing delimiter returns error",
			filename: "unclosed.md",
			input:    "---\nkey: x\n",
			wantErr:  "frontmatter: missing closing ---",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, tt.filename)
			require.NoError(t, os.WriteFile(path, []byte(tt.input), 0o600))

			item, err := ParseContentFile(path)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantKey, item.Key)
			assert.Equal(t, tt.wantCT, item.ContentType)
			assert.Equal(t, tt.wantBody, string(item.Body))
			assert.Equal(t, tt.wantMetadata, item.Metadata)
			assert.False(t, item.UpdatedAt.IsZero())
		})
	}
}

func TestParseContentDir(t *testing.T) {
	t.Run("walks directory recursively and sorts by key", func(t *testing.T) {
		dir := t.TempDir()
		subdir := filepath.Join(dir, "sub")
		require.NoError(t, os.MkdirAll(subdir, 0o750))

		files := map[string]string{
			filepath.Join(dir, "b.md"):        "---\nkey: b.item\n---\nB body\n",
			filepath.Join(dir, "a.md"):        "---\nkey: a.item\n---\nA body\n",
			filepath.Join(subdir, "c.md"):     "---\nkey: c.item\n---\nC body\n",
		}
		for path, content := range files {
			require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		}

		items, err := ParseContentDir(dir)
		require.NoError(t, err)
		require.Len(t, items, 3)

		assert.Equal(t, "a.item", items[0].Key)
		assert.Equal(t, "b.item", items[1].Key)
		assert.Equal(t, "c.item", items[2].Key)
	})

	t.Run("skips non-.md files", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.md"), []byte("---\nkey: r\n---\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"key":"ignored"}`), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("plain text"), 0o600))

		items, err := ParseContentDir(dir)
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, "r", items[0].Key)
	})

	t.Run("returns sorted results from flat directory", func(t *testing.T) {
		dir := t.TempDir()
		keys := []string{"z.item", "m.item", "a.item"}
		for _, k := range keys {
			content := "---\nkey: " + k + "\n---\n"
			fname := strings.ReplaceAll(k, ".", "_") + ".md"
			require.NoError(t, os.WriteFile(filepath.Join(dir, fname), []byte(content), 0o600))
		}

		items, err := ParseContentDir(dir)
		require.NoError(t, err)
		require.Len(t, items, 3)
		assert.Equal(t, "a.item", items[0].Key)
		assert.Equal(t, "m.item", items[1].Key)
		assert.Equal(t, "z.item", items[2].Key)
	})

	t.Run("empty directory returns empty slice", func(t *testing.T) {
		dir := t.TempDir()
		items, err := ParseContentDir(dir)
		require.NoError(t, err)
		assert.Empty(t, items)
	})
}
