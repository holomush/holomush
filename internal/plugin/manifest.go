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
	"github.com/holomush/holomush/internal/core"
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
	Name         string            `yaml:"name" json:"name" jsonschema:"required,minLength=1,maxLength=64,pattern=^[a-z](-?[a-z0-9])*$"`
	Version      string            `yaml:"version" json:"version" jsonschema:"required,minLength=1"`
	Type         Type              `yaml:"type" json:"type" jsonschema:"required,enum=lua,enum=binary,enum=setting"`
	Engine       string            `yaml:"engine,omitempty" json:"engine,omitempty" jsonschema:"description=HoloMUSH version constraint (e.g. >= 2.0.0)"`
	Dependencies map[string]string `yaml:"dependencies,omitempty" json:"dependencies,omitempty" jsonschema:"description=Plugin dependencies with version constraints"`
	Events       []string          `yaml:"events,omitempty" json:"events,omitempty"`
	Emits        []string          `yaml:"emits,omitempty" json:"emits,omitempty"`
	// ActorKindsClaimable declares which Actor.Kind values the plugin may
	// vouch for on emitted events. Default if absent: ["plugin"]. Allowed
	// values: "plugin" (always required), "character". The "system" kind
	// is rejected at load — plugins may never claim the host's system
	// identity. See spec docs/superpowers/specs/2026-04-25-plugin-actor-claim-authentication-design.md §3.2.
	ActorKindsClaimable []string         `yaml:"actor_kinds_claimable,omitempty" json:"actor_kinds_claimable,omitempty"`
	Policies            []ManifestPolicy `yaml:"policies,omitempty" json:"policies,omitempty"`
	Commands            []CommandSpec    `yaml:"commands,omitempty" json:"commands,omitempty" jsonschema:"description=Commands provided by this plugin"`
	Verbs               []VerbSpec       `yaml:"verbs,omitempty" json:"verbs,omitempty" jsonschema:"description=Verb registrations contributed by this plugin"`
	Priority            *LoadPriority    `yaml:"priority,omitempty" json:"priority,omitempty" jsonschema:"description=Load priority (lower loads first)"`
	SessionStreams      bool             `yaml:"session_streams,omitempty" json:"session_streams,omitempty" jsonschema:"description=Plugin contributes streams to session subscriptions via QuerySessionStreams"`
	LuaPlugin           *LuaConfig       `yaml:"lua-plugin,omitempty" json:"lua-plugin,omitempty"`
	BinaryPlugin        *BinaryConfig    `yaml:"binary-plugin,omitempty" json:"binary-plugin,omitempty"`
	Setting             *SettingConfig   `yaml:"setting,omitempty" json:"setting,omitempty"`

	// Deprecated: capabilities field is no longer supported. Use policies instead.
	// This field exists only to detect old-format manifests and produce a clear error.
	Capabilities []string `yaml:"capabilities,omitempty" json:"-" jsonschema:"-"`

	// Service contract declarations
	Requires []Dependency `yaml:"requires,omitempty" json:"requires,omitempty"`
	Provides []string     `yaml:"provides,omitempty" json:"provides,omitempty"`
	Storage  StorageType  `yaml:"storage,omitempty" json:"storage,omitempty"`

	// Crypto declares the plugin's event-type sensitivity contracts and
	// (forward-looking) decryption opt-in subscriptions. Phase 1 of the
	// event-payload-crypto design records these declarations; Phase 3
	// adds runtime enforcement.
	Crypto *CryptoSection `yaml:"crypto,omitempty" json:"crypto,omitempty"`

	// Config is the plugin's runtime config schema, keyed by config key.
	// Opaque to host semantics (host validates generic types + merges values;
	// plugin owns meaning). Empty for plugins with no runtime config.
	Config map[string]ConfigParam `yaml:"config,omitempty" json:"config,omitempty"`

	// Audit declares NATS subject patterns the plugin owns for JetStream
	// audit purposes. When set, the host routes AuditEvent deliveries
	// matching these patterns to the plugin's PluginAuditService RPC
	// instead of the host events_audit projection, and QueryHistory calls
	// on those subjects are forwarded to the plugin's QueryHistory RPC.
	Audit []AuditBlock `yaml:"audit,omitempty" json:"audit,omitempty"`

	// HistoryScope declares which history visibility bucket the plugin's
	// emitted events fall into. Required when emits is non-empty (INV-PRIVACY-7).
	// Valid values: "grid" (visible to all observers in the grid),
	// "scene" (visible to scene participants only),
	// "custom" (plugin owns visibility via QueryHistory RPC).
	// See spec docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md §INV-PRIVACY-7.
	HistoryScope string `yaml:"history_scope,omitempty" json:"history_scope,omitempty"`

	// ABAC trust boundary fields
	ResourceTypes []string     `yaml:"resource_types,omitempty" json:"resource_types,omitempty"`
	Actions       []string     `yaml:"actions,omitempty" json:"actions,omitempty"`
	Trust         *TrustConfig `yaml:"trust,omitempty" json:"trust,omitempty"`

	emitsDeclared               bool `yaml:"-" json:"-" jsonschema:"-"`
	actorKindsClaimableDeclared bool `yaml:"-" json:"-" jsonschema:"-"`
}

