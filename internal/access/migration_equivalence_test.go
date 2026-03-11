// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/audit"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// TestMigrationEquivalence validates that StaticAccessControl and AccessPolicyEngine
// produce identical authorization decisions for all production call sites.
//
// This test is critical for Phase 7.6 migration: it ensures that replacing
// StaticAccessControl.Check() with AccessPolicyEngine.Evaluate() does not break
// existing authorization behavior. Any divergence must be explicitly documented
// with a justification in the test case's comment field.
//
// Coverage: This test exercises representative samples from all 29 production call sites
// identified in docs/plans/2026-02-06-full-abac-phase-7.6.md Task 28.5.
//
// LIMITATION: The static engine's player role uses location-based permissions ($here token)
// which require a LocationResolver. Since this test uses simplified policies without location
// context, it focuses on:
//   - Admin role (full wildcard access)
//   - Builder role (world modification)
//   - Operations that don't depend on location context
//
// Location-based permission equivalence is validated separately in production integration tests
// where LocationResolver is available.
func TestMigrationEquivalence(t *testing.T) {
	ctx := context.Background()

	// Bootstrap both engines with identical data
	staticEngine := bootstrapStaticEngine(t)
	policyEngine := bootstrapPolicyEngine(t)

	tests := []struct {
		name     string
		subject  string
		action   string
		resource string
		comment  string // Document expected divergence if any
	}{
		// === Admin role tests (full wildcard access) ===

		// Command execution
		{
			name:     "admin - execute privileged command",
			subject:  "character:admin-01ABC",
			action:   "execute",
			resource: "command:shutdown",
		},
		{
			name:     "admin - grant capability",
			subject:  "character:admin-01ABC",
			action:   "grant",
			resource: "capability:custom",
		},

		// Location operations
		{
			name:     "admin - create location",
			subject:  "character:admin-01ABC",
			action:   "write",
			resource: "location:*",
		},
		{
			name:     "admin - delete location",
			subject:  "character:admin-01ABC",
			action:   "delete",
			resource: "location:01JKL",
		},

		// Exit operations
		{
			name:     "admin - delete exit",
			subject:  "character:admin-01ABC",
			action:   "delete",
			resource: "exit:01STU",
		},

		// Object operations
		{
			name:     "admin - delete object",
			subject:  "character:admin-01ABC",
			action:   "delete",
			resource: "object:01VWX",
		},

		// Character listing (decomposed from legacy location:*:characters read)
		{
			name:     "admin - list characters at location",
			subject:  "character:admin-01ABC",
			action:   "list_characters",
			resource: "location:01JKL",
			comment:  "EXPECTED DIVERGENCE: list_characters is a new decomposed action not present in static engine",
		},

		// Character operations
		{
			name:     "admin - move other character",
			subject:  "character:admin-01ABC",
			action:   "write",
			resource: "character:player-01DEF",
		},
		{
			name:     "admin - delete character",
			subject:  "character:admin-01ABC",
			action:   "delete",
			resource: "character:01YZA",
		},

		// === Builder role tests (world modification, no delete on locations) ===

		// Location operations
		{
			name:     "builder - create location",
			subject:  "character:builder-01GHI",
			action:   "write",
			resource: "location:*",
		},
		{
			name:     "builder - update location",
			subject:  "character:builder-01GHI",
			action:   "write",
			resource: "location:01JKL",
		},
		{
			name:     "builder - delete location (denied)",
			subject:  "character:builder-01GHI",
			action:   "delete",
			resource: "location:01JKL",
		},

		// Exit operations
		{
			// Both engines deny — builders do NOT have write:exit:* in either engine.
			name:     "builder - create exit",
			subject:  "character:builder-01GHI",
			action:   "write",
			resource: "exit:*",
		},
		{
			// Both engines deny — builders do NOT have write:exit:<id> in either engine.
			name:     "builder - update exit",
			subject:  "character:builder-01GHI",
			action:   "write",
			resource: "exit:01STU",
		},
		{
			name:     "builder - delete exit (denied)",
			subject:  "character:builder-01GHI",
			action:   "delete",
			resource: "exit:01STU",
		},

		// Object operations
		{
			name:     "builder - create object",
			subject:  "character:builder-01GHI",
			action:   "write",
			resource: "object:*",
		},
		{
			name:     "builder - update object",
			subject:  "character:builder-01GHI",
			action:   "write",
			resource: "object:01VWX",
		},
		{
			name:     "builder - delete object",
			subject:  "character:builder-01GHI",
			action:   "delete",
			resource: "object:01VWX",
		},

		// Command execution
		{
			name:     "builder - execute builder command",
			subject:  "character:builder-01GHI",
			action:   "execute",
			resource: "command:dig",
		},

		// Character listing (decomposed from legacy location:*:characters read)
		{
			name:     "builder - list characters at location",
			subject:  "character:builder-01GHI",
			action:   "list_characters",
			resource: "location:01JKL",
			comment:  "EXPECTED DIVERGENCE: list_characters is a new decomposed action not present in static engine",
		},

		// === Player role tests (basic operations, no world modification) ===

		// Operations that should be denied without location context
		{
			name:     "player - create location (denied)",
			subject:  "character:player-01DEF",
			action:   "write",
			resource: "location:*",
		},
		{
			name:     "player - create object (denied)",
			subject:  "character:player-01DEF",
			action:   "write",
			resource: "object:*",
		},
		{
			name:     "player - create exit (denied)",
			subject:  "character:player-01DEF",
			action:   "write",
			resource: "exit:*",
		},

		// Read/examine operations - players can read characters, locations, objects, exits, scenes
		{
			name:     "player - read character (examine)",
			subject:  "character:player-01DEF",
			action:   "read",
			resource: "character:player-01DEF",
		},
		{
			name:     "player - read other character (examine)",
			subject:  "character:player-01DEF",
			action:   "read",
			resource: "character:admin-01ABC",
			comment:  "EXPECTED DIVERGENCE: static engine uses $here token requiring LocationResolver; policy engine permits without location context",
		},
		{
			name:     "player - read location (look)",
			subject:  "character:player-01DEF",
			action:   "read",
			resource: "location:01JKL",
			comment:  "EXPECTED DIVERGENCE: static engine uses $here token requiring LocationResolver; policy engine permits without location context",
		},
		{
			name:     "player - read object (examine)",
			subject:  "character:player-01DEF",
			action:   "read",
			resource: "object:01VWX",
			comment:  "EXPECTED DIVERGENCE: static engine uses $here token requiring LocationResolver; policy engine permits without location context",
		},
		{
			name:     "player - read scene",
			subject:  "character:player-01DEF",
			action:   "read",
			resource: "scene:01MNO",
			comment:  "EXPECTED DIVERGENCE: static engine uses $here token requiring LocationResolver; policy engine permits without location context",
		},

		// Command execution - basic commands allowed
		{
			name:     "player - execute basic command",
			subject:  "character:player-01DEF",
			action:   "execute",
			resource: "command:say",
		},

		// Capabilities - no bypass for player
		{
			name:     "player - rate limit bypass (denied)",
			subject:  "character:player-01DEF",
			action:   "execute",
			resource: "capability:rate_limit_bypass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Old engine
			staticResult := staticEngine.Check(ctx, tt.subject, tt.action, tt.resource)

			// New engine
			req, err := types.NewAccessRequest(tt.subject, tt.action, tt.resource)
			require.NoError(t, err, "failed to create access request")

			decision, err := policyEngine.Evaluate(ctx, req)
			require.NoError(t, err, "policy engine evaluation failed")

			policyResult := decision.IsAllowed()

			// Validate equivalence
			if tt.comment == "" {
				// No documented divergence - decisions MUST match
				assert.Equal(t, staticResult, policyResult,
					"Decision mismatch: static=%v, policy=%v (subject=%s, action=%s, resource=%s, reason=%s, effect=%s)",
					staticResult, policyResult, tt.subject, tt.action, tt.resource, decision.Reason(), decision.Effect())
			} else {
				// Documented divergence — assert the direction: new engine is MORE permissive.
				// All known divergences are new-engine-more-permissive because they involve
				// decomposed actions (e.g., list_characters) or simplified location handling
				// that the static engine doesn't support. If a future divergence is
				// new-engine-more-restrictive, add a separate divergence category here.
				require.False(t, staticResult,
					"expected divergence: static engine should deny (got allow) for: %s", tt.name)
				require.True(t, policyResult,
					"expected divergence: policy engine should allow (got deny) for: %s — %s", tt.name, tt.comment)
			}
		})
	}
}

