// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package bootstrap provides first-boot initialization for HoloMUSH.
package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/naming"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/world"
)

// CharacterCreator is the subset of auth.CharacterService needed for admin
// bootstrap. Its Create is genesis-backed (05-15): it commits the admin
// character + its genesis envelope atomically with NO binding (behavior
// preserved) — the signature is unchanged.
type CharacterCreator interface {
	Create(ctx context.Context, playerID ulid.ULID, name string) (*world.Character, error)
}

// SeedAdminDeps holds the dependencies for admin bootstrapping.
type SeedAdminDeps struct {
	PlayerRepo  auth.PlayerRepository
	CharService CharacterCreator
	RoleStore   store.RoleStore
	Hasher      auth.PasswordHasher
	NameTheme   naming.Theme
}

// SeedAdmin creates an admin player and character on first boot.
// Skips if any players already exist.
func SeedAdmin(ctx context.Context, deps SeedAdminDeps) error {
	count, err := deps.PlayerRepo.Count(ctx)
	if err != nil {
		return oops.Code("ADMIN_BOOTSTRAP_FAILED").Wrap(err)
	}
	if count > 0 {
		slog.DebugContext(ctx, "admin bootstrap skipped: players already exist", "count", count)
		return nil
	}

	username := envOrDefault("HOLOMUSH_ADMIN_USERNAME", "admin")
	password, generated, genErr := envOrGenerate("HOLOMUSH_ADMIN_PASSWORD")
	if genErr != nil {
		return oops.Code("ADMIN_BOOTSTRAP_FAILED").With("operation", "generate password").Wrap(genErr)
	}
	charName := os.Getenv("HOLOMUSH_ADMIN_CHARACTER")
	if charName == "" {
		firstName, secondName := deps.NameTheme.Generate()
		switch {
		case firstName != "":
			charName = firstName
		case secondName != "":
			charName = secondName
		default:
			return oops.Code("ADMIN_BOOTSTRAP_FAILED").Errorf("name theme returned empty names")
		}
	}

	hash, err := deps.Hasher.Hash(password)
	if err != nil {
		return oops.Code("ADMIN_BOOTSTRAP_FAILED").With("operation", "hash password").Wrap(err)
	}

	player, err := auth.NewPlayer(username, nil, hash)
	if err != nil {
		return oops.Code("ADMIN_BOOTSTRAP_FAILED").With("operation", "create player").Wrap(err)
	}

	// Ordering (round-4 B4): commit the admin PLAYER (its own pool), then run the
	// genesis transaction (character + envelope, NO binding — behavior preserved),
	// then assign the admin ROLE AFTER the genesis tx has committed. role_store
	// writes character_roles on its OWN pool and does NOT enroll in the world
	// transaction, so assigning the role while the character row is still
	// uncommitted in the outer world tx would BLOCK on the character_roles→
	// characters FK against a row held on a second connection. Assigning it after
	// the character commits avoids that cross-connection FK wait. player+character+
	// role are NOT one transaction (the round-3 claim was false); admin-role-missing
	// on a post-commit role failure is an accepted, documented compensation gap
	// (reconciled by a SeedAdmin re-run) outside INV-WORLD-4.
	if createErr := deps.PlayerRepo.Create(ctx, player); createErr != nil {
		return oops.Code("ADMIN_BOOTSTRAP_FAILED").With("operation", "persist player").Wrap(createErr)
	}
	char, charErr := deps.CharService.Create(ctx, player.ID, charName)
	if charErr != nil {
		return oops.Code("ADMIN_BOOTSTRAP_FAILED").With("operation", "create character").Wrap(charErr)
	}
	if roleErr := deps.RoleStore.AddRole(ctx, char.ID.String(), access.RoleAdmin); roleErr != nil {
		return oops.Code("ADMIN_BOOTSTRAP_FAILED").With("operation", "assign admin role").Wrap(roleErr)
	}

	slog.InfoContext(
		ctx,
		"admin account created",
		slog.String("username", username),
		slog.String("character", charName),
	)
	if generated {
		// Write generated password to stderr (not structured logs) to avoid
		// persisting credentials in log aggregation systems.
		fmt.Fprintf(os.Stderr, "\n  *** Admin password: %s ***\n  Change this immediately after first login.\n\n", password)
	}

	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrGenerate(key string) (value string, generated bool, err error) {
	if v := os.Getenv(key); v != "" {
		return v, false, nil
	}
	b := make([]byte, 18) // 18 bytes → 24 base64 chars
	if _, err := rand.Read(b); err != nil {
		return "", false, oops.Errorf("crypto/rand failed: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), true, nil
}
