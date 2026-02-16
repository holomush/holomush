// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access_test

import (
	"context"
	"testing"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/plugin/capability"
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStaticAccessControl_SystemAlwaysAllowed(t *testing.T) {
	ac := access.NewStaticAccessControl(nil, nil)

	ctx := context.Background()

	// System can do anything
	assert.True(t, ac.Check(ctx, "system", "read", "anything"))
	assert.True(t, ac.Check(ctx, "system", "write", "location:*"))
	assert.True(t, ac.Check(ctx, "system", "grant", "character:admin"))
}

func TestStaticAccessControl_UnknownSubjectDenied(t *testing.T) {
	ac := access.NewStaticAccessControl(nil, nil)

	ctx := context.Background()

	// Unknown subjects are denied by default
	assert.False(t, ac.Check(ctx, "char:unknown", "read", "anything"))
	assert.False(t, ac.Check(ctx, "session:unknown", "read", "anything"))
	assert.False(t, ac.Check(ctx, "", "read", "anything"))
}

func TestStaticAccessControl_PluginDelegatesToEnforcer(t *testing.T) {
	enforcer := capability.NewEnforcer()
	// Capability format: action.resource (colons become dots)
	// When Check gets action="read" resource="location:01ABC", it creates "read.location:01ABC"
	err := enforcer.SetGrants("echo-bot", []string{"read.location:*", "emit.stream:*"})
	require.NoError(t, err)

	ac := access.NewStaticAccessControl(nil, enforcer)
	ctx := context.Background()

	// Plugin with matching capability allowed
	assert.True(t, ac.Check(ctx, "plugin:echo-bot", "read", "location:01ABC"))
	assert.True(t, ac.Check(ctx, "plugin:echo-bot", "emit", "stream:location:01ABC"))

	// Plugin without matching capability denied
	assert.False(t, ac.Check(ctx, "plugin:echo-bot", "write", "location:01ABC"))
	assert.False(t, ac.Check(ctx, "plugin:echo-bot", "grant", "admin"))

	// Unknown plugin denied
	assert.False(t, ac.Check(ctx, "plugin:unknown", "read", "location:01ABC"))
}

func TestStaticAccessControl_PluginWithNilEnforcer(t *testing.T) {
	ac := access.NewStaticAccessControl(nil, nil)
	ctx := context.Background()

	// All plugin checks fail with nil enforcer
	assert.False(t, ac.Check(ctx, "plugin:echo-bot", "read", "location:01ABC"))
	assert.False(t, ac.Check(ctx, "plugin:any", "emit", "stream:test"))
}

func TestStaticAccessControl_RoleBasedAccess(t *testing.T) {
	ac := access.NewStaticAccessControl(nil, nil)
	ctx := context.Background()

	// Assign roles
	require.NoError(t, ac.AssignRole("char:player1", "player"))
	require.NoError(t, ac.AssignRole("char:builder1", "builder"))
	require.NoError(t, ac.AssignRole("char:admin1", "admin"))

	// Player can execute basic commands
	assert.True(t, ac.Check(ctx, "char:player1", "execute", "command:say"))
	assert.True(t, ac.Check(ctx, "char:player1", "execute", "command:look"))

	// Player cannot use builder commands
	assert.False(t, ac.Check(ctx, "char:player1", "execute", "command:dig"))
	assert.False(t, ac.Check(ctx, "char:player1", "write", "location:01ABC"))

	// Builder can use builder commands
	assert.True(t, ac.Check(ctx, "char:builder1", "execute", "command:dig"))
	assert.True(t, ac.Check(ctx, "char:builder1", "write", "location:01ABC"))

	// Admin can do everything
	assert.True(t, ac.Check(ctx, "char:admin1", "execute", "command:dig"))
	assert.True(t, ac.Check(ctx, "char:admin1", "grant", "anything"))
	assert.True(t, ac.Check(ctx, "char:admin1", "delete", "world:everything"))
}

func TestStaticAccessControl_AssignRole(t *testing.T) {
	ac := access.NewStaticAccessControl(nil, nil)
	ctx := context.Background()

	// Before assignment - denied
	assert.False(t, ac.Check(ctx, "char:test", "execute", "command:say"))

	// After assignment - allowed
	require.NoError(t, ac.AssignRole("char:test", "player"))
	assert.True(t, ac.Check(ctx, "char:test", "execute", "command:say"))

	// Change role
	require.NoError(t, ac.AssignRole("char:test", "admin"))
	assert.True(t, ac.Check(ctx, "char:test", "grant", "anything"))
}

func TestStaticAccessControl_RevokeRole(t *testing.T) {
	ac := access.NewStaticAccessControl(nil, nil)
	ctx := context.Background()

	require.NoError(t, ac.AssignRole("char:test", "player"))
	assert.True(t, ac.Check(ctx, "char:test", "execute", "command:say"))

	require.NoError(t, ac.RevokeRole("char:test"))
	assert.False(t, ac.Check(ctx, "char:test", "execute", "command:say"))
}

func TestStaticAccessControl_GetRole(t *testing.T) {
	ac := access.NewStaticAccessControl(nil, nil)

	// No role assigned
	assert.Equal(t, "", ac.GetRole("char:unknown"))

	// Role assigned
	require.NoError(t, ac.AssignRole("char:test", "builder"))
	assert.Equal(t, "builder", ac.GetRole("char:test"))
}

func TestStaticAccessControl_UnknownPrefixDenied(t *testing.T) {
	ac := access.NewStaticAccessControl(nil, nil)
	ctx := context.Background()

	// Unknown prefix types are denied
	assert.False(t, ac.Check(ctx, "object:01ABC", "read", "anything"))
	assert.False(t, ac.Check(ctx, "location:01ABC", "read", "anything"))
	assert.False(t, ac.Check(ctx, "invalid", "read", "anything"))
}

func TestStaticAccessControl_SessionSubjects(t *testing.T) {
	ac := access.NewStaticAccessControl(nil, nil)
	ctx := context.Background()

	// Session subjects work like char subjects
	require.NoError(t, ac.AssignRole("session:web-123", "player"))
	assert.True(t, ac.Check(ctx, "session:web-123", "execute", "command:say"))
	assert.False(t, ac.Check(ctx, "session:web-123", "execute", "command:dig"))
}

func TestStaticAccessControl_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	ac := access.NewStaticAccessControl(nil, nil)
	ctx := context.Background()

	done := make(chan bool)

	// Concurrent reads and writes
	for i := 0; i < 100; i++ {
		go func(n int) {
			subject := "char:user" + string(rune('0'+n%10))
			_ = ac.AssignRole(subject, "player")
			ac.Check(ctx, subject, "execute", "command:say")
			ac.GetRole(subject)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}
}

func TestStaticAccessControl_UnknownRoleReturnsError(t *testing.T) {
	ac := access.NewStaticAccessControl(nil, nil)

	// Assign a non-existent role returns error
	err := ac.AssignRole("char:test", "nonexistent")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "UNKNOWN_ROLE")
	errutil.AssertErrorContext(t, err, "role", "nonexistent")
}