// bootstrapStaticEngine creates and configures a StaticAccessControl with production-equivalent
// role definitions. This engine uses the legacy role-based permission system.
func bootstrapStaticEngine(t *testing.T) access.AccessControl {
	t.Helper()

	// Use default roles (player, builder, admin)
	static := access.NewStaticAccessControl(nil, nil)

	// Assign roles to test subjects
	subjects := map[string]string{
		"character:admin-01ABC":   "admin",
		"character:builder-01GHI": "builder",
		"character:player-01DEF":  "player",
		"character:player-01MNO":  "player",
		"character:guest-01GHI":   "player", // Guests use player role in static engine
	}

	for subject, role := range subjects {
		err := static.AssignRole(subject, role)
		require.NoError(t, err, "failed to assign role %s to %s", role, subject)
	}

	return static
}

// bootstrapPolicyEngine creates and configures an AccessPolicyEngine with policies
// equivalent to the static engine's role definitions. This uses the new ABAC system.
func bootstrapPolicyEngine(t *testing.T) types.AccessPolicyEngine {
	t.Helper()

	// Create attribute resolver
	registry := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(registry)

	// Register character attribute provider
	charProvider := &testCharacterProvider{
		roles: map[string]string{
			"admin-01ABC":   "admin",
			"builder-01GHI": "builder",
			"player-01DEF":  "player",
			"player-01MNO":  "player",
			"guest-01GHI":   "player",
		},
	}
	err := resolver.RegisterProvider(charProvider)
	require.NoError(t, err)

	// Create audit logger - use ModeMinimal to minimize overhead in test
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test-wal.jsonl")
	auditLogger := audit.NewLogger(audit.ModeMinimal, &discardWriter{}, walPath)
	t.Cleanup(func() {
		_ = auditLogger.Close()
		_ = os.RemoveAll(tmpDir)
	})

	// Create cache with equivalent policies
	cache := createCacheWithEquivalentPolicies(t)

	// Create engine
	engine := policy.NewEngine(resolver, cache, nil, auditLogger)
	return engine
}

