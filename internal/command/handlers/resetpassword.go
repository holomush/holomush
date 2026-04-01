// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/world"
)

const (
	generatedPasswordLen     = 16
	minPasswordLen           = 8
	resetPasswordCommandName = "resetpassword"
	resetPasswordUsage       = "resetpassword <username> [password] [--kick]"
	alphanumericChars        = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	capPasswordSet = "admin:password.set"
	capSessionKick = "admin:session.kick"
)

// CharacterLister is the ISP interface for listing characters by player.
type CharacterLister interface {
	ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error)
}

// AdminDeps holds the dependencies injected into the reset password handler.
type AdminDeps struct {
	PlayerRepo  auth.PlayerRepository
	Hasher      auth.PasswordHasher
	WebSessions auth.WebSessionRepository
	ResetRepo   auth.PasswordResetRepository
	CharLister  CharacterLister
}

type resetArgs struct {
	username string
	password string
	kick     bool
}

// NewResetPasswordHandler creates a command handler for admin password resets.
func NewResetPasswordHandler(deps AdminDeps) command.CommandHandler {
	return func(ctx context.Context, exec *command.CommandExecution) error {
		return handleResetPassword(ctx, exec, deps)
	}
}

func parseResetArgs(raw string) (resetArgs, error) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return resetArgs{}, command.ErrInvalidArgs(resetPasswordCommandName, resetPasswordUsage)
	}

	args := resetArgs{username: fields[0]}
	for _, f := range fields[1:] {
		switch {
		case strings.EqualFold(f, "--kick"):
			args.kick = true
		case args.password == "":
			args.password = f
		default:
			//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
			return resetArgs{}, command.ErrInvalidArgs(resetPasswordCommandName, resetPasswordUsage)
		}
	}
	return args, nil
}

func generatePassword() (string, error) {
	charsetLen := big.NewInt(int64(len(alphanumericChars)))
	buf := make([]byte, generatedPasswordLen)
	for i := range buf {
		idx, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", fmt.Errorf("crypto/rand failed: %w", err)
		}
		buf[i] = alphanumericChars[idx.Int64()]
	}
	return string(buf), nil
}

func handleResetPassword(ctx context.Context, exec *command.CommandExecution, deps AdminDeps) error {
	args, err := parseResetArgs(exec.Args)
	if err != nil {
		return err
	}

	engine := exec.Services().Engine()
	subject := fmt.Sprintf("character:%s", exec.CharacterID().String())

	// Capability checks: explicit password requires admin:password.set,
	// --kick requires admin:session.kick.
	if args.password != "" {
		//nolint:wrapcheck // CheckCapability returns structured oops errors with codes
		if capErr := command.CheckCapability(ctx, engine, subject, capPasswordSet, resetPasswordCommandName); capErr != nil {
			return capErr
		}
	}
	if args.kick {
		//nolint:wrapcheck // CheckCapability returns structured oops errors with codes
		if capErr := command.CheckCapability(ctx, engine, subject, capSessionKick, resetPasswordCommandName); capErr != nil {
			return capErr
		}
	}

	// Look up target player.
	player, err := deps.PlayerRepo.GetByUsername(ctx, args.username)
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			//nolint:wrapcheck // ErrTargetNotFound creates a structured oops error
			return command.ErrTargetNotFound(args.username)
		}
		slog.ErrorContext(ctx, "resetpassword player lookup failed",
			"target_username", args.username, "error", err)
		//nolint:wrapcheck // ErrResetPasswordFailed wraps the cause with oops
		return command.ErrResetPasswordFailed(err)
	}

	// Determine password.
	password := args.password
	generated := false
	if password == "" {
		password, err = generatePassword()
		if err != nil {
			//nolint:wrapcheck // ErrResetPasswordFailed wraps the cause with oops
			return command.ErrResetPasswordFailed(err)
		}
		generated = true
	}

	if len(password) < minPasswordLen {
		//nolint:wrapcheck // WorldError creates a structured oops error
		return command.WorldError(
			fmt.Sprintf("Password must be at least %d characters.", minPasswordLen), nil)
	}

	// Hash password.
	hash, err := deps.Hasher.Hash(password)
	if err != nil {
		//nolint:wrapcheck // ErrResetPasswordFailed wraps the cause with oops
		return command.ErrResetPasswordFailed(err)
	}

	// Targeted password update + lockout clear — single UPDATE touching only
	// password_hash, failed_attempts, locked_until, updated_at.
	if err := deps.PlayerRepo.UpdatePasswordAndClearLockout(ctx, player.ID, hash); err != nil {
		//nolint:wrapcheck // ErrResetPasswordFailed wraps the cause with oops
		return command.ErrResetPasswordFailed(err)
	}

	// Best-effort invalidation — track failures for accurate audit logging.
	var warnings []string

	if err := deps.ResetRepo.DeleteByPlayer(ctx, player.ID); err != nil {
		slog.WarnContext(ctx, "best-effort reset token cleanup failed",
			"player", args.username, "error", err)
		warnings = append(warnings, "reset token cleanup failed")
	}

	if err := deps.WebSessions.DeleteByPlayer(ctx, player.ID); err != nil {
		slog.WarnContext(ctx, "best-effort web session invalidation failed",
			"player", args.username, "error", err)
		warnings = append(warnings, "web session invalidation failed")
	}

	// If --kick: terminate game sessions for all player characters.
	kicksRequested := 0
	kicksCompleted := 0
	if args.kick {
		chars, listErr := deps.CharLister.ListByPlayer(ctx, player.ID)
		if listErr != nil {
			slog.WarnContext(ctx, "best-effort character listing for kick failed",
				"player", args.username, "error", listErr)
			warnings = append(warnings, "character listing for kick failed")
		} else {
			kicksRequested = len(chars)
			for _, ch := range chars {
				deletedSession, delErr := exec.Services().Session().DeleteByCharacter(ctx, ch.ID, "password reset kick")
				if delErr != nil {
					slog.WarnContext(ctx, "best-effort game session kick failed",
						"player", args.username, "character", ch.Name, "error", delErr)
				} else if deletedSession != nil {
					kicksCompleted++
				}
			}
			if kicksCompleted < kicksRequested {
				warnings = append(warnings, fmt.Sprintf("kicked %d/%d sessions", kicksCompleted, kicksRequested))
			}
		}
	}

	// Audit log — includes actual outcomes, not just requested flags.
	slog.InfoContext(ctx, "admin password reset",
		"event", "admin_password_reset",
		"admin_player_id", exec.PlayerID().String(),
		"admin_character_id", exec.CharacterID().String(),
		"admin_character_name", exec.CharacterName(),
		"target_player_id", player.ID.String(),
		"target_username", player.Username,
		"password_generated", args.password == "",
		"kick_requested", args.kick,
		"characters_found", kicksRequested,
		"kicks_completed", kicksCompleted,
		"partial_failure", len(warnings) > 0,
	)

	// Output result — include warnings if any best-effort operations failed.
	if generated {
		writeOutputf(ctx, exec, resetPasswordCommandName,
			"Password for %s has been reset.\nNew password: %s\n", args.username, password)
	} else {
		writeOutputf(ctx, exec, resetPasswordCommandName,
			"Password for %s has been reset.\n", args.username)
	}
	if len(warnings) > 0 {
		writeOutputf(ctx, exec, resetPasswordCommandName,
			"Warning: %s\n", strings.Join(warnings, "; "))
	}

	return nil
}
