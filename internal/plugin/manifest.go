// Package plugin provides plugin management and lifecycle control.
package plugin

import (
	"fmt"
	"regexp"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

// Type identifies the plugin runtime.
type Type string

// Plugin types supported by the system.
const (
	TypeLua    Type = "lua"
	TypeBinary Type = "binary"
)

// Manifest represents a plugin.yaml file.
type Manifest struct {
	Name         string        `yaml:"name" json:"name" jsonschema:"required,minLength=1,maxLength=64,pattern=^[a-z](-?[a-z0-9])*$"`
	Version      string        `yaml:"version" json:"version" jsonschema:"required,minLength=1"`
	Type         Type          `yaml:"type" json:"type" jsonschema:"required,enum=lua,enum=binary"`
	Events       []string      `yaml:"events,omitempty" json:"events,omitempty"`
	Capabilities []string      `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	LuaPlugin    *LuaConfig    `yaml:"lua-plugin,omitempty" json:"lua-plugin,omitempty"`
	BinaryPlugin *BinaryConfig `yaml:"binary-plugin,omitempty" json:"binary-plugin,omitempty"`
}

// LuaConfig holds Lua-specific configuration.
type LuaConfig struct {
	Entry string `yaml:"entry" json:"entry" jsonschema:"required,minLength=1"`
}

// BinaryConfig holds binary plugin configuration.
type BinaryConfig struct {
	Executable string `yaml:"executable" json:"executable" jsonschema:"required,minLength=1"`
}

// maxNameLength is the maximum allowed length for plugin names.
const maxNameLength = 64

// namePattern validates plugin names: must start with lowercase letter,
// followed by lowercase letters, digits, or single hyphens (no consecutive hyphens).
// Cannot end with a hyphen. Single character names are allowed.
var namePattern = regexp.MustCompile(`^[a-z](-?[a-z0-9])*$`)

// ParseManifest parses and validates a plugin.yaml file.
func ParseManifest(data []byte) (*Manifest, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("manifest data is empty")
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	if err := m.Validate(); err != nil {
		return nil, err
	}

	return &m, nil
}

// Validate checks manifest constraints.
func (m *Manifest) Validate() error {
	if m.Name == "" || !namePattern.MatchString(m.Name) {
		return fmt.Errorf("name %q must start with a-z, contain only a-z, 0-9, single hyphens, and not end with a hyphen", m.Name)
	}
	if len(m.Name) > maxNameLength {
		return fmt.Errorf("name must be %d characters or less, got %d", maxNameLength, len(m.Name))
	}

	if m.Version == "" {
		return fmt.Errorf("version is required")
	}
	if _, err := semver.StrictNewVersion(m.Version); err != nil {
		return fmt.Errorf("version %q must be valid semver (e.g., 1.0.0): %w", m.Version, err)
	}

	switch m.Type {
	case TypeLua:
		if m.LuaPlugin == nil {
			return fmt.Errorf("lua-plugin is required when type is lua")
		}
		if m.LuaPlugin.Entry == "" {
			return fmt.Errorf("lua-plugin.entry is required")
		}
	case TypeBinary:
		if m.BinaryPlugin == nil {
			return fmt.Errorf("binary-plugin is required when type is binary")
		}
		if m.BinaryPlugin.Executable == "" {
			return fmt.Errorf("binary-plugin.executable is required")
		}
	default:
		return fmt.Errorf("type must be 'lua' or 'binary', got %q", m.Type)
	}

	return nil
}