// testCharacterProvider is a mock attribute provider for character attributes.
type testCharacterProvider struct {
	roles map[string]string // charID → role
}

func (p *testCharacterProvider) Namespace() string {
	return "character"
}

func (p *testCharacterProvider) ResolveSubject(_ context.Context, subjectID string) (map[string]any, error) {
	// Extract character ID from "character:ID" format
	role, ok := p.roles[subjectID]
	if !ok {
		return map[string]any{}, nil
	}
	return map[string]any{
		"role": role,
	}, nil
}

func (p *testCharacterProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	return map[string]any{}, nil
}

func (p *testCharacterProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"role": types.AttrTypeString,
		},
	}
}

// discardWriter is a no-op audit writer for tests.
type discardWriter struct{}

func (d *discardWriter) WriteSync(_ context.Context, _ audit.Entry) error {
	return nil
}

func (d *discardWriter) WriteAsync(_ audit.Entry) error {
	return nil
}

func (d *discardWriter) Close() error {
	return nil
}

// createCacheWithEquivalentPolicies creates a policy cache with DSL policies that
// replicate the behavior of DefaultRoles() from internal/access/permissions.go.
//
// The static engine uses these role definitions:
//   - player: Basic commands, self access, current location access
//   - builder: Player powers + world modification (locations, objects, exits)
//   - admin: Full access (read/write/delete/execute/grant on all resources)
func createCacheWithEquivalentPolicies(t *testing.T) *policy.Cache {
	t.Helper()

	// Define DSL policies equivalent to DefaultRoles()
	// Note: These policies are simplified to match the static engine's behavior.
	// The static engine uses glob patterns which match broader than these specific policies,
	// but for the test subjects and resources used, the behavior is equivalent.
	dslPolicies := []string{
		// Player role: read/write self
		`permit(principal is character, action in ["read", "write"], resource is character) when { principal.character.role == "player" };`,

		// Player role: read locations, objects, exits, scenes
		`permit(principal is character, action in ["read"], resource is location) when { principal.character.role == "player" };`,
		`permit(principal is character, action in ["read"], resource is object) when { principal.character.role == "player" };`,
		`permit(principal is character, action in ["read"], resource is exit) when { principal.character.role == "player" };`,
		`permit(principal is character, action in ["read"], resource is scene) when { principal.character.role == "player" };`,

		// Player role: list characters at location (decomposed from legacy location:*:characters read)
		`permit(principal is character, action in ["list_characters"], resource is location) when { principal.character.role == "player" };`,

		// Player role: write scenes
		`permit(principal is character, action in ["write"], resource is scene) when { principal.character.role == "player" };`,

		// Player role: execute basic commands
		`permit(principal is character, action in ["execute"], resource is command) when { principal.character.role == "player" };`,

		// Builder role: all player permissions + world modification
		`permit(principal is character, action in ["read", "write"], resource is character) when { principal.character.role == "builder" };`,
		`permit(principal is character, action in ["read"], resource is location) when { principal.character.role == "builder" };`,
		`permit(principal is character, action in ["read"], resource is object) when { principal.character.role == "builder" };`,
		`permit(principal is character, action in ["read"], resource is exit) when { principal.character.role == "builder" };`,
		`permit(principal is character, action in ["read"], resource is scene) when { principal.character.role == "builder" };`,
		`permit(principal is character, action in ["write"], resource is scene) when { principal.character.role == "builder" };`,
		`permit(principal is character, action in ["execute"], resource is command) when { principal.character.role == "builder" };`,

		// Builder role: list characters at location (decomposed from legacy location:*:characters read)
		`permit(principal is character, action in ["list_characters"], resource is location) when { principal.character.role == "builder" };`,

		// Builder role: world modification
		`permit(principal is character, action in ["write"], resource is location) when { principal.character.role == "builder" };`,
		`permit(principal is character, action in ["write"], resource is object) when { principal.character.role == "builder" };`,
		`permit(principal is character, action in ["delete"], resource is object) when { principal.character.role == "builder" };`,
		// Note: builders do NOT have write:exit:* in the static engine (see permissions.go builderPowers).

		// Admin role: full access (wildcard action and resource)
		`permit(principal is character, action, resource) when { principal.character.role == "admin" };`,
	}

	schema := types.NewAttributeSchema()
	compiler := policy.NewCompiler(schema)

	policies := make([]policy.CachedPolicy, 0, len(dslPolicies))
	for i, dslText := range dslPolicies {
		compiled, _, err := compiler.Compile(dslText)
		require.NoError(t, err, "failed to compile policy %d: %v", i, err)

		policies = append(policies, policy.CachedPolicy{
			ID:       fmt.Sprintf("equiv-policy-%d", i+1),
			Name:     fmt.Sprintf("equivalence-test-policy-%d", i+1),
			Compiled: compiled,
		})
	}

	// Use the test helper to set snapshot
	return policy.NewCacheWithPoliciesForTest(policies)
}

