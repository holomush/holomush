// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	content "github.com/holomush/holomush/internal/content"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/world"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ---- mock implementations ----

// mockContentStore is an in-memory content.Store for tests.
type mockContentStore struct {
	mu    sync.RWMutex
	items map[string]*content.Item
}

func newMockContentStore() *mockContentStore {
	return &mockContentStore{items: make(map[string]*content.Item)}
}

func (m *mockContentStore) Get(_ context.Context, key string) (*content.Item, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.items[key]
	if !ok {
		return nil, nil
	}
	return item, nil
}

func (m *mockContentStore) List(_ context.Context, prefix string, _ content.ListOptions) (*content.ListResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var items []*content.Item
	for k, v := range m.items {
		if prefix == "" || len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			items = append(items, v)
		}
	}
	return &content.ListResult{Items: items}, nil
}

func (m *mockContentStore) Put(_ context.Context, item *content.Item) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[item.Key] = item
	return nil
}

func (m *mockContentStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, key)
	return nil
}

// mockMetadataStore is an in-memory MetadataStore for tests.
type mockMetadataStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func newMockMetadataStore() *mockMetadataStore {
	return &mockMetadataStore{data: make(map[string]string)}
}

func (m *mockMetadataStore) Get(_ context.Context, key string) (string, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	return v, ok, nil
}

func (m *mockMetadataStore) Set(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *mockMetadataStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

// mockWorldSeeder implements worldSeeder for tests.
type mockWorldSeeder struct {
	mu        sync.RWMutex
	locations map[string]*world.Location // name → Location
	exits     []*world.Exit
	createErr error // if set, returned by CreateLocation / CreateExit
}

func newMockWorldSeeder() *mockWorldSeeder {
	return &mockWorldSeeder{locations: make(map[string]*world.Location)}
}

func (m *mockWorldSeeder) CreateLocation(_ context.Context, _ string, loc *world.Location) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.locations[loc.Name]; exists {
		return errAlreadyExists
	}
	if loc.ID.IsZero() {
		loc.ID = ulid.Make()
	}
	cp := *loc
	m.locations[loc.Name] = &cp
	return nil
}

func (m *mockWorldSeeder) CreateExit(_ context.Context, _ string, exit *world.Exit) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.exits {
		if e.Name == exit.Name && e.FromLocationID == exit.FromLocationID {
			return errAlreadyExists
		}
	}
	cp := *exit
	m.exits = append(m.exits, &cp)
	return nil
}

func (m *mockWorldSeeder) FindLocationByName(_ context.Context, _ string, name string) (*world.Location, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	loc, ok := m.locations[name]
	if !ok {
		return nil, errors.New("not found")
	}
	return loc, nil
}

// ---- helpers ----

// newTestBootstrapper constructs a SettingBootstrapper wired to a mockWorldSeeder.
func newTestBootstrapper(cs content.Store, ws worldSeeder, ms MetadataStore, opts SettingBootstrapperOpts) *SettingBootstrapper {
	b := &SettingBootstrapper{
		contentStore:  cs,
		worldService:  ws,
		metadataStore: ms,
		settingName:   opts.SettingName,
		resetSetting:  opts.ResetSetting,
		logger:        discardLogger(),
	}
	return b
}

// settingManifest builds a minimal valid setting manifest for tests.
// contentDir is always "content" by convention in these tests.
func settingManifest(worldDir, theme string) *plugins.Manifest {
	return &plugins.Manifest{
		Name:    "test-setting",
		Version: "1.0.0",
		Type:    plugins.TypeSetting,
		Setting: &plugins.SettingConfig{
			DisplayName: "Test Setting",
			Description: "For unit tests",
			ContentDir:  "content",
			WorldDir:    worldDir,
			Theme:       theme,
		},
	}
}

// writeMarkdownFile writes a markdown content file with frontmatter.
func writeMarkdownFile(t *testing.T, dir, filename, key, body string) {
	t.Helper()
	content := "---\nkey: " + key + "\n---\n" + body
	require.NoError(t, os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o600))
}

// ---- tests ----

func TestSettingBootstrapper_Priority(t *testing.T) {
	b := NewSettingBootstrapper(SettingBootstrapperOpts{
		ContentStore:  newMockContentStore(),
		MetadataStore: newMockMetadataStore(),
		SettingName:   "test",
	})
	assert.Equal(t, plugins.BootstrapPriorityWorld, b.Priority())
}

