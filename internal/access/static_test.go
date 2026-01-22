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
	assert.False(t, ac.Check(ctx, "char:player1", "execute", "command:@dig"))
	assert.False(t, ac.Check(ctx, "char:player1", "write", "location:01ABC"))

	// Builder can use builder commands
	assert.True(t, ac.Check(ctx, "char:builder1", "execute", "command:@dig"))
	assert.True(t, ac.Check(ctx, "char:builder1", "write", "location:01ABC"))

	// Admin can do everything
	assert.True(t, ac.Check(ctx, "char:admin1", "execute", "command:@dig"))
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
	assert.False(t, ac.Check(ctx, "session:web-123", "execute", "command:@dig"))
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