func TestPolicyEngine_PlayerLocationPermissions(t *testing.T) {
	// Unit test verifying the policy engine correctly handles player location
	// read/write operations. This covers the gap where the static engine uses
	// $here tokens requiring a LocationResolver, but the policy engine evaluates
	// based on role attributes alone.
	ctx := context.Background()
	policyEngine := bootstrapPolicyEngine(t)

	tests := []struct {
		name    string
		subject string
		action  string
		resource string
		allowed bool
	}{
		{
			name:     "player can read own location",
			subject:  "character:player-01DEF",
			action:   "read",
			resource: "location:01JKL",
			allowed:  true,
		},
		{
			name:     "player can read any location",
			subject:  "character:player-01DEF",
			action:   "read",
			resource: "location:other-room",
			allowed:  true,
		},
		{
			name:     "player cannot write location",
			subject:  "character:player-01DEF",
			action:   "write",
			resource: "location:01JKL",
			allowed:  false,
		},
		{
			name:     "player cannot delete location",
			subject:  "character:player-01DEF",
			action:   "delete",
			resource: "location:01JKL",
			allowed:  false,
		},
		{
			name:     "builder can read location",
			subject:  "character:builder-01GHI",
			action:   "read",
			resource: "location:01JKL",
			allowed:  true,
		},
		{
			name:     "builder can write location",
			subject:  "character:builder-01GHI",
			action:   "write",
			resource: "location:01JKL",
			allowed:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := types.NewAccessRequest(tt.subject, tt.action, tt.resource)
			require.NoError(t, err)

			decision, err := policyEngine.Evaluate(ctx, req)
			require.NoError(t, err)
			assert.Equal(t, tt.allowed, decision.IsAllowed(),
				"subject=%s action=%s resource=%s: expected allowed=%v, got %v (reason: %s)",
				tt.subject, tt.action, tt.resource, tt.allowed, decision.IsAllowed(), decision.Reason())
		})
	}
}
