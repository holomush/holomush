// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	plugins "github.com/holomush/holomush/internal/plugin"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

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

// declarationExemptCapabilities are SELF-GATED capabilities: their access is
// authorized by a dedicated mechanism, not by manifest `requires:` declaration,
// so the capability interceptor MUST NOT declaration-gate them (doing so
// fail-closes legitimate calls — holomush-eykuh.3.17). They still carry
// descriptor entries (classification/Class) so the interceptor recognizes them.
//   - "emit": gated by the emit fence (event_emitter.go::Emit) + `emits:` field
//   - actor_kinds_claimable; plugins declare via `emits:`, not requires:.
//   - "command-registry": gated by the host-vouched dispatch subject (eykuh.3.12).
var declarationExemptCapabilities = map[string]bool{
	"emit":             true,
	"command-registry": true,
}

// IsDeclarationExempt reports whether a capability token is self-gated (its
// access is authorized by a dedicated mechanism, not by manifest declaration +
// the default-deny ABAC decision). Exempt capabilities short-circuit before the
// ABAC gate, so they require no default-permit seed. Exposed for the
// seed-completeness drift guard (INV-PLUGIN-50).
func IsDeclarationExempt(capToken string) bool {
	return declarationExemptCapabilities[capToken]
}

// Descriptors is the single host-owned source for M1/M2/M3 per-method metadata,
// keyed by capability token. It is the per-method companion to the sub-spec-2
// token->service registry, covering every served host.v1 capability token (the
// fail-closed interceptor denies UNCLASSIFIED_CAPABILITY_METHOD for any
// classified method with no entry here). Scope-eligibility and the per-method
// extractors for the read/write rows beyond world.mutation are layered on by
// later tasks (.10/.11).
var Descriptors = map[string]CapabilityDescriptor{
	"eval": {Token: "eval", Methods: map[string]MethodDescriptor{
		"Evaluate": {Action: "evaluate", Resource: "policy", Class: ClassRead},
	}},
	"settings": {Token: "settings", Methods: map[string]MethodDescriptor{
		"GetSetting": {Action: "read", Resource: "setting", Class: ClassRead},
		"SetSetting": {Action: "write", Resource: "setting", Class: ClassWrite},
	}},
	"kv": {Token: "kv", Methods: map[string]MethodDescriptor{
		"Get":    {Action: "read", Resource: "kv", Class: ClassRead},
		"Set":    {Action: "write", Resource: "kv", Class: ClassWrite},
		"Delete": {Action: "write", Resource: "kv", Class: ClassWrite},
	}},
	"command-registry": {Token: "command-registry", Methods: map[string]MethodDescriptor{
		"ListCommands":   {Action: "list", Resource: "command", Class: ClassRead},
		"GetCommandHelp": {Action: "read", Resource: "command", Class: ClassRead},
	}},
	"world.mutation": {Token: "world.mutation", Methods: map[string]MethodDescriptor{
		// CreateLocation has no pre-existing location operand → not scope-eligible.
		"CreateLocation": {Action: "write", Resource: "location", Class: ClassWrite},
		// CreateExit acts on its source location (from_id); own-location restricts
		// a plugin to building exits out of the dispatch location.
		"CreateExit": {
			Action: "write", Resource: "location", Class: ClassWrite,
			Scopes: []string{"own-location"},
			Extract: func(req any) (string, bool) {
				r, ok := req.(*hostv1.CreateExitRequest)
				if !ok {
					return "", false
				}
				return r.GetFromId(), r.GetFromId() != ""
			},
		},
		// CreateObject acts on its location placement; GetLocationId() is "" for
		// character-held / container-nested placements (ok=false), which is correct.
		"CreateObject": {
			Action: "write", Resource: "location", Class: ClassWrite,
			Scopes: []string{"own-location"},
			Extract: func(req any) (string, bool) {
				r, ok := req.(*hostv1.CreateObjectRequest)
				if !ok {
					return "", false
				}
				return r.GetLocationId(), r.GetLocationId() != ""
			},
		},
	}},
	"world.query": {Token: "world.query", Methods: map[string]MethodDescriptor{
		"QueryLocation":           {Action: "read", Resource: "location", Class: ClassRead},
		"QueryCharacter":          {Action: "read", Resource: "character", Class: ClassRead},
		"QueryLocationCharacters": {Action: "read", Resource: "location", Class: ClassRead},
		"QueryObject":             {Action: "read", Resource: "object", Class: ClassRead},
		"FindLocation":            {Action: "read", Resource: "location", Class: ClassRead},
	}},
	"property": {Token: "property", Methods: map[string]MethodDescriptor{
		"GetProperty": {Action: "read", Resource: "property", Class: ClassRead},
		"SetProperty": {Action: "write", Resource: "property", Class: ClassWrite},
	}},
	"session": {Token: "session", Methods: map[string]MethodDescriptor{
		"FindByName":       {Action: "read", Resource: "session", Class: ClassRead},
		"ListActive":       {Action: "list", Resource: "session", Class: ClassRead},
		"SetLastWhispered": {Action: "write", Resource: "session", Class: ClassWrite},
	}},
	"session.admin": {Token: "session.admin", Methods: map[string]MethodDescriptor{
		"Broadcast":  {Action: "write", Resource: "session", Class: ClassWrite},
		"Disconnect": {Action: "write", Resource: "session", Class: ClassWrite},
	}},
	"focus": {Token: "focus", Methods: map[string]MethodDescriptor{
		"JoinFocus":          {Action: "write", Resource: "focus", Class: ClassWrite},
		"LeaveFocus":         {Action: "write", Resource: "focus", Class: ClassWrite},
		"LeaveFocusByTarget": {Action: "write", Resource: "focus", Class: ClassWrite},
		"PresentFocus":       {Action: "write", Resource: "focus", Class: ClassWrite},
		"SetConnectionFocus": {Action: "write", Resource: "focus", Class: ClassWrite},
		"GetConnectionFocus": {Action: "read", Resource: "focus", Class: ClassRead},
		"AutoFocusOnJoin":    {Action: "write", Resource: "focus", Class: ClassWrite},
		"IsAnyConnFocused":   {Action: "read", Resource: "focus", Class: ClassRead},
	}},
	"emit": {Token: "emit", Methods: map[string]MethodDescriptor{
		"EmitEvent":        {Action: "write", Resource: "event", Class: ClassWrite},
		"RequestEmitToken": {Action: "write", Resource: "event", Class: ClassWrite},
		"RegisterEmitType": {Action: "write", Resource: "event", Class: ClassWrite},
	}},
	"stream.history": {Token: "stream.history", Methods: map[string]MethodDescriptor{
		"QueryStreamHistory": {Action: "read", Resource: "stream", Class: ClassRead},
	}},
	"stream.subscription": {Token: "stream.subscription", Methods: map[string]MethodDescriptor{
		"AddSessionStream":    {Action: "write", Resource: "stream", Class: ClassWrite},
		"RemoveSessionStream": {Action: "write", Resource: "stream", Class: ClassWrite},
	}},
	"audit": {Token: "audit", Methods: map[string]MethodDescriptor{
		"DecryptOwnAuditRows": {Action: "read", Resource: "audit", Class: ClassRead},
	}},
}

// init registers the scope vocabulary of each capability descriptor into the
// plugins package scope-token registry. Called once at program startup; the
// plugins package MUST NOT import hostcap (cycle), so hostcap registers inward.
func init() {
	for token, cap := range Descriptors {
		seen := map[string]bool{}
		var scopes []string
		for _, m := range cap.Methods {
			for _, s := range m.Scopes {
				if !seen[s] {
					seen[s] = true
					scopes = append(scopes, s)
				}
			}
		}
		if len(scopes) > 0 {
			plugins.RegisterCapabilityScopeTokens(token, scopes...)
		}
	}
}
