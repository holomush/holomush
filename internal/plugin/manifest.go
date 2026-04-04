// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package plugins provides plugin management and lifecycle control.
//
//go:generate go run github.com/holomush/holomush/internal/plugin/gen-schema
package plugins

import (
	"regexp"

	"github.com/Masterminds/semver/v3"
	"github.com/samber/oops"
	"gopkg.in/yaml.v3"

	"github.com/holomush/holomush/internal/command"
)

// Type identifies the plugin runtime.
type Type string

// Plugin types supported by the system.
const (
	TypeCore    Type = "core"
	TypeLua     Type = "lua"
	TypeBinary  Type = "binary"
	TypeSetting Type = "setting"
)

// LoadPriority determines plugin load order. Lower values load first.
type LoadPriority int

// Standard load priorities.
const (
	LoadPriorityCore    LoadPriority = 0   // core plugins load first
	LoadPriorityDefault LoadPriority = 100 // default for user plugins
)

// ManifestPolicy defines an ABAC policy contributed by a plugin.
type ManifestPolicy struct {
	Name string `yaml:"name" json:"name"`
	DSL  string `yaml:"dsl" json:"dsl"`
}

// Manifest represents a plugin.yaml file.
type Manifest struct {
	Name         string            `yaml:"name" json:"name" jsonschema:"required,minLength=1,maxLength=64,pattern=^[a-z](-?[a-z0-9])*$"`
	Version      string            `yaml:"version" json:"version" jsonschema:"required,minLength=1"`
	Type         Type              `yaml:"type" json:"type" jsonschema:"required,enum=core,enum=lua,enum=binary,enum=setting"`
	Engine       string            `yaml:"engine,omitempty" json:"engine,omitempty" jsonschema:"description=HoloMUSH version constraint (e.g. >= 2.0.0)"`
	Dependencies map[string]string `yaml:"dependencies,omitempty" json:"dependencies,omitempty" jsonschema:"description=Plugin dependencies with version constraints"`
	Events       []string          `yaml:"events,omitempty" json:"events,omitempty"`
	Policies     []ManifestPolicy  `yaml:"policies,omitempty" json:"policies,omitempty"`
	Commands     []CommandSpec     `yaml:"commands,omitempty" json:"commands,omitempty" jsonschema:"description=Commands provided by this plugin"`
	Priority     *LoadPriority     `yaml:"priority,omitempty" json:"priority,omitempty" jsonschema:"description=Load priority (lower loads first)"`
	LuaPlugin    *LuaConfig        `yaml:"lua-plugin,omitempty" json:"lua-plugin,omitempty"`
	BinaryPlugin *BinaryConfig     `yaml:"binary-plugin,omitempty" json:"binary-plugin,omitempty"`
	Setting      *SettingConfig    `yaml:"setting,omitempty" json:"setting,omitempty"`

	// Deprecated: capabilities field is no longer supported. Use policies instead.
	// This field exists only to detect old-format manifests and produce a clear error.
	Capabilities []string `yaml:"capabilities,omitempty" json:"-" jsonschema:"-"`
}

// EffectivePriority returns the manifest's load priority, applying a
// type-appropriate default when the priority is not explicitly set.
// Core plugins default to LoadPriorityCore (0); all others default to
// LoadPriorityDefault (100).
func (m *Manifest) EffectivePriority() LoadPriority {
	if m.Priority != nil {
		return *m.Priority
	}
	if m.Type == TypeCore {
		return LoadPriorityCore
	}
	return LoadPriorityDefault
}

// LuaConfig holds Lua-specific configuration.
type LuaConfig struct {
	Entry string `yaml:"entry" json:"entry" jsonschema:"required,minLength=1"`
}

// BinaryConfig holds binary plugin configuration.
type BinaryConfig struct {
	Executable string `yaml:"executable" json:"executable" jsonschema:"required,minLength=1"`
}

