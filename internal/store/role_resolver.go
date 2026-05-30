// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"log/slog"
	"strings"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/attribute"
)

// Compile-time check that PostgresRoleResolver satisfies attribute.RoleResolver.
var _ attribute.RoleResolver = (*PostgresRoleResolver)(nil)

// PostgresRoleResolver adapts a RoleStore into the attribute.RoleResolver interface.
type PostgresRoleResolver struct {
	store RoleStore
}

// NewPostgresRoleResolver creates a new resolver that looks up roles via a RoleStore.
func NewPostgresRoleResolver(store RoleStore) *PostgresRoleResolver {
	return &PostgresRoleResolver{store: store}
}

// GetRoles returns the roles for a subject. The subject ID arrives as "character:01ABC...";
// the character: prefix (access.SubjectCharacter) is stripped before querying the store.
func (r *PostgresRoleResolver) GetRoles(ctx context.Context, subject string) []string {
	charID := strings.TrimPrefix(subject, access.SubjectCharacter)
	roles, err := r.store.GetRoles(ctx, charID)
	if err != nil {
		slog.ErrorContext(ctx, "role resolution failed", "subject", subject, "error", err)
		return nil
	}
	return roles
}
