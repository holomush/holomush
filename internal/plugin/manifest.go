// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package plugin provides plugin management and lifecycle control.
package plugin

import (
	"regexp"

	"github.com/Masterminds/semver/v3"
	"github.com/samber/oops"
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
	Name         string            `yaml:"name" json:"name" jsonschema:"required,minLength=1,maxLength=64,pattern=^[a-z](-?[a-z0-9])*$"`
	Version      string            `yaml:"version" json:"version" jsonschema:"required,minLength=1"`
	Type         Type              `yaml:"type" json:"type" jsonschema:"required,enum=lua,enum=binary"`
	Engine       string            `yaml:"engine,omitempty" json:"engine,omitempty" jsonschema:"description=HoloMUSH version constraint (e.g. >= 2.0.0)"`
	Dependencies map[string]string `yaml:"dependencies,omitempty" json:"dependencies,omitempty" jsonschema:"description=Plugin dependencies with version constraints"`
	Events       []string          `yaml:"events,omitempty" json:"events,omitempty"`
	Capabilities []string          `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Commands     []CommandSpec     `yaml:"commands,omitempty" json:"commands,omitempty" jsonschema:"description=Commands provided by this plugin"`
	LuaPlugin    *LuaConfig        `yaml:"lua-plugin,omitempty" json:"lua-plugin,omitempty"`
	BinaryPlugin *BinaryConfig     `yaml:"binary-plugin,omitempty" json:"binary-plugin,omitempty"`
}

// LuaConfig holds Lua-specific configuration.
type LuaConfig struct {
	Entry string `yaml:"entry" json:"entry" jsonschema:"required,minLength=1"`
}

// BinaryConfig holds binary plugin configuration.
type BinaryConfig struct {
	Executable string `yaml:"executable" json:"executable" jsonschema:"required,minLength=1"`
}

// CommandSpec declares a command provided by a plugin.
type CommandSpec struct {
	// Name is the canonical command name (e.g., "say", "teleport").
	Name string `yaml:"name" json:"name" jsonschema:"required,minLength=1"`

	// Capabilities lists all required capabilities for the command (AND logic).
	// The player must have ALL listed capabilities to use this command.
	Capabilities []string `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`

	// Help is a short one-line description of the command.
	Help string `yaml:"help,omitempty" json:"help,omitempty"`

	// Usage shows the command syntax (e.g., "say <message>").
	Usage string `yaml:"usage,omitempty" json:"usage,omitempty"`

	// HelpText provides detailed inline markdown help.
	// Mutually exclusive with HelpFile.
	HelpText string `yaml:"helpText,omitempty" json:"helpText,omitempty"`

	// HelpFile references an external markdown file for detailed help.
	// Path is relative to the plugin directory.
	// Mutually exclusive with HelpText.
	HelpFile string `yaml:"helpFile,omitempty" json:"helpFile,omitempty"`
}

// Validate checks command spec constraints.
func (c *CommandSpec) Validate() error {
	if c.Name == "" {
		return oops.In("command").New("command name is required")
	}

	if c.HelpText != "" && c.HelpFile != "" {
		return oops.In("command").With("name", c.Name).New("cannot specify both helpText and helpFile")
	}

	return nil
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
		return nil, oops.In("manifest").New("manifest data is empty")
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, oops.In("manifest").Hint("invalid YAML").Wrap(err)
	}

	if err := m.Validate(); err != nil {
		return nil, err
	}

	return &m, nil
}

// Validate checks manifest constraints.
func (m *Manifest) Validate() error {
	if m.Name == "" || !namePattern.MatchString(m.Name) {
		return oops.In("manifest").With("name", m.Name).New("name must start with a-z, contain only a-z, 0-9, single hyphens, and not end with a hyphen")
	}
	if len(m.Name) > maxNameLength {
		return oops.In("manifest").With("name", m.Name).With("max_length", maxNameLength).With("actual_length", len(m.Name)).New("name exceeds maximum length")
	}

	if m.Version == "" {
		return oops.In("manifest").With("name", m.Name).New("version is required")
	}
	if _, err := semver.StrictNewVersion(m.Version); err != nil {
		return oops.In("manifest").With("name", m.Name).With("version", m.Version).Hint("version must be valid semver (e.g., 1.0.0)").Wrap(err)
	}

	// Validate engine constraint if specified
	if m.Engine != "" {
		if _, err := semver.NewConstraint(m.Engine); err != nil {
			return oops.In("manifest").With("name", m.Name).With("engine", m.Engine).Hint("engine must be a valid version constraint").Wrap(err)
		}
	}

	// Validate dependency constraints
	for name, constraint := range m.Dependencies {
		if _, err := semver.NewConstraint(constraint); err != nil {
			return oops.In("manifest").With("plugin", m.Name).With("dependency", name).With("constraint", constraint).Hint("invalid dependency constraint").Wrap(err)
		}
	}

	switch m.Type {
	case TypeLua:
		if m.LuaPlugin == nil {
			return oops.In("manifest").With("name", m.Name).New("lua-plugin is required when type is lua")
		}
		if m.LuaPlugin.Entry == "" {
			return oops.In("manifest").With("name", m.Name).New("lua-plugin.entry is required")
		}
	case TypeBinary:
		if m.BinaryPlugin == nil {
			return oops.In("manifest").With("name", m.Name).New("binary-plugin is required when type is binary")
		}
		if m.BinaryPlugin.Executable == "" {
			return oops.In("manifest").With("name", m.Name).New("binary-plugin.executable is required")
		}
	default:
		return oops.In("manifest").With("name", m.Name).With("type", m.Type).New("type must be 'lua' or 'binary'")
	}

	// Validate commands
	for i := range m.Commands {
		if err := m.Commands[i].Validate(); err != nil {
			return oops.In("manifest").With("plugin", m.Name).With("command_index", i).Wrap(err)
		}
	}

	return nil
}
