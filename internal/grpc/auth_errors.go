// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"github.com/samber/oops"
)

// Sanitized user-facing messages for auth-related error codes.
//
// SECURITY: Raw err.Error() strings from oops-wrapped errors can leak
// structured context (operation names, parameter values, wrapped chains)
// including schema/constraint identifiers and internal implementation
// details. Each code below maps to a fixed, safe message; the detailed
// error is logged server-side only.
const (
	msgGenericRequestFailed = "request failed"

	// CreatePlayer / registration.
	msgRegisterInvalidUsername = "invalid username"
	msgRegisterInvalidPassword = "invalid password"
	msgRegisterUsernameTaken   = "username is already taken"
	msgRegisterFailed          = "registration failed"

	// CreateCharacter.
	msgCharacterInvalidName        = "invalid character name"
	msgCharacterNameTaken          = "character name is already taken"
	msgCharacterLimitReached       = "character limit reached"
	msgCharacterCreateFailed       = "character creation failed"
	msgCharacterNoStartingLocation = "character creation unavailable"

	// charname.Gate refusals (plan 02-06). Without these four the gate's
	// verdicts all fell through to msgGenericRequestFailed, so a player was
	// told "request failed" for every name the admission decision declined —
	// including the exact-duplicate case, which is how the PR #4941 regression
	// surfaced.
	//
	// SECURITY: msgCharacterNameConfusable names NO colliding character and
	// msgCharacterNameBlocked names NO pattern or index. §6.1.2 and the
	// block-list design are both explicit that these refusals must not become
	// enumeration oracles — a message that said WHICH character, or WHICH
	// pattern, would let an attacker read back the corpus or the operator's
	// configuration one submission at a time. They deliberately mirror the
	// wording charname.Gate itself uses.
	msgCharacterNameConfusable  = "that character name is too similar to an existing one"
	msgCharacterNameBlocked     = "that character name is not available"
	msgCharacterNameMixedScript = "character names cannot mix writing systems; please use one"

	// msgCharacterNameUnverifiable is the D-30 fail-closed state, and it reads
	// as TRANSIENT on purpose: the name was not rejected. The corpus simply
	// could not answer the confusability question yet (some row's skeleton is
	// still unpopulated), so the gate refused to adjudicate rather than guess.
	// Telling the player their name is unacceptable would be false and would
	// send them off to pick a different one for no reason.
	msgCharacterNameUnverifiable = "character name check is temporarily unavailable; please try again shortly"

	// ConfirmPasswordReset.
	msgResetPasswordEmpty   = "new password cannot be empty"
	msgResetInvalidPassword = "invalid password"
	msgResetTokenEmpty      = "reset token cannot be empty"
	msgResetTokenInvalid    = "reset token is invalid"
	msgResetTokenExpired    = "reset token has expired"
	msgResetPasswordFailed  = "password reset failed"
)

// sanitizeAuthError maps a known oops error code to a fixed user-facing
// message. Unknown or non-oops errors fall through to a generic message
// so that internal details never reach the client.
//
// Callers MUST log the raw error server-side (slog.WarnContext) before
// returning the sanitized message to the caller.
func sanitizeAuthError(err error) string {
	if err == nil {
		return ""
	}
	oopsErr, isOops := oops.AsOops(err)
	if !isOops {
		return msgGenericRequestFailed
	}
	switch oopsErr.Code() {
	// CreatePlayer.
	case "REGISTER_INVALID_USERNAME":
		return msgRegisterInvalidUsername
	case "REGISTER_INVALID_PASSWORD":
		return msgRegisterInvalidPassword
	case "REGISTER_USERNAME_TAKEN":
		return msgRegisterUsernameTaken
	case "REGISTER_FAILED":
		return msgRegisterFailed

	// CreateCharacter.
	case "CHARACTER_INVALID_NAME":
		return msgCharacterInvalidName
	case "CHARACTER_NAME_TAKEN":
		return msgCharacterNameTaken
	case "CHARACTER_LIMIT_REACHED":
		return msgCharacterLimitReached
	case "CHARACTER_CREATE_FAILED":
		return msgCharacterCreateFailed
	case "CHARACTER_NO_STARTING_LOCATION":
		return msgCharacterNoStartingLocation

	// charname.Gate refusals (plan 02-06).
	case "NAME_CONFUSABLE":
		return msgCharacterNameConfusable
	case "NAME_BLOCKED":
		return msgCharacterNameBlocked
	case "NAME_MIXED_SCRIPT":
		return msgCharacterNameMixedScript
	case "NAME_SKELETON_UNVERIFIABLE":
		return msgCharacterNameUnverifiable

	// ConfirmPasswordReset.
	case "RESET_PASSWORD_EMPTY":
		return msgResetPasswordEmpty
	case "AUTH_INVALID_PASSWORD":
		return msgResetInvalidPassword
	case "RESET_TOKEN_EMPTY":
		return msgResetTokenEmpty
	case "RESET_TOKEN_INVALID":
		return msgResetTokenInvalid
	case "RESET_TOKEN_EXPIRED":
		return msgResetTokenExpired
	case "RESET_VALIDATE_FAILED", "RESET_PASSWORD_FAILED":
		return msgResetPasswordFailed
	}
	return msgGenericRequestFailed
}