func TestStaticAccessControl_AssignRoleValidation(t *testing.T) {
	ac := access.NewStaticAccessControl(nil, nil)

	tests := []struct {
		name        string
		subject     string
		role        string
		wantCode    string
		wantContext map[string]any
	}{
		{
			name:     "empty subject",
			subject:  "",
			role:     "player",
			wantCode: "INVALID_SUBJECT",
		},
		{
			name:     "empty role",
			subject:  "char:test",
			role:     "",
			wantCode: "INVALID_ROLE",
		},
		{
			name:        "unknown role",
			subject:     "char:test",
			role:        "superadmin",
			wantCode:    "UNKNOWN_ROLE",
			wantContext: map[string]any{"role": "superadmin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ac.AssignRole(tt.subject, tt.role)
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, tt.wantCode)
			for k, v := range tt.wantContext {
				errutil.AssertErrorContext(t, err, k, v)
			}
		})
	}
}

func TestStaticAccessControl_RevokeRoleValidation(t *testing.T) {
	ac := access.NewStaticAccessControl(nil, nil)

	// Empty subject returns error
	err := ac.RevokeRole("")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_SUBJECT")

	// Non-existent subject is not an error (idempotent)
	err = ac.RevokeRole("char:nonexistent")
	assert.NoError(t, err)
}