func TestSettingBootstrapper_Bootstrap_NilManifest(t *testing.T) {
	b := NewSettingBootstrapper(SettingBootstrapperOpts{
		ContentStore:  newMockContentStore(),
		MetadataStore: newMockMetadataStore(),
		SettingName:   "test",
	})

	err := b.Bootstrap(context.Background(), nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest is nil")
}

func TestSettingBootstrapper_Bootstrap_SeedsContentFromMarkdown(t *testing.T) {
	tmpDir := t.TempDir()
	contentDir := filepath.Join(tmpDir, "content")
	require.NoError(t, os.MkdirAll(contentDir, 0o750))

	writeMarkdownFile(t, contentDir, "hero.md", "landing.hero", "Welcome to the MUSH!")
	writeMarkdownFile(t, contentDir, "intro.md", "landing.intro", "This is an introduction.")

	cs := newMockContentStore()
	ms := newMockMetadataStore()

	b := NewSettingBootstrapper(SettingBootstrapperOpts{
		ContentStore:  cs,
		MetadataStore: ms,
		SettingName:   "my-setting",
	})

	manifest := settingManifest("", "")
	err := b.Bootstrap(context.Background(), manifest, tmpDir)
	require.NoError(t, err)

	item, err := cs.Get(context.Background(), "landing.hero")
	require.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, "Welcome to the MUSH!", string(item.Body))

	item2, err := cs.Get(context.Background(), "landing.intro")
	require.NoError(t, err)
	require.NotNil(t, item2)
}

func TestSettingBootstrapper_Bootstrap_SkipsExistingContent(t *testing.T) {
	tmpDir := t.TempDir()
	contentDir := filepath.Join(tmpDir, "content")
	require.NoError(t, os.MkdirAll(contentDir, 0o750))

	writeMarkdownFile(t, contentDir, "hero.md", "landing.hero", "New body")

	cs := newMockContentStore()
	// Pre-populate so the item appears to already exist.
	require.NoError(t, cs.Put(context.Background(), &content.Item{
		Key:         "landing.hero",
		ContentType: "text/markdown",
		Body:        []byte("Original body"),
		Metadata:    map[string]string{},
	}))

	ms := newMockMetadataStore()
	b := NewSettingBootstrapper(SettingBootstrapperOpts{
		ContentStore:  cs,
		MetadataStore: ms,
		SettingName:   "my-setting",
	})

	manifest := settingManifest("", "")
	err := b.Bootstrap(context.Background(), manifest, tmpDir)
	require.NoError(t, err)

	item, err := cs.Get(context.Background(), "landing.hero")
	require.NoError(t, err)
	// Original body should be preserved (idempotency: we do not overwrite).
	assert.Equal(t, "Original body", string(item.Body))
}

func TestSettingBootstrapper_Bootstrap_RecordsActiveSettingMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	contentDir := filepath.Join(tmpDir, "content")
	require.NoError(t, os.MkdirAll(contentDir, 0o750))

	cs := newMockContentStore()
	ms := newMockMetadataStore()

	b := NewSettingBootstrapper(SettingBootstrapperOpts{
		ContentStore:  cs,
		MetadataStore: ms,
		SettingName:   "my-setting",
	})

	manifest := settingManifest("", "")
	err := b.Bootstrap(context.Background(), manifest, tmpDir)
	require.NoError(t, err)

	v, found, err := ms.Get(context.Background(), "active_setting")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "my-setting", v)

	ver, found, err := ms.Get(context.Background(), "setting_version")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "1.0.0", ver)
}

func TestSettingBootstrapper_Bootstrap_SkipsWhenAlreadyBootstrapped(t *testing.T) {
	tmpDir := t.TempDir()
	contentDir := filepath.Join(tmpDir, "content")
	require.NoError(t, os.MkdirAll(contentDir, 0o750))

	writeMarkdownFile(t, contentDir, "hero.md", "landing.hero", "body")

	cs := newMockContentStore()
	ms := newMockMetadataStore()
	// Simulate a previous bootstrap.
	require.NoError(t, ms.Set(context.Background(), "active_setting", "my-setting"))

	b := NewSettingBootstrapper(SettingBootstrapperOpts{
		ContentStore:  cs,
		MetadataStore: ms,
		SettingName:   "my-setting",
	})

	manifest := settingManifest("", "")
	err := b.Bootstrap(context.Background(), manifest, tmpDir)
	require.NoError(t, err)

	// Content should NOT have been seeded because bootstrap was skipped.
	item, err := cs.Get(context.Background(), "landing.hero")
	require.NoError(t, err)
	assert.Nil(t, item, "content should not be seeded on skip")
}

