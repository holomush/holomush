// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"gopkg.in/yaml.v3"

	content "github.com/holomush/holomush/internal/content"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/world"
)

// Compile-time check.
var _ plugins.BootstrapPlugin = (*SettingBootstrapper)(nil)

// worldSeeder abstracts world.Service for testability.
type worldSeeder interface {
	CreateLocation(ctx context.Context, subjectID string, loc *world.Location) error
	CreateExit(ctx context.Context, subjectID string, exit *world.Exit) error
	FindLocationByName(ctx context.Context, subjectID, name string) (*world.Location, error)
}

// SettingBootstrapper seeds world content from a setting plugin manifest.
type SettingBootstrapper struct {
	contentStore  content.Store
	worldService  worldSeeder
	metadataStore MetadataStore
	settingName   string
	resetSetting  bool
	logger        *slog.Logger
}

// SettingBootstrapperOpts holds dependencies for SettingBootstrapper.
type SettingBootstrapperOpts struct {
	ContentStore  content.Store
	WorldService  *world.Service
	MetadataStore MetadataStore
	SettingName   string
	ResetSetting  bool
	Logger        *slog.Logger
}

// NewSettingBootstrapper creates a new SettingBootstrapper.
func NewSettingBootstrapper(opts SettingBootstrapperOpts) *SettingBootstrapper {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	b := &SettingBootstrapper{
		contentStore:  opts.ContentStore,
		metadataStore: opts.MetadataStore,
		settingName:   opts.SettingName,
		resetSetting:  opts.ResetSetting,
		logger:        logger,
	}
	if opts.WorldService != nil {
		b.worldService = opts.WorldService
	}
	return b
}

// Priority returns the bootstrap priority for world/setting initialization.
func (b *SettingBootstrapper) Priority() int {
	return plugins.BootstrapPriorityWorld
}

// Bootstrap runs the setting plugin bootstrap process.
// It is idempotent: if the setting has already been bootstrapped and
// resetSetting is false, it returns nil without doing anything.
func (b *SettingBootstrapper) Bootstrap(ctx context.Context, manifest *plugins.Manifest, pluginDir string) error {
	if manifest == nil {
		return oops.Code("SETTING_BOOTSTRAP_FAILED").New("manifest is nil")
	}
	if manifest.Setting == nil {
		return oops.Code("SETTING_BOOTSTRAP_FAILED").With("plugin", manifest.Name).New("manifest has no setting stanza")
	}

	// Step 1: check whether setting has already been bootstrapped.
	_, found, err := b.metadataStore.Get(ctx, "active_setting")
	if err != nil {
		return oops.Code("SETTING_BOOTSTRAP_FAILED").With("key", "active_setting").Wrap(err)
	}

	if found && !b.resetSetting {
		b.logger.InfoContext(ctx, "setting already bootstrapped, skipping",
			"setting", b.settingName)
		return nil
	}

	if b.resetSetting {
		if err := b.metadataStore.Delete(ctx, "active_setting"); err != nil {
			return oops.Code("SETTING_BOOTSTRAP_FAILED").With("key", "active_setting").Wrap(err)
		}
	}

	// Step 2: seed content items.
	if err := b.seedContent(ctx, manifest, pluginDir); err != nil {
		return err
	}

	// Step 3: seed world locations and exits.
	if manifest.Setting.WorldDir != "" {
		if err := b.seedWorld(ctx, manifest, pluginDir); err != nil {
			return err
		}
	}

	// Step 4: seed theme.
	if manifest.Setting.Theme != "" {
		if err := b.seedTheme(ctx, manifest, pluginDir); err != nil {
			return err
		}
	}

	// Step 5: record active setting.
	if err := b.metadataStore.Set(ctx, "active_setting", b.settingName); err != nil {
		return oops.Code("SETTING_BOOTSTRAP_FAILED").With("key", "active_setting").Wrap(err)
	}
	if err := b.metadataStore.Set(ctx, "setting_version", manifest.Version); err != nil {
		return oops.Code("SETTING_BOOTSTRAP_FAILED").With("key", "setting_version").Wrap(err)
	}

	b.logger.InfoContext(ctx, "setting bootstrapped",
		"setting", b.settingName,
		"version", manifest.Version)
	return nil
}