func TestStaticAccessControl_InvalidRolePattern(t *testing.T) {
	// Test that invalid glob patterns in roles cause NewStaticAccessControlWithRoles
	// to return an error rather than silently skipping the pattern.
	invalidRoles := map[string][]string{
		"player": {"read:[invalid"}, // Invalid glob pattern (unclosed bracket)
	}

	_, err := access.NewStaticAccessControlWithRoles(invalidRoles, nil, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_PERMISSION_PATTERN")
	errutil.AssertErrorContext(t, err, "role", "player")
	errutil.AssertErrorContext(t, err, "pattern", "read:[invalid")
}

func TestStaticAccessControl_MalformedSubjectID(t *testing.T) {
	// Subject IDs with glob metacharacters could break pattern compilation
	// when $self is resolved. Verify this is handled safely (denied, not crash).
	ac := access.NewStaticAccessControl(nil, nil)
	ctx := context.Background()

	// Subject with invalid glob chars - should not cause crash or allow bypass
	err := ac.AssignRole("char:[invalid", "player")
	require.NoError(t, err)

	// Permission check should deny (pattern fails to compile after $self resolution)
	// The permission "read:character:$self" becomes "read:character:[invalid"
	// which is an invalid glob pattern and should be safely denied.
	assert.False(t, ac.Check(ctx, "char:[invalid", "read", "character:[invalid"))

	// Non-$self permissions should still work
	assert.True(t, ac.Check(ctx, "char:[invalid", "execute", "command:say"))
}

func TestStaticAccessControl_MalformedLocationID(t *testing.T) {
	// Location IDs with glob metacharacters returned by LocationResolver
	// could break pattern compilation when $here is resolved.
	// Verify this is handled safely (denied, not crash).
	resolver := &testLocationResolver{
		locations: map[string]string{
			"testchar": "[invalid-room", // Invalid glob pattern in location ID
		},
	}
	ac := access.NewStaticAccessControl(resolver, nil)
	ctx := context.Background()

	err := ac.AssignRole("char:testchar", "player")
	require.NoError(t, err)

	// Permission check should deny (pattern fails to compile after $here resolution)
	// The permission "read:location:$here" becomes "read:location:[invalid-room"
	// which is an invalid glob pattern and should be safely denied.
	assert.False(t, ac.Check(ctx, "char:testchar", "read", "location:[invalid-room"))

	// Non-$here permissions should still work
	assert.True(t, ac.Check(ctx, "char:testchar", "execute", "command:say"))
	assert.True(t, ac.Check(ctx, "char:testchar", "read", "character:testchar"))
}

func TestStaticAccessControl_SelfToken(t *testing.T) {
	ac := access.NewStaticAccessControl(nil, nil)
	ctx := context.Background()

	// Assign player role (has read:character:$self)
	err := ac.AssignRole("char:01ABC", "player")
	require.NoError(t, err)

	// Can read own character ($self resolves to 01ABC)
	assert.True(t, ac.Check(ctx, "char:01ABC", "read", "character:01ABC"))

	// Cannot read other character
	assert.False(t, ac.Check(ctx, "char:01ABC", "read", "character:01XYZ"))

	// Can write own character
	assert.True(t, ac.Check(ctx, "char:01ABC", "write", "character:01ABC"))
}

func TestStaticAccessControl_SelfTokenMultipleSubjects(t *testing.T) {
	ac := access.NewStaticAccessControl(nil, nil)
	ctx := context.Background()

	// Assign player role to multiple characters
	require.NoError(t, ac.AssignRole("char:ALICE", "player"))
	require.NoError(t, ac.AssignRole("char:BOB", "player"))

	// Each can only access their own character
	assert.True(t, ac.Check(ctx, "char:ALICE", "read", "character:ALICE"))
	assert.False(t, ac.Check(ctx, "char:ALICE", "read", "character:BOB"))

	assert.True(t, ac.Check(ctx, "char:BOB", "read", "character:BOB"))
	assert.False(t, ac.Check(ctx, "char:BOB", "read", "character:ALICE"))
}

// testLocationResolver is a mock for LocationResolver.
type testLocationResolver struct {
	locations  map[string]string   // charID → locationID
	characters map[string][]string // locationID → charIDs
}

func (r *testLocationResolver) CurrentLocation(_ context.Context, charID string) (string, error) {
	if loc, ok := r.locations[charID]; ok {
		return loc, nil
	}
	return "", nil
}

func (r *testLocationResolver) CharactersAt(_ context.Context, locationID string) ([]string, error) {
	return r.characters[locationID], nil
}

func TestStaticAccessControl_HereToken(t *testing.T) {
	resolver := &testLocationResolver{
		locations: map[string]string{
			"01ABC": "room1",
		},
	}
	ac := access.NewStaticAccessControl(resolver, nil)
	ctx := context.Background()

	// Assign player role (has read:location:$here)
	err := ac.AssignRole("char:01ABC", "player")
	require.NoError(t, err)

	// Can read current location
	assert.True(t, ac.Check(ctx, "char:01ABC", "read", "location:room1"))

	// Cannot read other location
	assert.False(t, ac.Check(ctx, "char:01ABC", "read", "location:room2"))

	// Can emit to current location stream
	assert.True(t, ac.Check(ctx, "char:01ABC", "emit", "stream:location:room1"))
}

func TestStaticAccessControl_HereTokenNullResolver(t *testing.T) {
	// With NullResolver, $here never matches (no location returned)
	ac := access.NewStaticAccessControl(nil, nil)
	ctx := context.Background()

	err := ac.AssignRole("char:01ABC", "player")
	require.NoError(t, err)

	// Cannot read any location (no $here resolution)
	assert.False(t, ac.Check(ctx, "char:01ABC", "read", "location:room1"))
	assert.False(t, ac.Check(ctx, "char:01ABC", "read", "location:room2"))
}

func TestStaticAccessControl_HereTokenMultipleCharacters(t *testing.T) {
	resolver := &testLocationResolver{
		locations: map[string]string{
			"ALICE": "room1",
			"BOB":   "room2",
		},
	}
	ac := access.NewStaticAccessControl(resolver, nil)
	ctx := context.Background()

	require.NoError(t, ac.AssignRole("char:ALICE", "player"))
	require.NoError(t, ac.AssignRole("char:BOB", "player"))

	// Alice can read her room, not Bob's
	assert.True(t, ac.Check(ctx, "char:ALICE", "read", "location:room1"))
	assert.False(t, ac.Check(ctx, "char:ALICE", "read", "location:room2"))

	// Bob can read his room, not Alice's
	assert.True(t, ac.Check(ctx, "char:BOB", "read", "location:room2"))
	assert.False(t, ac.Check(ctx, "char:BOB", "read", "location:room1"))
}

// errorLocationResolver returns an error from CurrentLocation.
type errorLocationResolver struct {
	err error
}

func (r *errorLocationResolver) CurrentLocation(_ context.Context, _ string) (string, error) {
	return "", r.err
}

func (r *errorLocationResolver) CharactersAt(_ context.Context, _ string) ([]string, error) {
	return nil, r.err
}

func TestStaticAccessControl_HereTokenResolverError(t *testing.T) {
	// When LocationResolver returns an error, $here tokens should not resolve,
	// resulting in location-based permissions being denied (fail-safe behavior).
	// This documents the intentional fail-closed security posture.
	resolver := &errorLocationResolver{err: context.DeadlineExceeded}
	ac := access.NewStaticAccessControl(resolver, nil)
	ctx := context.Background()

	err := ac.AssignRole("char:01ABC", "player")
	require.NoError(t, err)

	// Location-based permissions denied when resolver fails
	// (player role has read:location:$here which can't resolve)
	assert.False(t, ac.Check(ctx, "char:01ABC", "read", "location:room1"))

	// Non-location permissions still work
	assert.True(t, ac.Check(ctx, "char:01ABC", "execute", "command:say"))
	assert.True(t, ac.Check(ctx, "char:01ABC", "read", "character:01ABC")) // $self works
}

func TestStaticAccessControl_DualPrefixRegression(t *testing.T) {
	// Regression test: Verify that both "char:" and "character:" prefixes
	// produce identical results. During Phase 7.6 migration to AccessPolicyEngine,
	// the "character:" prefix was added alongside the legacy "char:" prefix.
	// TEMPORARY: This dual-prefix support will be removed in Phase 7.7 (tracked by holomush-c6qch).
	// This test ensures that both prefixes are handled equivalently, so that
	// if someone accidentally removes "character:" from the switch statement,
	// the test will catch the breakage.

	tests := []struct {
		name      string
		role      string
		action    string
		resource  string
		wantAllow bool
	}{
		{
			name:      "player allow - execute say",
			role:      "player",
			action:    "execute",
			resource:  "command:say",
			wantAllow: true,
		},
		{
			name:      "player deny - execute dig",
			role:      "player",
			action:    "execute",
			resource:  "command:dig",
			wantAllow: false,
		},
		{
			name:      "admin allow - grant role",
			role:      "admin",
			action:    "grant",
			resource:  "role:any",
			wantAllow: true,
		},
		{
			name:      "builder allow - write location",
			role:      "builder",
			action:    "write",
			resource:  "location:room1",
			wantAllow: true,
		},
		{
			name:      "player deny - write location",
			role:      "player",
			action:    "write",
			resource:  "location:room1",
			wantAllow: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test with "char:" prefix
			acChar := access.NewStaticAccessControl(nil, nil)
			ctxChar := context.Background()
			require.NoError(t, acChar.AssignRole("char:testsubject", tt.role))
			charResult := acChar.Check(ctxChar, "char:testsubject", tt.action, tt.resource)

			// Test with "character:" prefix
			acCharacter := access.NewStaticAccessControl(nil, nil)
			ctxCharacter := context.Background()
			require.NoError(t, acCharacter.AssignRole("character:testsubject", tt.role))
			characterResult := acCharacter.Check(ctxCharacter, "character:testsubject", tt.action, tt.resource)

			// Both should produce identical results
			assert.Equal(t, charResult, characterResult,
				"char: and character: prefixes should produce identical results")
			assert.Equal(t, tt.wantAllow, charResult,
				"char: prefix result should match expected value")
			assert.Equal(t, tt.wantAllow, characterResult,
				"character: prefix result should match expected value")
		})
	}
}