func TestSettingBootstrapper_Bootstrap_ResetSettingClearsAndReruns(t *testing.T) {
	tmpDir := t.TempDir()
	contentDir := filepath.Join(tmpDir, "content")
	require.NoError(t, os.MkdirAll(contentDir, 0o750))

	writeMarkdownFile(t, contentDir, "hero.md", "landing.hero", "fresh body")

	cs := newMockContentStore()
	ms := newMockMetadataStore()
	// Simulate a previous bootstrap.
	require.NoError(t, ms.Set(context.Background(), "active_setting", "my-setting"))

	b := NewSettingBootstrapper(SettingBootstrapperOpts{
		ContentStore:  cs,
		MetadataStore: ms,
		SettingName:   "my-setting",
		ResetSetting:  true,
	})

	manifest := settingManifest("", "")
	err := b.Bootstrap(context.Background(), manifest, tmpDir)
	require.NoError(t, err)

	// Content should have been seeded because reset was requested.
	item, err := cs.Get(context.Background(), "landing.hero")
	require.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, "fresh body", string(item.Body))

	// active_setting should be re-written.
	v, found, err := ms.Get(context.Background(), "active_setting")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "my-setting", v)
}

func TestSettingBootstrapper_Bootstrap_ThemeJSON(t *testing.T) {
	tmpDir := t.TempDir()
	contentDir := filepath.Join(tmpDir, "content")
	require.NoError(t, os.MkdirAll(contentDir, 0o750))

	themeData := map[string]any{
		"default":   map[string]any{"primary": "#000000"},
		"overrides": map[string]any{"bg": "#ffffff"},
		"custom":    map[string]any{"font": "serif"},
	}
	raw, err := json.Marshal(themeData)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "theme.json"), raw, 0o600))

	cs := newMockContentStore()
	ms := newMockMetadataStore()

	b := NewSettingBootstrapper(SettingBootstrapperOpts{
		ContentStore:  cs,
		MetadataStore: ms,
		SettingName:   "my-setting",
	})

	manifest := settingManifest("", "theme.json")
	err = b.Bootstrap(context.Background(), manifest, tmpDir)
	require.NoError(t, err)

	defaultItem, err := cs.Get(context.Background(), "theme.default")
	require.NoError(t, err)
	require.NotNil(t, defaultItem, "theme.default should be seeded")
	assert.Equal(t, "application/json", defaultItem.ContentType)

	bgItem, err := cs.Get(context.Background(), "theme.overrides.bg")
	require.NoError(t, err)
	require.NotNil(t, bgItem, "theme.overrides.bg should be seeded")

	fontItem, err := cs.Get(context.Background(), "theme.custom.font")
	require.NoError(t, err)
	require.NotNil(t, fontItem, "theme.custom.font should be seeded")
}

func TestSettingBootstrapper_Bootstrap_MissingContentDir(t *testing.T) {
	tmpDir := t.TempDir()
	// content/ subdir intentionally NOT created.

	cs := newMockContentStore()
	ms := newMockMetadataStore()

	b := NewSettingBootstrapper(SettingBootstrapperOpts{
		ContentStore:  cs,
		MetadataStore: ms,
		SettingName:   "my-setting",
	})

	manifest := settingManifest("", "")
	err := b.Bootstrap(context.Background(), manifest, tmpDir)
	require.NoError(t, err, "missing content dir should be handled gracefully")

	// metadata should still be recorded.
	v, found, err := ms.Get(context.Background(), "active_setting")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "my-setting", v)
}