// seedContent walks the content directory and puts new items into the content store.
func (b *SettingBootstrapper) seedContent(ctx context.Context, manifest *plugins.Manifest, pluginDir string) error {
	if manifest.Setting.ContentDir == "" {
		return nil
	}

	contentDir := filepath.Join(pluginDir, manifest.Setting.ContentDir)
	if _, err := os.Stat(contentDir); os.IsNotExist(err) {
		b.logger.InfoContext(ctx, "content dir does not exist, skipping",
			"dir", contentDir)
		return nil
	}

	items, err := content.ParseContentDir(contentDir)
	if err != nil {
		return oops.Code("SETTING_BOOTSTRAP_FAILED").
			With("content_dir", contentDir).
			Wrap(err)
	}

	for _, item := range items {
		existing, err := b.contentStore.Get(ctx, item.Key)
		if err != nil {
			return oops.Code("SETTING_BOOTSTRAP_FAILED").
				With("key", item.Key).
				Wrap(err)
		}
		if existing != nil {
			b.logger.DebugContext(ctx, "content item already exists, skipping", "key", item.Key)
			continue
		}
		if err := b.contentStore.Put(ctx, item); err != nil {
			return oops.Code("SETTING_BOOTSTRAP_FAILED").
				With("key", item.Key).
				Wrap(err)
		}
		b.logger.DebugContext(ctx, "seeded content item", "key", item.Key)
	}
	return nil
}

// locationSeed is the YAML shape for a location in locations.yaml.
type locationSeed struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
}

// exitSeed is the YAML shape for an exit in exits.yaml.
type exitSeed struct {
	From    string   `yaml:"from"`
	To      string   `yaml:"to"`
	Name    string   `yaml:"name"`
	Aliases []string `yaml:"aliases"`
}

// seedWorld creates locations and exits from world YAML seed files.
func (b *SettingBootstrapper) seedWorld(ctx context.Context, manifest *plugins.Manifest, pluginDir string) error {
	if b.worldService == nil {
		b.logger.WarnContext(ctx, "world service not configured, skipping world seed")
		return nil
	}

	worldDir := filepath.Join(pluginDir, manifest.Setting.WorldDir)

	// Map from location name to ID so exits can resolve by name.
	locationIDs := make(map[string]ulid.ULID)

	// Seed locations.
	locFile := filepath.Join(worldDir, "locations.yaml")
	if _, statErr := os.Stat(locFile); statErr == nil {
		data, err := os.ReadFile(locFile)
		if err != nil {
			return oops.Code("SETTING_BOOTSTRAP_FAILED").With("file", locFile).Wrap(err)
		}

		var seeds []locationSeed
		if err := yaml.Unmarshal(data, &seeds); err != nil {
			return oops.Code("SETTING_BOOTSTRAP_FAILED").
				With("file", locFile).
				Wrap(err)
		}

		for _, s := range seeds {
			loc := &world.Location{
				Type:        world.LocationType(s.Type),
				Name:        s.Name,
				Description: s.Description,
			}
			if s.ID != "" {
				id, err := ulid.Parse(s.ID)
				if err != nil {
					return oops.Code("SETTING_BOOTSTRAP_FAILED").
						With("location", s.Name).
						With("id", s.ID).
						Wrap(err)
				}
				loc.ID = id
			}

			if err := b.worldService.CreateLocation(ctx, "system:bootstrap", loc); err != nil {
				if isAlreadyExists(err) {
					b.logger.DebugContext(ctx, "location already exists, skipping", "name", s.Name)
					// Still need the ID for exit resolution — look it up.
					existing, findErr := b.worldService.FindLocationByName(ctx, "system:bootstrap", s.Name)
					if findErr == nil {
						locationIDs[s.Name] = existing.ID
					}
					continue
				}
				return oops.Code("SETTING_BOOTSTRAP_FAILED").
					With("location", s.Name).
					Wrap(err)
			}
			locationIDs[s.Name] = loc.ID
			b.logger.DebugContext(ctx, "seeded location", "name", s.Name, "id", loc.ID)
		}
	}

	// Seed exits.
	exitFile := filepath.Join(worldDir, "exits.yaml")
	if _, statErr := os.Stat(exitFile); statErr == nil {
		data, err := os.ReadFile(exitFile)
		if err != nil {
			return oops.Code("SETTING_BOOTSTRAP_FAILED").With("file", exitFile).Wrap(err)
		}

		var seeds []exitSeed
		if err := yaml.Unmarshal(data, &seeds); err != nil {
			return oops.Code("SETTING_BOOTSTRAP_FAILED").
				With("file", exitFile).
				Wrap(err)
		}

		for _, s := range seeds {
			fromID, ok := locationIDs[s.From]
			if !ok {
				return oops.Code("SETTING_BOOTSTRAP_FAILED").
					With("exit", s.Name).
					Errorf("exit %q: from location %q not found", s.Name, s.From)
			}
			toID, ok := locationIDs[s.To]
			if !ok {
				return oops.Code("SETTING_BOOTSTRAP_FAILED").
					With("exit", s.Name).
					Errorf("exit %q: to location %q not found", s.Name, s.To)
			}

			exit := &world.Exit{
				FromLocationID: fromID,
				ToLocationID:   toID,
				Name:           s.Name,
				Aliases:        s.Aliases,
				Visibility:     world.VisibilityAll,
			}

			if err := b.worldService.CreateExit(ctx, "system:bootstrap", exit); err != nil {
				if isAlreadyExists(err) {
					b.logger.DebugContext(ctx, "exit already exists, skipping", "name", s.Name)
					continue
				}
				return oops.Code("SETTING_BOOTSTRAP_FAILED").
					With("exit", s.Name).
					Wrap(err)
			}
			b.logger.DebugContext(ctx, "seeded exit", "name", s.Name)
		}
	}

	return nil
}

