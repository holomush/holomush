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