func TestStaticAccessControl_CharacterPrefixEquivalence(t *testing.T) {
	// Simplified test: "character:" prefix should work identically to "char:" prefix.
	// TEMPORARY: This dual-prefix support will be removed in Phase 7.7 (tracked by holomush-c6qch).
	// This ensures the migration path is safe and can be reverted without issues.
	ac := access.NewStaticAccessControl(nil, nil)
	ctx := context.Background()

	require.NoError(t, ac.AssignRole("char:alice", "player"))
	require.NoError(t, ac.AssignRole("character:bob", "player"))

	// Both prefixes with same role should have same permissions
	charAliceCanSay := ac.Check(ctx, "char:alice", "execute", "command:say")
	charAliceCantDig := !ac.Check(ctx, "char:alice", "execute", "command:dig")

	charBobCanSay := ac.Check(ctx, "character:bob", "execute", "command:say")
	charBobCantDig := !ac.Check(ctx, "character:bob", "execute", "command:dig")

	assert.True(t, charAliceCanSay, "char:alice should execute say")
	assert.True(t, charAliceCantDig, "char:alice should not execute dig")
	assert.True(t, charBobCanSay, "character:bob should execute say")
	assert.True(t, charBobCantDig, "character:bob should not execute dig")
}

func TestStaticAccessControl_DualPrefixSameSubjectID(t *testing.T) {
	// End-to-end test: Verify that a subject ID with "char:" prefix produces the same
	// evaluation result as "character:" prefix through the full permission check path.
	// This validates the prefix translation in StaticAccessControl.Check() switch statement.
	// TEMPORARY: This dual-prefix support will be removed in Phase 7.7 (tracked by holomush-c6qch).
	//
	// Regression scenario: If someone accidentally removes "char:" from the switch case,
	// this test will catch the breakage by showing different results for the same ID.
	//
	// Note: StaticAccessControl stores roles keyed by raw subject string, so each prefix
	// needs its own role assignment. This tests that both prefixes route correctly through
	// the Check() switch and produce identical results for the same role.
	subjectID := "01ABC"
	ctx := context.Background()

	// Assign role via char: prefix, verify it works
	acChar := access.NewStaticAccessControl(nil, nil)
	require.NoError(t, acChar.AssignRole("char:"+subjectID, "player"))
	charAllow := acChar.Check(ctx, "char:"+subjectID, "execute", "command:say")
	charDeny := acChar.Check(ctx, "char:"+subjectID, "execute", "command:dig")

	// Assign role via character: prefix, verify it works
	acCharacter := access.NewStaticAccessControl(nil, nil)
	require.NoError(t, acCharacter.AssignRole("character:"+subjectID, "player"))
	characterAllow := acCharacter.Check(ctx, "character:"+subjectID, "execute", "command:say")
	characterDeny := acCharacter.Check(ctx, "character:"+subjectID, "execute", "command:dig")

	// Both prefixes should produce identical results for same role
	assert.Equal(t, characterAllow, charAllow,
		"char: and character: prefixes for same ID should produce identical allow results")
	assert.True(t, charAllow, "player role should allow execute:command:say via char:")
	assert.True(t, characterAllow, "player role should allow execute:command:say via character:")

	assert.Equal(t, characterDeny, charDeny,
		"char: and character: prefixes should deny the same actions")
	assert.False(t, charDeny, "player role should not allow execute:command:dig via char:")
	assert.False(t, characterDeny, "player role should not allow execute:command:dig via character:")
}