func TestSettingBootstrapper_Bootstrap_ManifestWithNoSettingStanza(t *testing.T) {
	b := NewSettingBootstrapper(SettingBootstrapperOpts{
		ContentStore:  newMockContentStore(),
		MetadataStore: newMockMetadataStore(),
		SettingName:   "test",
	})

	manifest := &plugins.Manifest{
		Name:    "bad-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeSetting,
		Setting: nil,
	}

	err := b.Bootstrap(context.Background(), manifest, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no setting stanza")
}

func TestSettingBootstrapper_Bootstrap_SeedsLocationsAndExits(t *testing.T) {
	tmpDir := t.TempDir()
	contentDir := filepath.Join(tmpDir, "content")
	worldDir := filepath.Join(tmpDir, "world")
	require.NoError(t, os.MkdirAll(contentDir, 0o750))
	require.NoError(t, os.MkdirAll(worldDir, 0o750))

	locYAML := `
- name: "The Nexus"
  type: "persistent"
  description: "A vast circular plaza"
- name: "The Threshold"
  type: "persistent"
  description: "A shimmering archway"
`
	exitYAML := `
- from: "The Nexus"
  to: "The Threshold"
  name: "threshold"
  aliases: ["arch"]
`
	require.NoError(t, os.WriteFile(filepath.Join(worldDir, "locations.yaml"), []byte(locYAML), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(worldDir, "exits.yaml"), []byte(exitYAML), 0o600))

	ws := newMockWorldSeeder()
	cs := newMockContentStore()
	ms := newMockMetadataStore()

	b := newTestBootstrapper(cs, ws, ms, SettingBootstrapperOpts{SettingName: "my-setting"})

	manifest := settingManifest("world", "")
	err := b.Bootstrap(context.Background(), manifest, tmpDir)
	require.NoError(t, err)

	ws.mu.RLock()
	defer ws.mu.RUnlock()
	assert.Contains(t, ws.locations, "The Nexus")
	assert.Contains(t, ws.locations, "The Threshold")
	require.Len(t, ws.exits, 1)
	assert.Equal(t, "threshold", ws.exits[0].Name)
	assert.Equal(t, []string{"arch"}, ws.exits[0].Aliases)
}

func TestSettingBootstrapper_Bootstrap_WorldIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	contentDir := filepath.Join(tmpDir, "content")
	worldDir := filepath.Join(tmpDir, "world")
	require.NoError(t, os.MkdirAll(contentDir, 0o750))
	require.NoError(t, os.MkdirAll(worldDir, 0o750))

	locYAML := `
- name: "The Nexus"
  type: "persistent"
  description: "A plaza"
`
	require.NoError(t, os.WriteFile(filepath.Join(worldDir, "locations.yaml"), []byte(locYAML), 0o600))

	ws := newMockWorldSeeder()
	// Pre-seed so the location "already exists".
	existingID := ulid.Make()
	ws.locations["The Nexus"] = &world.Location{ID: existingID, Name: "The Nexus"}

	cs := newMockContentStore()
	ms := newMockMetadataStore()

	b := newTestBootstrapper(cs, ws, ms, SettingBootstrapperOpts{SettingName: "my-setting"})

	manifest := settingManifest("world", "")
	err := b.Bootstrap(context.Background(), manifest, tmpDir)
	require.NoError(t, err)

	// Location count should still be 1 — not duplicated.
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	assert.Len(t, ws.locations, 1)
}

func TestSettingBootstrapper_Bootstrap_NilWorldServiceSkipsWorldSeed(t *testing.T) {
	tmpDir := t.TempDir()
	contentDir := filepath.Join(tmpDir, "content")
	worldDir := filepath.Join(tmpDir, "world")
	require.NoError(t, os.MkdirAll(contentDir, 0o750))
	require.NoError(t, os.MkdirAll(worldDir, 0o750))

	locYAML := "- name: \"The Nexus\"\n  type: persistent\n  description: test\n"
	require.NoError(t, os.WriteFile(filepath.Join(worldDir, "locations.yaml"), []byte(locYAML), 0o600))

	cs := newMockContentStore()
	ms := newMockMetadataStore()

	// Pass nil worldSeeder explicitly.
	b := newTestBootstrapper(cs, nil, ms, SettingBootstrapperOpts{SettingName: "my-setting"})

	manifest := settingManifest("world", "")
	err := b.Bootstrap(context.Background(), manifest, tmpDir)
	require.NoError(t, err, "nil world service should be a warn-and-skip, not an error")
}

func TestSettingBootstrapper_Bootstrap_ExitReferencesUnknownLocation(t *testing.T) {
	tmpDir := t.TempDir()
	contentDir := filepath.Join(tmpDir, "content")
	worldDir := filepath.Join(tmpDir, "world")
	require.NoError(t, os.MkdirAll(contentDir, 0o750))
	require.NoError(t, os.MkdirAll(worldDir, 0o750))

	// Exit references "Unknown Place" which is not in locations.yaml.
	locYAML := "- name: \"The Nexus\"\n  type: persistent\n  description: test\n"
	exitYAML := "- from: \"The Nexus\"\n  to: \"Unknown Place\"\n  name: \"nowhere\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(worldDir, "locations.yaml"), []byte(locYAML), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(worldDir, "exits.yaml"), []byte(exitYAML), 0o600))

	ws := newMockWorldSeeder()
	cs := newMockContentStore()
	ms := newMockMetadataStore()

	b := newTestBootstrapper(cs, ws, ms, SettingBootstrapperOpts{SettingName: "my-setting"})

	manifest := settingManifest("world", "")
	err := b.Bootstrap(context.Background(), manifest, tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Unknown Place")
}
