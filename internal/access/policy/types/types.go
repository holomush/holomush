// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package types defines the core types for the ABAC policy engine.
package types

import "fmt"

// Effect represents the evaluated outcome of an access control decision.
type Effect int

// Effect constants define the possible outcomes of policy evaluation.
const (
	EffectDefaultDeny  Effect = iota // default_deny
	EffectAllow                      // allow
	EffectDeny                       // deny
	EffectSystemBypass               // system_bypass
)

var effectStrings = [...]string{
	"default_deny",
	"allow",
	"deny",
	"system_bypass",
}

func (e Effect) String() string {
	if e >= 0 && int(e) < len(effectStrings) {
		return effectStrings[e]
	}
	return fmt.Sprintf("unknown(%d)", int(e))
}

// PolicyEffect is a string type used in policy declarations to specify
// the intended effect when a policy matches.
type PolicyEffect string

// PolicyEffect constants define the valid policy effect declarations.
const (
	PolicyEffectPermit PolicyEffect = "permit"
	PolicyEffectForbid PolicyEffect = "forbid"
)

// String returns the underlying string value for DB serialization.
func (pe PolicyEffect) String() string {
	return string(pe)
}

// ToEffect converts a PolicyEffect to the runtime Effect type.
// Permit maps to EffectAllow, Forbid maps to EffectDeny,
// and any unknown value maps to EffectDefaultDeny.
func (pe PolicyEffect) ToEffect() Effect {
	switch pe {
	case PolicyEffectPermit:
		return EffectAllow
	case PolicyEffectForbid:
		return EffectDeny
	default:
		return EffectDefaultDeny
	}
}

// AccessRequest represents a subject attempting an action on a resource.
type AccessRequest struct {
	Subject  string // "character:01ABC", "plugin:echo-bot", "system"
	Action   string // "read", "write", "delete", "enter", "execute", "emit"
	Resource string // "location:01XYZ", "command:dig", "property:01DEF"
}

// NewAccessRequest creates a validated AccessRequest. Returns an error if any
// field is empty, preventing silent misuse at access control boundaries.
func NewAccessRequest(subject, action, resource string) (AccessRequest, error) {
	if subject == "" {
		return AccessRequest{}, fmt.Errorf("access request: subject must not be empty")
	}
	if action == "" {
		return AccessRequest{}, fmt.Errorf("access request: action must not be empty")
	}
	if resource == "" {
		return AccessRequest{}, fmt.Errorf("access request: resource must not be empty")
	}
	return AccessRequest{
		Subject:  subject,
		Action:   action,
		Resource: resource,
	}, nil
}

// Decision is the result of evaluating an access request against the policy engine.
// The allowed field is unexported to prevent invariant bypass.
type Decision struct {
	allowed    bool
	Effect     Effect
	Reason     string
	PolicyID   string
	Policies   []PolicyMatch
	Attributes *AttributeBags
}

// NewDecision creates a Decision with the allowed field set consistently
// based on the effect: Allow and SystemBypass grant access, all others deny.
func NewDecision(effect Effect, reason, policyID string) Decision {
	allowed := effect == EffectAllow || effect == EffectSystemBypass
	return Decision{
		allowed:  allowed,
		Effect:   effect,
		Reason:   reason,
		PolicyID: policyID,
	}
}

// IsAllowed returns whether the decision grants access.
func (d Decision) IsAllowed() bool {
	return d.allowed
}

// Validate checks that the Decision invariant holds: the allowed field
// must be consistent with the Effect. Returns an error if the invariant
// is violated. This should be called at engine return boundaries.
func (d Decision) Validate() error {
	expectAllowed := d.Effect == EffectAllow || d.Effect == EffectSystemBypass
	if d.allowed != expectAllowed {
		return fmt.Errorf(
			"decision invariant violated: allowed=%v but effect=%s",
			d.allowed, d.Effect,
		)
	}
	return nil
}

// PolicyMatch records that a specific policy matched an access request
// and what effect it contributed.
type PolicyMatch struct {
	PolicyID      string
	PolicyName    string
	Effect        Effect
	ConditionsMet bool
}

// AttributeBags holds the attribute collections used during policy evaluation.
type AttributeBags struct {
	Subject     map[string]any
	Resource    map[string]any
	Action      map[string]any
	Environment map[string]any
}

// NewAttributeBags creates an AttributeBags with all maps initialized.
func NewAttributeBags() *AttributeBags {
	return &AttributeBags{
		Subject:     make(map[string]any),
		Resource:    make(map[string]any),
		Action:      make(map[string]any),
		Environment: make(map[string]any),
	}
}

// AttrType represents the data type of an attribute value.
type AttrType int

// AttrType constants define the supported attribute data types.
const (
	AttrTypeString     AttrType = iota // string
	AttrTypeInt                        // int
	AttrTypeFloat                      // float
	AttrTypeBool                       // bool
	AttrTypeStringList                 // string_list
)

var attrTypeStrings = [...]string{
	"string",
	"int",
	"float",
	"bool",
	"string_list",
}

func (at AttrType) String() string {
	if at >= 0 && int(at) < len(attrTypeStrings) {
		return attrTypeStrings[at]
	}
	return fmt.Sprintf("unknown(%d)", int(at))
}

// AttributeSchema defines the valid attributes and their types.
// Task 6 will extend this with full namespace/attribute registration.
type AttributeSchema struct {
	namespaces map[string]*NamespaceSchema
}

// NamespaceSchema defines the attributes within a namespace.
type NamespaceSchema struct {
	Attributes map[string]AttrType
}

// NewAttributeSchema creates an empty AttributeSchema.
func NewAttributeSchema() *AttributeSchema {
	return &AttributeSchema{
		namespaces: make(map[string]*NamespaceSchema),
	}
}

// Register adds a namespace schema. Full implementation in Task 12.
func (s *AttributeSchema) Register(namespace string, schema *NamespaceSchema) error {
	s.namespaces[namespace] = schema
	return nil
}

// HasNamespace returns true if the given namespace has been registered.
func (s *AttributeSchema) HasNamespace(namespace string) bool {
	_, ok := s.namespaces[namespace]
	return ok
}

// IsRegistered checks if a namespace+key pair exists. Full implementation in Task 12.
func (s *AttributeSchema) IsRegistered(namespace, key string) bool {
	ns, ok := s.namespaces[namespace]
	if !ok {
		return false
	}
	_, exists := ns.Attributes[key]
	return exists
}

// PolicySource identifies where a policy originated.
type PolicySource string

// PolicySource constants define valid policy origins.
const (
	PolicySourceSeed   PolicySource = "seed"
	PolicySourceLock   PolicySource = "lock"
	PolicySourceAdmin  PolicySource = "admin"
	PolicySourcePlugin PolicySource = "plugin"
)

// PropertyVisibility controls who can see a property.
type PropertyVisibility string

// PropertyVisibility constants define valid visibility levels.
const (
	PropertyVisibilityPublic     PropertyVisibility = "public"
	PropertyVisibilityPrivate    PropertyVisibility = "private"
	PropertyVisibilityRestricted PropertyVisibility = "restricted"
	PropertyVisibilitySystem     PropertyVisibility = "system"
	PropertyVisibilityAdmin      PropertyVisibility = "admin"
)

// EntityType identifies the kind of game entity.
type EntityType string

// EntityType constants define valid game entity kinds.
const (
	EntityTypeCharacter EntityType = "character"
	EntityTypeLocation  EntityType = "location"
	EntityTypeObject    EntityType = "object"
)