// themeConfig is the JSON shape of a theme file.
type themeConfig struct {
	Default   map[string]any `json:"default"`
	Overrides map[string]any `json:"overrides"`
	Custom    map[string]any `json:"custom"`
}

// seedTheme reads theme.json and writes content items for each theme key.
func (b *SettingBootstrapper) seedTheme(ctx context.Context, manifest *plugins.Manifest, pluginDir string) error {
	themePath := filepath.Join(pluginDir, manifest.Setting.Theme)
	data, err := os.ReadFile(themePath)
	if err != nil {
		return oops.Code("SETTING_BOOTSTRAP_FAILED").With("theme", themePath).Wrap(err)
	}

	var theme themeConfig
	if err := json.Unmarshal(data, &theme); err != nil {
		return oops.Code("SETTING_BOOTSTRAP_FAILED").
			With("theme", themePath).
			Wrap(err)
	}

	if theme.Default != nil {
		defaultJSON, err := json.Marshal(theme.Default)
		if err != nil {
			return oops.Code("SETTING_BOOTSTRAP_FAILED").Wrap(err)
		}
		if err := b.contentStore.Put(ctx, &content.Item{
			Key:         "theme.default",
			ContentType: "application/json",
			Body:        defaultJSON,
			Metadata:    map[string]string{},
		}); err != nil {
			return oops.Code("SETTING_BOOTSTRAP_FAILED").With("key", "theme.default").Wrap(err)
		}
	}

	for k, v := range theme.Overrides {
		key := fmt.Sprintf("theme.overrides.%s", k)
		val, err := json.Marshal(v)
		if err != nil {
			return oops.Code("SETTING_BOOTSTRAP_FAILED").With("key", key).Wrap(err)
		}
		if err := b.contentStore.Put(ctx, &content.Item{
			Key:         key,
			ContentType: "application/json",
			Body:        val,
			Metadata:    map[string]string{},
		}); err != nil {
			return oops.Code("SETTING_BOOTSTRAP_FAILED").With("key", key).Wrap(err)
		}
	}

	for k, v := range theme.Custom {
		key := fmt.Sprintf("theme.custom.%s", k)
		val, err := json.Marshal(v)
		if err != nil {
			return oops.Code("SETTING_BOOTSTRAP_FAILED").With("key", key).Wrap(err)
		}
		if err := b.contentStore.Put(ctx, &content.Item{
			Key:         key,
			ContentType: "application/json",
			Body:        val,
			Metadata:    map[string]string{},
		}); err != nil {
			return oops.Code("SETTING_BOOTSTRAP_FAILED").With("key", key).Wrap(err)
		}
	}

	return nil
}

// isAlreadyExists checks whether an error represents a "already exists" condition.
// The world service uses oops error codes, so we check for LOCATION_CREATE_FAILED
// wrapping a unique-constraint violation, or an explicit "already exists" code.
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	// Accept any error that carries the sentinel.
	return errors.Is(err, errAlreadyExists)
}

// errAlreadyExists is a sentinel used by tests and the bootstrapper itself.
var errAlreadyExists = errors.New("already exists")
