// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"errors"
	"strings"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
)

func TestSanitizeAuthErrorReturnsEmptyForNil(t *testing.T) {
	assert.Equal(t, "", sanitizeAuthError(nil))
}

func TestSanitizeAuthErrorReturnsGenericForPlainError(t *testing.T) {
	// SECURITY: plain (non-oops) errors MUST NOT leak err.Error() to clients.
	err := errors.New(`pq: duplicate key value violates unique constraint "players_username_idx"`)
	assert.Equal(t, msgGenericRequestFailed, sanitizeAuthError(err))
}

func TestSanitizeAuthErrorReturnsGenericForUnknownCode(t *testing.T) {
	err := oops.Code("UNKNOWN_INTERNAL_CODE").
		With("schema", "auth_v3").
		Errorf("database connection refused on host db.internal.svc:5432")
	got := sanitizeAuthError(err)
	assert.Equal(t, msgGenericRequestFailed, got)
	assert.NotContains(t, got, "db.internal")
	assert.NotContains(t, got, "schema")
}

func TestSanitizeAuthErrorMapsKnownCodes(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected string
	}{
		// CreatePlayer.
		{"maps REGISTER_INVALID_USERNAME", "REGISTER_INVALID_USERNAME", msgRegisterInvalidUsername},
		{"maps REGISTER_INVALID_PASSWORD", "REGISTER_INVALID_PASSWORD", msgRegisterInvalidPassword},
		{"maps REGISTER_USERNAME_TAKEN", "REGISTER_USERNAME_TAKEN", msgRegisterUsernameTaken},
		{"maps REGISTER_FAILED", "REGISTER_FAILED", msgRegisterFailed},

		// CreateCharacter.
		{"maps CHARACTER_INVALID_NAME", "CHARACTER_INVALID_NAME", msgCharacterInvalidName},
		{"maps CHARACTER_NAME_TAKEN", "CHARACTER_NAME_TAKEN", msgCharacterNameTaken},
		{"maps CHARACTER_LIMIT_REACHED", "CHARACTER_LIMIT_REACHED", msgCharacterLimitReached},
		{"maps CHARACTER_CREATE_FAILED", "CHARACTER_CREATE_FAILED", msgCharacterCreateFailed},
		{"maps CHARACTER_NO_STARTING_LOCATION", "CHARACTER_NO_STARTING_LOCATION", msgCharacterNoStartingLocation},

		// charname.Gate refusals (plan 02-06). Before these cases existed all
		// four fell through to msgGenericRequestFailed, which is how the
		// PR #4941 "request failed" regression reached the browser.
		{"maps NAME_CONFUSABLE", "NAME_CONFUSABLE", msgCharacterNameConfusable},
		{"maps NAME_BLOCKED", "NAME_BLOCKED", msgCharacterNameBlocked},
		{"maps NAME_MIXED_SCRIPT", "NAME_MIXED_SCRIPT", msgCharacterNameMixedScript},
		{"maps NAME_SKELETON_UNVERIFIABLE", "NAME_SKELETON_UNVERIFIABLE", msgCharacterNameUnverifiable},

		// ConfirmPasswordReset.
		{"maps RESET_PASSWORD_EMPTY", "RESET_PASSWORD_EMPTY", msgResetPasswordEmpty},
		{"maps AUTH_INVALID_PASSWORD", "AUTH_INVALID_PASSWORD", msgResetInvalidPassword},
		{"maps RESET_TOKEN_EMPTY", "RESET_TOKEN_EMPTY", msgResetTokenEmpty},
		{"maps RESET_TOKEN_INVALID", "RESET_TOKEN_INVALID", msgResetTokenInvalid},
		{"maps RESET_TOKEN_EXPIRED", "RESET_TOKEN_EXPIRED", msgResetTokenExpired},
		{"maps RESET_VALIDATE_FAILED", "RESET_VALIDATE_FAILED", msgResetPasswordFailed},
		{"maps RESET_PASSWORD_FAILED", "RESET_PASSWORD_FAILED", msgResetPasswordFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Construct an oops error that embeds internal details the client must NOT see.
			err := oops.Code(tt.code).
				With("operation", "internal_op_do_not_leak").
				With("internal_host", "db.internal.svc:5432").
				Errorf("internal raw message that must not leak to clients")

			got := sanitizeAuthError(err)

			assert.Equal(t, tt.expected, got)
			assert.NotContains(t, got, "internal_op_do_not_leak")
			assert.NotContains(t, got, "db.internal.svc")
			assert.NotContains(t, got, "internal raw message")
		})
	}
}

// TestGateRefusalMessagesAreDistinctAndDiscloseNothing pins the two properties
// the table above cannot express.
//
// DISTINCTNESS is what makes the mapping load-bearing: four codes collapsed onto
// one message would satisfy every row of that table and still leave a player
// unable to tell "too similar" from "temporarily unavailable" — which is the
// generic-message defect this change exists to remove, one layer in.
func TestGateRefusalMessagesAreDistinctAndDiscloseNothing(t *testing.T) {
	messages := map[string]string{
		"NAME_CONFUSABLE":            sanitizeAuthError(oops.Code("NAME_CONFUSABLE").Errorf("x")),
		"NAME_BLOCKED":               sanitizeAuthError(oops.Code("NAME_BLOCKED").Errorf("x")),
		"NAME_MIXED_SCRIPT":          sanitizeAuthError(oops.Code("NAME_MIXED_SCRIPT").Errorf("x")),
		"NAME_SKELETON_UNVERIFIABLE": sanitizeAuthError(oops.Code("NAME_SKELETON_UNVERIFIABLE").Errorf("x")),
	}

	seen := map[string]string{}
	for code, msg := range messages {
		assert.NotEqual(t, msgGenericRequestFailed, msg,
			"%s must not fall through to the generic message", code)
		if other, dup := seen[msg]; dup {
			t.Errorf("%s and %s share the message %q; a player cannot tell them apart", code, other, msg)
		}
		seen[msg] = code
	}

	// The confusable refusal must not become an enumeration oracle: it says
	// that a similar name exists, never WHICH one (§6.1.2).
	confusable := messages["NAME_CONFUSABLE"]
	assert.NotContains(t, strings.ToLower(confusable), "cocoa")
	assert.Regexp(t, `(?i)similar`, confusable)

	// The block-list refusal must not echo operator configuration back — not
	// the pattern, not its index.
	blocked := messages["NAME_BLOCKED"]
	assert.NotContains(t, blocked, "pattern")
	assert.NotContains(t, blocked, "index")

	// The fail-closed state is TRANSIENT, not a rejection of the name.
	assert.Regexp(t, `(?i)again shortly`, messages["NAME_SKELETON_UNVERIFIABLE"])
}
