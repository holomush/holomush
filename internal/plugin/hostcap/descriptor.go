// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

// OperationClass is the read/write class of a host.v1 method (M2).
type OperationClass int

const (
	// ClassRead marks a host method that only reads state.
	ClassRead OperationClass = iota
	// ClassWrite marks a host method that mutates state.
	ClassWrite
)

// ScopedResourceFn extracts the ABAC resource id a request touches, for the
// scope condition (M3). Returns "" when no resource is in play.
type ScopedResourceFn func(req any) (resourceID string, ok bool)

// MethodDescriptor is the host-owned per-method classification.
type MethodDescriptor struct {
	Action   string           // ABAC action, e.g. "write"
	Resource string           // ABAC resource type, e.g. "location"
	Class    OperationClass   // read | write (M2)
	Scopes   []string         // supported scope tokens (M3); empty => not scope-eligible
	Extract  ScopedResourceFn // required iff len(Scopes) > 0 (M3, INV-PLUGIN-52)
}

// CapabilityDescriptor is the host-owned table for one capability token.
type CapabilityDescriptor struct {
	Token   string
	Methods map[string]MethodDescriptor
}

// Descriptors is the single host-owned source for M1/M2/M3 per-method metadata,
// keyed by capability token. It is the per-method companion to the sub-spec-2
// token->service registry. Scope-eligible rows and the remaining capability
// tokens (focus, emit, audit, stream-history, stream-subscription, session,
// property, world, log) are added in Task 4.
var Descriptors = map[string]CapabilityDescriptor{
	"eval": {Token: "eval", Methods: map[string]MethodDescriptor{
		"Evaluate": {Action: "evaluate", Resource: "policy", Class: ClassRead},
	}},
	"settings": {Token: "settings", Methods: map[string]MethodDescriptor{
		"GetSetting": {Action: "read", Resource: "setting", Class: ClassRead},
		"SetSetting": {Action: "write", Resource: "setting", Class: ClassWrite},
	}},
	"kv": {Token: "kv", Methods: map[string]MethodDescriptor{
		"KVGet":    {Action: "read", Resource: "kv", Class: ClassRead},
		"KVSet":    {Action: "write", Resource: "kv", Class: ClassWrite},
		"KVDelete": {Action: "write", Resource: "kv", Class: ClassWrite},
	}},
	"command-registry": {Token: "command-registry", Methods: map[string]MethodDescriptor{
		"ListCommands":   {Action: "list", Resource: "command", Class: ClassRead},
		"GetCommandHelp": {Action: "read", Resource: "command", Class: ClassRead},
	}},
}
