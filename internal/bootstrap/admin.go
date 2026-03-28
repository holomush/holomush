// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

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

// CharacterCreator is the subset of auth.CharacterService needed for admin bootstrap.
type CharacterCreator interface {
	Create(ctx context.Context, playerID ulid.ULID, name string) (*world.Character, error)
}

// Transactor wraps a function in a database transaction.
type Transactor interface {
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// SeedAdminDeps holds the dependencies for admin bootstrapping.
type SeedAdminDeps struct {
	PlayerRepo  auth.PlayerRepository
	CharService CharacterCreator
	RoleStore   store.RoleStore
	Hasher      auth.PasswordHasher
	NameTheme   naming.Theme
	Transactor  Transactor // optional; nil = no transaction wrapping
}

// SeedAdmin creates an admin player and character on first boot.
// Skips if any players already exist.
func SeedAdmin(ctx context.Context, deps SeedAdminDeps) error {
	count, err := deps.PlayerRepo.Count(ctx)
	if err != nil {
		return oops.Code("ADMIN_BOOTSTRAP_FAILED").Wrap(err)
	}
	if count > 0 {
		slog.Debug("admin bootstrap skipped: players already exist", "count", count)
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

	var char *world.Character
	writesFn := func(txCtx context.Context) error {
		if createErr := deps.PlayerRepo.Create(txCtx, player); createErr != nil {
			return oops.Code("ADMIN_BOOTSTRAP_FAILED").With("operation", "persist player").Wrap(createErr)
		}
		var charErr error
		char, charErr = deps.CharService.Create(txCtx, player.ID, charName)
		if charErr != nil {
			return oops.Code("ADMIN_BOOTSTRAP_FAILED").With("operation", "create character").Wrap(charErr)
		}
		if roleErr := deps.RoleStore.AddRole(txCtx, char.ID.String(), access.RoleAdmin); roleErr != nil {
			return oops.Code("ADMIN_BOOTSTRAP_FAILED").With("operation", "assign admin role").Wrap(roleErr)
		}
		return nil
	}

	if deps.Transactor != nil {
		if err := deps.Transactor.WithTx(ctx, writesFn); err != nil {
			return oops.Code("ADMIN_BOOTSTRAP_FAILED").With("operation", "transaction").Wrap(err)
		}
	} else {
		if err := writesFn(ctx); err != nil {
			return err
		}
	}

	slog.Info("admin account created", //nolint:gosec // values from env vars or generated, not untrusted user input
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
