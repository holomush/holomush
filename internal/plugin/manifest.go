// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package plugins provides plugin management and lifecycle control.
//
//go:generate go run github.com/holomush/holomush/internal/plugin/gen-schema
package plugins

import (
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/samber/oops"
	"gopkg.in/yaml.v3"

	"github.com/holomush/holomush/internal/command"
)

// Type identifies the plugin runtime.
type Type string

// Plugin types supported by the system.
const (
	TypeLua     Type = "lua"
	TypeBinary  Type = "binary"
	TypeSetting Type = "setting"
)

// StorageType declares the persistence tier a plugin requires.
type StorageType string

// Storage type constants declare the persistence tier a plugin requires.
const (
	StorageKV       StorageType = "kv"       // KV store only (default)
	StoragePostgres StorageType = "postgres" // schema-isolated PostgreSQL
)

// TrustConfig declares trust escalation for the plugin.
// When AllPrincipals is true AND the server config allowlists this plugin,
// the plugin can install policies targeting any principal/resource type.
type TrustConfig struct {
	AllPrincipals bool `yaml:"all_principals" json:"all_principals"`
}

// ProtectedResourceTypes are core resource types that plugins MUST NOT
// declare in resource_types. Plugins cannot install character-level
// policies targeting these types without trust escalation.
var ProtectedResourceTypes = map[string]bool{
	"character": true, "location": true, "exit": true, "object": true,
	"stream": true, "property": true, "command": true,
	"system": true, "server": true, "player": true,
}

// LoadPriority determines plugin load order. Lower values load first.
type LoadPriority int

// Standard load priorities.
const (
	LoadPriorityDefault LoadPriority = 100 // default for user plugins
)

// ManifestPolicy defines an ABAC policy contributed by a plugin.
type ManifestPolicy struct {
	Name string `yaml:"name" json:"name"`
	DSL  string `yaml:"dsl" json:"dsl"`
}

// Manifest represents a plugin.yaml file.
type Manifest struct {
	Name           string            `yaml:"name" json:"name" jsonschema:"required,minLength=1,maxLength=64,pattern=^[a-z](-?[a-z0-9])*$"`
	Version        string            `yaml:"version" json:"version" jsonschema:"required,minLength=1"`
	Type           Type              `yaml:"type" json:"type" jsonschema:"required,enum=lua,enum=binary,enum=setting"`
	Engine         string            `yaml:"engine,omitempty" json:"engine,omitempty" jsonschema:"description=HoloMUSH version constraint (e.g. >= 2.0.0)"`
	Dependencies   map[string]string `yaml:"dependencies,omitempty" json:"dependencies,omitempty" jsonschema:"description=Plugin dependencies with version constraints"`
	Events         []string          `yaml:"events,omitempty" json:"events,omitempty"`
	Emits          []string          `yaml:"emits,omitempty" json:"emits,omitempty"`
	Policies       []ManifestPolicy  `yaml:"policies,omitempty" json:"policies,omitempty"`
	Commands       []CommandSpec     `yaml:"commands,omitempty" json:"commands,omitempty" jsonschema:"description=Commands provided by this plugin"`
	Verbs          []VerbSpec        `yaml:"verbs,omitempty" json:"verbs,omitempty" jsonschema:"description=Verb registrations contributed by this plugin"`
	Priority       *LoadPriority     `yaml:"priority,omitempty" json:"priority,omitempty" jsonschema:"description=Load priority (lower loads first)"`
	SessionStreams bool              `yaml:"session_streams,omitempty" json:"session_streams,omitempty" jsonschema:"description=Plugin contributes streams to session subscriptions via QuerySessionStreams"`
	LuaPlugin      *LuaConfig        `yaml:"lua-plugin,omitempty" json:"lua-plugin,omitempty"`
	BinaryPlugin   *BinaryConfig     `yaml:"binary-plugin,omitempty" json:"binary-plugin,omitempty"`
	Setting        *SettingConfig    `yaml:"setting,omitempty" json:"setting,omitempty"`

	// Deprecated: capabilities field is no longer supported. Use policies instead.
	// This field exists only to detect old-format manifests and produce a clear error.
	Capabilities []string `yaml:"capabilities,omitempty" json:"-" jsonschema:"-"`

	// Service contract declarations
	Requires []string    `yaml:"requires,omitempty" json:"requires,omitempty"`
	Provides []string    `yaml:"provides,omitempty" json:"provides,omitempty"`
	Storage  StorageType `yaml:"storage,omitempty" json:"storage,omitempty"`

	// ABAC trust boundary fields
	ResourceTypes []string     `yaml:"resource_types,omitempty" json:"resource_types,omitempty"`
	Actions       []string     `yaml:"actions,omitempty" json:"actions,omitempty"`
	Trust         *TrustConfig `yaml:"trust,omitempty" json:"trust,omitempty"`

	emitsDeclared bool `yaml:"-" json:"-" jsonschema:"-"`
}

