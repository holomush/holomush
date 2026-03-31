// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package content

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samber/oops"
	"gopkg.in/yaml.v3"
)

const (
	fmDelimiter         = "---\n"
	defaultContentType  = "text/markdown"
	frontmatterKeyField = "key"
	frontmatterCTField  = "content_type"
)

// ParseContentFile reads a markdown file with optional YAML frontmatter and
// returns a content Item.
//
// Format:
//
//	---
//	key: landing.hero
//	content_type: text/markdown
//	title: "The Crossroads"
//	---
//	Body content here...
//
// If no frontmatter is present, the entire file is treated as a text/markdown
// body. The key defaults to the filename (without extension) if not in
// frontmatter. content_type defaults to "text/markdown" if not in frontmatter.
// All other frontmatter fields become Metadata entries.
func ParseContentFile(path string) (*Item, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, oops.With("path", path).Wrap(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, oops.With("path", path).Wrap(err)
	}

	content := string(data)
	base := filepath.Base(path)
	filenameKey := strings.TrimSuffix(base, filepath.Ext(base))

	if !strings.HasPrefix(content, fmDelimiter) {
		return &Item{
			Key:         filenameKey,
			ContentType: defaultContentType,
			Body:        data,
			Metadata:    map[string]string{},
			UpdatedAt:   info.ModTime(),
		}, nil
	}

	// Find closing delimiter after the opening one.
	rest := content[len(fmDelimiter):]
	closeIdx := strings.Index(rest, fmDelimiter)
	if closeIdx == -1 {
		return nil, oops.With("path", path).Errorf("frontmatter: missing closing ---")
	}

	rawYAML := rest[:closeIdx]
	bodyRaw := rest[closeIdx+len(fmDelimiter):]

	// Parse YAML into a generic map to preserve all fields.
	var fm map[string]string
	if err := yaml.Unmarshal([]byte(rawYAML), &fm); err != nil {
		return nil, oops.With("path", path).Errorf("frontmatter: invalid YAML: %w", err)
	}
	if fm == nil {
		fm = map[string]string{}
	}

	key, ok := fm[frontmatterKeyField]
	if !ok || key == "" {
		return nil, oops.With("path", path).Errorf("frontmatter: missing required field %q", frontmatterKeyField)
	}

	contentType := defaultContentType
	if ct, ok := fm[frontmatterCTField]; ok && ct != "" {
		contentType = ct
	}

	metadata := make(map[string]string, len(fm))
	for k, v := range fm {
		if k == frontmatterKeyField || k == frontmatterCTField {
			continue
		}
		metadata[k] = v
	}

	// Strip a single leading newline from the body.
	body := strings.TrimPrefix(bodyRaw, "\n")

	return &Item{
		Key:         key,
		ContentType: contentType,
		Body:        []byte(body),
		Metadata:    metadata,
		UpdatedAt:   info.ModTime(),
	}, nil
}

// ParseContentDir walks a directory recursively, parsing all .md files.
// Returns items sorted by key. Non-.md files are skipped without error.
func ParseContentDir(dir string) ([]*Item, error) {
	var items []*Item

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return oops.With("path", path).Wrap(err)
		}
		if info.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		item, parseErr := ParseContentFile(path)
		if parseErr != nil {
			return parseErr
		}
		items = append(items, item)
		return nil
	})
	if err != nil {
		return nil, oops.With("dir", dir).Wrap(err)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Key < items[j].Key
	})

	// Guarantee a non-nil slice.
	if items == nil {
		items = []*Item{}
	}

	return items, nil
}
