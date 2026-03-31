// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package content

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samber/oops"
)

const metaSuffix = ".meta.json"

// fileMeta is the sidecar JSON structure persisted alongside each body file.
type fileMeta struct {
	ContentType string            `json:"content_type"`
	Metadata    map[string]string `json:"metadata"`
}

// FileStore implements Store using the local filesystem. Keys map to file paths
// by replacing dots with directory separators.
type FileStore struct {
	rootDir string
}

// NewFileStore creates a FileStore rooted at rootDir. The directory is created
// on first use; it does not need to exist at construction time.
func NewFileStore(rootDir string) *FileStore {
	return &FileStore{rootDir: rootDir}
}

// keyToPath converts a dot-separated key to a file path under rootDir.
func (s *FileStore) keyToPath(key string) string {
	rel := strings.ReplaceAll(key, ".", string(filepath.Separator))
	return filepath.Join(s.rootDir, rel)
}

// pathToKey converts a file path back to a dot-separated key.
func (s *FileStore) pathToKey(path string) string {
	rel, err := filepath.Rel(s.rootDir, path)
	if err != nil {
		// Fall back to the raw path; callers treat unexpected keys as harmless.
		return path
	}
	return strings.ReplaceAll(rel, string(filepath.Separator), ".")
}

// validateKey rejects keys that could cause path traversal. Keys must be
// non-empty dot-separated identifiers without embedded path separators.
func validateKey(key string) error {
	if key == "" {
		return oops.Errorf("key must not be empty")
	}
	if strings.HasPrefix(key, "/") || strings.HasPrefix(key, "\\") {
		return oops.With("key", key).Errorf("key must not be an absolute path")
	}
	// Reject any embedded path separator — these bypass the dot-to-slash
	// conversion and can be used to escape rootDir.
	if strings.ContainsAny(key, "/\\") {
		return oops.With("key", key).Errorf("key must not contain path separators")
	}
	return nil
}

// safeAbsPath converts a key to an absolute path and verifies it stays under
// rootDir, returning an error for any traversal attempt.
func (s *FileStore) safeAbsPath(key string) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(s.keyToPath(key))
	if err != nil {
		return "", oops.With("key", key).Wrap(err)
	}
	rootAbs, err := filepath.Abs(s.rootDir)
	if err != nil {
		return "", oops.Wrap(err)
	}
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", oops.With("key", key).Errorf("key resolves outside root directory")
	}
	return abs, nil
}

// Put creates or replaces the content item identified by item.Key.
func (s *FileStore) Put(_ context.Context, item *Item) error {
	abs, err := s.safeAbsPath(item.Key)
	if err != nil {
		return err
	}
	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o750); mkErr != nil {
		return oops.With("key", item.Key).With("path", abs).Wrap(mkErr)
	}
	if writeErr := os.WriteFile(abs, item.Body, 0o600); writeErr != nil {
		return oops.With("key", item.Key).With("path", abs).Wrap(writeErr)
	}
	meta := fileMeta{
		ContentType: item.ContentType,
		Metadata:    item.Metadata,
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return oops.With("key", item.Key).Wrap(err)
	}
	sidecar := abs + metaSuffix
	if writeErr := os.WriteFile(sidecar, data, 0o600); writeErr != nil {
		return oops.With("key", item.Key).With("sidecar", sidecar).Wrap(writeErr)
	}
	return nil
}

// Get retrieves the content item for key. Returns nil, nil when the key does
// not exist.
func (s *FileStore) Get(_ context.Context, key string) (*Item, error) {
	abs, err := s.safeAbsPath(key)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, oops.With("key", key).With("path", abs).Wrap(err)
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		return nil, oops.With("key", key).With("path", abs).Wrap(err)
	}
	item := &Item{
		Key:       key,
		Body:      body,
		UpdatedAt: info.ModTime(),
	}
	sidecar := abs + metaSuffix
	raw, err := os.ReadFile(sidecar)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, oops.With("key", key).With("sidecar", sidecar).Wrap(err)
	}
	if err == nil {
		var meta fileMeta
		if jsonErr := json.Unmarshal(raw, &meta); jsonErr != nil {
			return nil, oops.With("key", key).With("sidecar", sidecar).Wrap(jsonErr)
		}
		item.ContentType = meta.ContentType
		item.Metadata = meta.Metadata
	}
	return item, nil
}

// List returns items whose keys start with prefix, sorted by key, with
// optional cursor-based pagination.
func (s *FileStore) List(_ context.Context, prefix string, opts ListOptions) (*ListResult, error) {
	rootAbs, err := filepath.Abs(s.rootDir)
	if err != nil {
		return nil, oops.Wrap(err)
	}

	var keys []string
	walkErr := filepath.Walk(rootAbs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// Skip sidecar files.
		if strings.HasSuffix(path, metaSuffix) {
			return nil
		}
		key := s.pathToKey(path)
		if prefix == "" || strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, os.ErrNotExist) {
		return nil, oops.With("rootDir", s.rootDir).Wrap(walkErr)
	}

	sort.Strings(keys)

	// Apply cursor: skip everything up to and including the cursor key.
	if opts.Cursor != "" {
		start := 0
		for start < len(keys) && keys[start] <= opts.Cursor {
			start++
		}
		keys = keys[start:]
	}

	var nextCursor string
	if opts.Limit > 0 && len(keys) > opts.Limit {
		nextCursor = keys[opts.Limit-1]
		keys = keys[:opts.Limit]
	}

	items := make([]*Item, 0, len(keys))
	for _, key := range keys {
		abs := s.keyToPath(key)
		absKey, absErr := filepath.Abs(abs)
		if absErr != nil {
			continue
		}
		info, statErr := os.Stat(absKey)
		if statErr != nil {
			continue
		}
		item := &Item{
			Key:       key,
			UpdatedAt: info.ModTime(),
		}
		sidecar := absKey + metaSuffix
		raw, readErr := os.ReadFile(sidecar)
		if readErr == nil {
			var meta fileMeta
			if jsonErr := json.Unmarshal(raw, &meta); jsonErr == nil {
				item.ContentType = meta.ContentType
				item.Metadata = meta.Metadata
			}
		}
		items = append(items, item)
	}

	return &ListResult{Items: items, NextCursor: nextCursor}, nil
}

// Delete removes the body file and its sidecar. Missing files are not an error.
func (s *FileStore) Delete(_ context.Context, key string) error {
	abs, err := s.safeAbsPath(key)
	if err != nil {
		return err
	}
	for _, path := range []string{abs, abs + metaSuffix} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return oops.With("key", key).With("path", path).Wrap(err)
		}
	}
	return nil
}