// EffectivePriority returns the manifest's load priority, applying
// LoadPriorityDefault (100) when the priority is not explicitly set.
func (m *Manifest) EffectivePriority() LoadPriority {
	if m.Priority != nil {
		return *m.Priority
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

// VerbSpec declares a verb registration contributed by a plugin.
type VerbSpec struct {
	Type          string `yaml:"type" json:"type" jsonschema:"required"`
	Category      string `yaml:"category" json:"category" jsonschema:"required"`
	Format        string `yaml:"format" json:"format" jsonschema:"required"`
	Label         string `yaml:"label,omitempty" json:"label,omitempty"`
	DisplayTarget string `yaml:"display_target" json:"display_target" jsonschema:"required"`
}

var validVerbCategories = map[string]bool{
	"communication": true, "movement": true, "state": true, "system": true, "command": true,
}

var validVerbFormats = map[string]bool{
	"speech": true, "action": true, "narrative": true, "notification": true,
	"error": true, "snapshot": true, "delta": true,
}

var validDisplayTargets = map[string]bool{
	"terminal": true, "state": true, "both": true,
}

// CommandSpec declares a command provided by a plugin.
type CommandSpec struct {
	// Name is the canonical command name (e.g., "say", "teleport").
	Name string `yaml:"name" json:"name" jsonschema:"required,minLength=1"`

	// Aliases lists shortcut strings that expand to this command (e.g., `"` for say).
	Aliases []string `yaml:"aliases,omitempty" json:"aliases,omitempty"`

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

	seenAliases := make(map[string]bool)
	for i, alias := range c.Aliases {
		if alias == "" {
			return oops.In("command").With("name", c.Name).With("alias_index", i).New("alias must not be empty")
		}
		if seenAliases[alias] {
			return oops.In("command").With("name", c.Name).With("alias", alias).New("duplicate alias")
		}
		seenAliases[alias] = true
	}

	for i, cap := range c.Capabilities {
		if err := cap.Validate(); err != nil {
			return oops.In("command").With("name", c.Name).With("capability_index", i).Wrap(err)
		}
	}

	return nil
}

func validateEmits(names []string) ([]string, error) {
	seen := make(map[string]bool, len(names))
	validated := make([]string, 0, len(names))
	for i, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			return nil, oops.In("manifest").With("emits_index", i).New("emits entries must not be empty")
		}
		if strings.Contains(name, ":") {
			return nil, oops.In("manifest").With("emits", name).New("emits entries must be bare namespaces without ':'")
		}
		if !namePattern.MatchString(name) {
			return nil, oops.In("manifest").With("emits", name).New("emits entries must match plugin naming pattern")
		}
		if seen[name] {
			return nil, oops.In("manifest").With("emits", name).New("duplicate emits namespace")
		}
		seen[name] = true
		validated = append(validated, name)
	}
	return validated, nil
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

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, oops.In("manifest").Hint("invalid YAML").Wrap(err)
	}

	var m Manifest
	if err := root.Decode(&m); err != nil {
		return nil, oops.In("manifest").Hint("invalid YAML").Wrap(err)
	}
	if emitsNode := manifestKeyNode(&root, "emits"); emitsNode != nil {
		m.emitsDeclared = true
		if emitsNode.Kind != yaml.SequenceNode {
			return nil, oops.In("manifest").With("name", m.Name).
				New("emits must be declared as a YAML sequence")
		}
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
		if len(m.Verbs) > 0 {
			return oops.In("manifest").With("name", m.Name).New("setting plugins must not declare verbs")
		}
		if m.LuaPlugin != nil {
			return oops.In("manifest").With("name", m.Name).New("setting plugins must not specify lua-plugin")
		}
		if m.BinaryPlugin != nil {
			return oops.In("manifest").With("name", m.Name).New("setting plugins must not specify binary-plugin")
		}
	default:
		return oops.In("manifest").With("name", m.Name).With("type", m.Type).New("type must be 'lua', 'binary', or 'setting'")
	}

	// Validate session_streams: only lua and binary plugins can contribute session streams.
	if m.SessionStreams && m.Type != TypeLua && m.Type != TypeBinary {
		return oops.In("manifest").With("name", m.Name).With("type", m.Type).
			New("session_streams is only valid for lua and binary plugin types")
	}
	if (m.emitsDeclared || len(m.Emits) > 0) && m.Type != TypeLua && m.Type != TypeBinary {
		return oops.In("manifest").With("name", m.Name).With("type", m.Type).
			New("emits is only valid for lua and binary plugin types")
	}
	if len(m.Emits) > 0 {
		validated, err := validateEmits(m.Emits)
		if err != nil {
			return err
		}
		m.Emits = validated
	}
	// Validate load priority: priorities below -999 are reserved (historically for core plugins).
	if m.Priority != nil && int(*m.Priority) < -999 {
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

	// Validate verbs and check for duplicates.
	seenVerbs := make(map[string]bool)
	for i, v := range m.Verbs {
		if v.Type == "" {
			return oops.In("manifest").With("plugin", m.Name).With("verb_index", i).
				New("verb type must not be empty")
		}
		if !validVerbCategories[v.Category] {
			return oops.In("manifest").With("plugin", m.Name).With("verb", v.Type).
				With("category", v.Category).New("unknown verb category")
		}
		if !validVerbFormats[v.Format] {
			return oops.In("manifest").With("plugin", m.Name).With("verb", v.Type).
				With("format", v.Format).New("unknown verb format")
		}
		if !validDisplayTargets[strings.ToLower(v.DisplayTarget)] {
			return oops.In("manifest").With("plugin", m.Name).With("verb", v.Type).
				With("display_target", v.DisplayTarget).New("unknown verb display_target")
		}
		if v.Format == "speech" && v.Label == "" {
			return oops.In("manifest").With("plugin", m.Name).With("verb", v.Type).
				New("label is required when verb format is speech")
		}
		if seenVerbs[v.Type] {
			return oops.In("manifest").With("plugin", m.Name).With("verb", v.Type).
				New("duplicate verb type")
		}
		seenVerbs[v.Type] = true
	}

	// Validate provides — only binary plugins can provide services.
	if len(m.Provides) > 0 && m.Type != TypeBinary {
		return oops.Code("INVALID_PROVIDES").
			With("plugin", m.Name).
			Errorf("only binary plugins can provide services")
	}

	// Validate storage — postgres only for binary plugins.
	if m.Storage == StoragePostgres && m.Type != TypeBinary {
		return oops.Code("INVALID_STORAGE").
			With("plugin", m.Name).
			Errorf("postgres storage is only available for binary plugins")
	}

	// Default storage to KV if not specified.
	if m.Storage == "" {
		m.Storage = StorageKV
	}

	// Validate resource_types: binary-only, valid names, no protected types.
	if len(m.ResourceTypes) > 0 {
		if m.Type != TypeBinary {
			return oops.In("manifest").With("name", m.Name).
				New("resource_types can only be declared by binary plugins")
		}
		seen := make(map[string]bool)
		for _, rt := range m.ResourceTypes {
			if !namePattern.MatchString(rt) {
				return oops.In("manifest").With("name", m.Name).With("resource_type", rt).
					New("resource type name must match plugin naming pattern")
			}
			if ProtectedResourceTypes[rt] {
				return oops.In("manifest").With("name", m.Name).With("resource_type", rt).
					New("resource type is protected and cannot be declared by plugins")
			}
			if seen[rt] {
				return oops.In("manifest").With("name", m.Name).With("resource_type", rt).
					New("duplicate resource type")
			}
			seen[rt] = true
		}
	}

	// Validate actions: no empty strings, no duplicates.
	// All plugin types may declare actions (unlike resource_types, actions
	// have no structural coupling to AttributeResolverService).
	// Note: no format constraint on action names — actions are free-form identifiers
	// and may re-declare core actions (unlike resource_types which checks namePattern
	// and ProtectedResourceTypes).
	if len(m.Actions) > 0 {
		seen := make(map[string]bool, len(m.Actions))
		for _, a := range m.Actions {
			if strings.TrimSpace(a) == "" {
				return oops.In("manifest").With("name", m.Name).
					New("action entry must not be empty")
			}
			if seen[a] {
				return oops.In("manifest").With("name", m.Name).With("action", a).
					New("duplicate action")
			}
			seen[a] = true
		}
	}

	return nil
}

func manifestKeyNode(root *yaml.Node, key string) *yaml.Node {
	if root == nil || len(root.Content) == 0 {
		return nil
	}
	mapping := root.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}