func TestStaticAccessControl_DualPrefixWithTokenResolution(t *testing.T) {
	// End-to-end test: Verify both "char:" and "character:" prefixes work through
	// the full path including $self token resolution (which extracts the subject ID).
	// TEMPORARY: This dual-prefix support will be removed in Phase 7.7 (tracked by holomush-c6qch).
	//
	// This ensures that the prefix translation doesn't break $self resolution in checkRole().
	//
	// Note: Roles are stored keyed by raw subject string, so each prefix instance
	// needs its own assignment. We verify $self resolution works for both prefixes.
	subjectID := "01XYZ"
	otherID := "01OTHER"
	ctx := context.Background()

	// Test char: prefix with $self resolution
	acChar := access.NewStaticAccessControl(nil, nil)
	require.NoError(t, acChar.AssignRole("char:"+subjectID, "player"))
	charCanReadSelf := acChar.Check(ctx, "char:"+subjectID, "read", "character:"+subjectID)
	charCannotReadOther := acChar.Check(ctx, "char:"+subjectID, "read", "character:"+otherID)

	// Test character: prefix with $self resolution
	acCharacter := access.NewStaticAccessControl(nil, nil)
	require.NoError(t, acCharacter.AssignRole("character:"+subjectID, "player"))
	characterCanReadSelf := acCharacter.Check(ctx, "character:"+subjectID, "read", "character:"+subjectID)
	characterCannotReadOther := acCharacter.Check(ctx, "character:"+subjectID, "read", "character:"+otherID)

	// Both should allow reading own character ($self)
	assert.True(t, charCanReadSelf, "char: prefix should resolve $self correctly")
	assert.True(t, characterCanReadSelf, "character: prefix should resolve $self correctly")
	assert.Equal(t, characterCanReadSelf, charCanReadSelf,
		"$self resolution should work identically for both prefixes")

	// Both should deny reading other characters
	assert.False(t, charCannotReadOther, "char: prefix should deny reading other character")
	assert.False(t, characterCannotReadOther, "character: prefix should deny reading other character")
	assert.Equal(t, characterCannotReadOther, charCannotReadOther,
		"both prefixes should deny access to other characters")
}