// EffectivePriority returns the manifest's load priority, applying
// LoadPriorityDefault (100) when the priority is not explicitly set.
func (m *Manifest) EffectivePriority() LoadPriority {
	if m.Priority != nil {
		return *m.Priority
	}
	return LoadPriorityDefault
}

// RequiredServiceNames returns the names of the service-kind dependencies,
// preserving the legacy flat-string semantics for broker / InjectRequired
// consumers.
func (m *Manifest) RequiredServiceNames() []string {
	out := make([]string, 0, len(m.Requires))
	for _, d := range m.Requires {
		if d.Kind == DependencyService {
			out = append(out, d.Name)
		}
	}
	return out
}

// RequiredCapabilities returns the names of the capability-kind dependencies.
func (m *Manifest) RequiredCapabilities() []string {
	out := make([]string, 0, len(m.Requires))
	for _, d := range m.Requires {
		if d.Kind == DependencyCapability {
			out = append(out, d.Name)
		}
	}
	return out
}

// RequiresDisplay returns human-readable "<kind>:<name>" strings for admin /
// `plugin info` rendering.
func (m *Manifest) RequiresDisplay() []string {
	out := make([]string, 0, len(m.Requires))
	for _, d := range m.Requires {
		out = append(out, string(d.Kind)+":"+d.Name)
	}
	return out
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

// ConfigParam declares one plugin runtime config key. Opaque to the host:
// type/default/required are validated generically; the host never interprets
// what a key controls. See
// docs/superpowers/specs/2026-05-26-plugin-runtime-config-design.md.
type ConfigParam struct {
	Type        string `yaml:"type" json:"type" jsonschema:"enum=duration,enum=int,enum=bool,enum=string"` // duration|int|bool|string
	Default     string `yaml:"default,omitempty" json:"default,omitempty"`
	Required    bool   `yaml:"required,omitempty" json:"required,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// AuditBlock declares a set of NATS subject patterns the plugin owns for
// JetStream audit routing. Subjects matching any Subjects pattern are
// excluded from the host events_audit projection and routed to the
// plugin's PluginAuditService.AuditEvent RPC. QueryHistory for a matching
// subject is forwarded to the plugin's PluginAuditService.QueryHistory.
//
// Schema and Table are advisory (used by operators to introspect where
// the plugin stores its audit rows); the host does not validate or touch
// the plugin's storage directly.
type AuditBlock struct {
	// Subjects is the list of NATS subject patterns owned by this
	// plugin. Each pattern follows NATS wildcard rules: `*` matches
	// exactly one token, `>` matches one-or-more terminal tokens.
	// Example: "events.*.scene.>".
	Subjects []string `yaml:"subjects" json:"subjects" jsonschema:"required"`

	// Schema is the PostgreSQL schema name where the plugin stores its
	// audit rows. Advisory — used by operators to introspect audit
	// storage. Example: "plugin_core_scenes".
	Schema string `yaml:"schema,omitempty" json:"schema,omitempty"`

	// Table is the table name in Schema that holds audit rows. Advisory.
	// Example: "scene_log".
	Table string `yaml:"table,omitempty" json:"table,omitempty"`
}

// Validate checks audit-block constraints.
func (a *AuditBlock) Validate() error {
	if len(a.Subjects) == 0 {
		return oops.In("manifest").New("audit block must declare at least one subject pattern")
	}
	for i, s := range a.Subjects {
		if strings.TrimSpace(s) == "" {
			return oops.In("manifest").With("subject_index", i).
				New("audit subject pattern must not be empty")
		}
	}
	return nil
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

// validHistoryScopes is the closed enum of accepted history_scope values.
// Enforced by INV-PRIVACY-7: any plugin that declares emits MUST declare one.
var validHistoryScopes = map[string]bool{
	"grid":   true,
	"scene":  true,
	"custom": true,
}

// ParseManifest parses and validates a plugin.yaml file.
func ParseManifest(data []byte) (*Manifest, error) {
	if len(data) == 0 {
		return nil, oops.In("manifest").New("manifest data is empty")
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, oops.In("manifest").Hint("invalid YAML").Wrap(err)
	}

	// Pre-Decode kind checks for sequence-typed fields. yaml.v3's `root.Decode`
	// fails with a generic "cannot unmarshal !!str into []string" error when a
	// scalar is supplied for a slice field, masking the manifest namespace. We
	// check sequence-typed fields BEFORE Decode so operators get a manifest-
	// namespaced error pointing at the offending key.
	if node := manifestKeyNode(&root, "actor_kinds_claimable"); node != nil {
		if node.Kind != yaml.SequenceNode {
			// Name may not be available yet (Decode hasn't run); read it
			// directly from the YAML for the error context.
			name := manifestScalarValue(&root, "name")
			return nil, oops.Code("MANIFEST_ACTOR_KINDS_MALFORMED").
				In("manifest").With("name", name).
				Errorf("actor_kinds_claimable must be declared as a YAML sequence")
		}
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
	m.actorKindsClaimableDeclared = manifestKeyNode(&root, "actor_kinds_claimable") != nil

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
	// INV-PRIVACY-7: validate history_scope closed enum and require it when emits is non-empty.
	if m.HistoryScope != "" && !validHistoryScopes[m.HistoryScope] {
		return oops.In("manifest").With("name", m.Name).
			With("history_scope", m.HistoryScope).
			Errorf("history_scope %q is invalid; valid: grid, scene, custom", m.HistoryScope)
	}
	if len(m.Emits) > 0 && m.HistoryScope == "" {
		return oops.In("manifest").With("name", m.Name).
			With("emits", m.Emits).
			New("plugin emits events but manifest does not declare history_scope (holomush-iwzt INV-PRIVACY-7)")
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
		// The wire event type and verbs[].type MUST be plugin-qualified
		// <owning-plugin>:<verb> (exactly one colon, non-empty verb), or
		// RenderingPublisher.Lookup hard-fails EMIT_UNKNOWN_VERB in production.
		// The registered-emit set and crypto.emits[].event_type stay bare
		// (INV-PLUGIN-32); only the wire/verb-registry vocabulary is qualified
		// (INV-PLUGIN-40, holomush-aneim).
		if want := m.Name + ":"; !strings.HasPrefix(v.Type, want) ||
			len(v.Type) <= len(want) || strings.Count(v.Type, ":") != 1 {
			return oops.Code("PLUGIN_WIRE_TYPE_NOT_QUALIFIED").
				In("manifest").With("plugin", m.Name).With("verb", v.Type).
				Errorf("verbs[].type must be %q-prefixed (<plugin>:<verb>, one colon); got %q", want, v.Type)
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

	// Validate audit blocks — binary plugins only (gRPC service requirement).
	if len(m.Audit) > 0 {
		if m.Type != TypeBinary {
			return oops.In("manifest").With("name", m.Name).With("type", m.Type).
				New("audit can only be declared by binary plugins (requires PluginAuditService gRPC server)")
		}
		for i := range m.Audit {
			if err := m.Audit[i].Validate(); err != nil {
				return oops.In("manifest").With("plugin", m.Name).With("audit_index", i).Wrap(err)
			}
		}
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

	// Validate and normalize actor_kinds_claimable per spec §3.2.
	normalizedKinds, err := m.validateActorKindsClaimable()
	if err != nil {
		return err
	}
	m.ActorKindsClaimable = normalizedKinds

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

	return validateConfigSchema(m.Config)
}

// validateActorKindsClaimable applies spec §3.2 validation rules and
// normalizes the field. The `actorKindsClaimableDeclared` flag distinguishes
// "field absent" (defaults silently to [plugin] per spec §3.2 row 2) from
// "field present but empty" (loud error per spec §5.2 test table; preserves
// the §2 G5 no-silent-fallback invariant — operators who type
// `actor_kinds_claimable: []` MUST get a loud failure rather than a silent
// rewrite to [plugin]). Returns the canonical form on success; oops error on
// violation. Errors are stamped with `plugin = m.Name` so multi-plugin load
// failures identify the offending manifest.
func (m *Manifest) validateActorKindsClaimable() ([]string, error) {
	if !m.actorKindsClaimableDeclared {
		return []string{"plugin"}, nil
	}
	if len(m.ActorKindsClaimable) == 0 {
		return nil, oops.Code("MANIFEST_ACTOR_KINDS_MISSING_PLUGIN").
			With("plugin", m.Name).
			Errorf("actor_kinds_claimable MUST contain %q (plugins always need to vouch for their own identity)", "plugin")
	}
	seen := make(map[string]bool, len(m.ActorKindsClaimable))
	out := make([]string, 0, len(m.ActorKindsClaimable))
	for _, k := range m.ActorKindsClaimable {
		switch k {
		case "plugin", "character":
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		case "system":
			return nil, oops.Code("MANIFEST_ACTOR_KIND_SYSTEM_FORBIDDEN").
				With("plugin", m.Name).
				Errorf("actor_kinds_claimable MUST NOT contain %q (host's system identity is not a claimable plugin capability)", k)
		default:
			return nil, oops.Code("MANIFEST_ACTOR_KIND_UNKNOWN").
				With("plugin", m.Name).
				With("kind", k).
				Errorf("actor_kinds_claimable contains unknown kind %q (allowed: plugin, character)", k)
		}
	}
	if !seen["plugin"] {
		return nil, oops.Code("MANIFEST_ACTOR_KINDS_MISSING_PLUGIN").
			With("plugin", m.Name).
			Errorf("actor_kinds_claimable MUST contain %q (plugins always need to vouch for their own identity)", "plugin")
	}
	return out, nil
}

// DeclaresActorKindClaimable returns true if the plugin manifest opts into
// vouching for the given actor kind on emitted events. Used by the manifest
// gate at internal/plugin/event_emitter.go::Emit.
//
// Defers to core.ActorKind.String() for the wire name; manifest validation
// guarantees "system" never appears in ActorKindsClaimable, so that case
// returns false naturally via the loop without a special branch.
func (m *Manifest) DeclaresActorKindClaimable(kind core.ActorKind) bool {
	name := kind.String()
	for _, k := range m.ActorKindsClaimable {
		if k == name {
			return true
		}
	}
	return false
}

// manifestScalarValue returns the scalar value of a top-level mapping key, or
// "" if the key is absent or non-scalar. Used to extract context (e.g., name)
// for pre-Decode error messages.
func manifestScalarValue(root *yaml.Node, key string) string {
	node := manifestKeyNode(root, key)
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return node.Value
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