// SettingConfig holds setting plugin configuration.
type SettingConfig struct {
	DisplayName      string `yaml:"display_name" json:"display_name" jsonschema:"required"`
	Description      string `yaml:"description" json:"description"`
	ContentDir       string `yaml:"content_dir" json:"content_dir" jsonschema:"required"`
	WorldDir         string `yaml:"world_dir" json:"world_dir"`
	Theme            string `yaml:"theme" json:"theme"`
	StartingLocation string `yaml:"starting_location" json:"starting_location"`
}

// CommandSpec declares a command provided by a plugin.
type CommandSpec struct {
	// Name is the canonical command name (e.g., "say", "teleport").
	Name string `yaml:"name" json:"name" jsonschema:"required,minLength=1"`

	// Capabilities lists all required capabilities for the command (AND logic).
	// The player must have ALL listed capabilities to use this command.
	Capabilities []command.Capability `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`

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
	if err := command.ValidateCommandName(c.Name); err != nil {
		return oops.In("command").Wrap(err)
	}

	if c.HelpText != "" && c.HelpFile != "" {
		return oops.In("command").With("name", c.Name).New("cannot specify both helpText and helpFile")
	}

	for i, cap := range c.Capabilities {
		if err := cap.Validate(); err != nil {
			return oops.In("command").With("name", c.Name).With("capability_index", i).Wrap(err)
		}
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

	if len(m.Capabilities) > 0 {
		return oops.In("manifest").With("name", m.Name).
			New("'capabilities' field is no longer supported; use 'policies' with ABAC policy definitions instead")
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
	case TypeCore:
		// Core plugins have no external entry point — they are wired in-process.
		// Commands are required since core plugins exist to provide command handlers.
		if len(m.Commands) == 0 {
			return oops.In("manifest").With("name", m.Name).New("core plugins must declare at least one command")
		}
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
	case TypeSetting:
		if m.Setting == nil {
			return oops.In("manifest").With("name", m.Name).New("setting stanza is required when type is setting")
		}
		if m.Setting.DisplayName == "" {
			return oops.In("manifest").With("name", m.Name).New("setting.display_name is required")
		}
		if m.Setting.ContentDir == "" {
			return oops.In("manifest").With("name", m.Name).New("setting.content_dir is required")
		}
		if m.Setting.StartingLocation == "" {
			return oops.In("manifest").With("name", m.Name).New("setting.starting_location is required")
		}
		if len(m.Commands) > 0 {
			return oops.In("manifest").With("name", m.Name).New("setting plugins must not declare commands")
		}
		if m.LuaPlugin != nil {
			return oops.In("manifest").With("name", m.Name).New("setting plugins must not specify lua-plugin")
		}
		if m.BinaryPlugin != nil {
			return oops.In("manifest").With("name", m.Name).New("setting plugins must not specify binary-plugin")
		}
	default:
		return oops.In("manifest").With("name", m.Name).With("type", m.Type).New("type must be 'core', 'lua', 'binary', or 'setting'")
	}

	// Validate load priority: priorities below -999 are reserved for core plugins.
	if m.Type != TypeCore && m.Priority != nil && int(*m.Priority) < -999 {
		return oops.In("manifest").With("name", m.Name).
			With("priority", *m.Priority).
			New("load_priority < -999 is reserved for core plugins")
	}

	// Validate policies
	for i, p := range m.Policies {
		if p.Name == "" {
			return oops.Errorf("policy[%d]: name cannot be empty", i)
		}
		if !namePattern.MatchString(p.Name) {
			return oops.Errorf("policy[%d] %q: name must match plugin naming pattern (lowercase, hyphens, no special chars)", i, p.Name)
		}
		if p.DSL == "" {
			return oops.Errorf("policy[%d] %q: dsl cannot be empty", i, p.Name)
		}
	}

	// Validate commands and check for duplicates
	seenCommands := make(map[string]bool)
	for i := range m.Commands {
		if err := m.Commands[i].Validate(); err != nil {
			return oops.In("manifest").With("plugin", m.Name).With("command_index", i).Wrap(err)
		}
		if seenCommands[m.Commands[i].Name] {
			return oops.In("manifest").With("plugin", m.Name).With("command", m.Commands[i].Name).New("duplicate command name")
		}
		seenCommands[m.Commands[i].Name] = true
	}

	return nil
}
