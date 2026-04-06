// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
)

// SchemaProvisioner creates schema-isolated Postgres environments for binary
// plugins that declare storage: postgres in their manifest.
type SchemaProvisioner struct {
	baseConnString string
	pool           *pgxpool.Pool
}

// NewSchemaProvisioner creates a SchemaProvisioner that will use the given
// base connection string to manage plugin schemas.
func NewSchemaProvisioner(baseConnString string) *SchemaProvisioner {
	return &SchemaProvisioner{baseConnString: baseConnString}
}

// Init opens the admin connection pool used for DDL operations.
func (sp *SchemaProvisioner) Init(ctx context.Context) error {
	pool, err := pgxpool.New(ctx, sp.baseConnString)
	if err != nil {
		return oops.Code("SCHEMA_POOL_INIT_FAILED").Wrap(err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return oops.Code("SCHEMA_POOL_PING_FAILED").Wrap(err)
	}
	sp.pool = pool
	return nil
}

// ProvisionSchema creates a plugin_<name> schema and returns a connection
// string scoped to that schema via search_path.
func (sp *SchemaProvisioner) ProvisionSchema(ctx context.Context, pluginName string) (string, error) {
	schemaName := pluginSchemaName(pluginName)

	identifier := pgx.Identifier{schemaName}
	ddl := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", identifier.Sanitize())
	if _, err := sp.pool.Exec(ctx, ddl); err != nil {
		return "", oops.Code("SCHEMA_CREATE_FAILED").
			With("plugin", pluginName).
			With("schema", schemaName).
			Wrap(err)
	}

	slog.Info("provisioned plugin schema", "plugin", pluginName, "schema", schemaName)

	roleName := pluginRoleName(pluginName)
	password, err := generatePassword()
	if err != nil {
		return "", oops.Code("SCHEMA_CONNSTRING_FAILED").
			With("plugin", pluginName).
			Wrap(err)
	}
	connStr, err := pluginConnString(sp.baseConnString, schemaName, roleName, password)
	if err != nil {
		return "", oops.Code("SCHEMA_CONNSTRING_FAILED").
			With("plugin", pluginName).
			Wrap(err)
	}
	return connStr, nil
}

// Close shuts down the admin connection pool.
func (sp *SchemaProvisioner) Close() {
	if sp.pool != nil {
		sp.pool.Close()
	}
}

// pluginSchemaName converts a plugin name to a Postgres schema name.
// Hyphens become underscores; the prefix "plugin_" is prepended.
func pluginSchemaName(name string) string {
	return "plugin_" + strings.ReplaceAll(name, "-", "_")
}

// pluginRoleName converts a plugin name to a PostgreSQL role name.
// Uses the same sanitization as pluginSchemaName but with the
// "holomush_plugin_" prefix for role namespace isolation.
func pluginRoleName(name string) string {
	return "holomush_plugin_" + strings.ReplaceAll(name, "-", "_")
}

// generatePassword returns 32 bytes (256 bits) of cryptographic randomness,
// base64url-encoded without padding. The charset (A-Za-z0-9-_) is safe for
// inclusion in PostgreSQL password literals without escaping.
func generatePassword() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", oops.Code("PASSWORD_GENERATION_FAILED").Wrap(err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// pluginConnString builds a connection string with plugin-specific credentials
// and search_path. Replaces user, password, and search_path in the base URL.
func pluginConnString(baseConnString, schemaName, roleName, password string) (string, error) {
	u, err := url.Parse(baseConnString)
	if err != nil {
		return "", oops.Code("SCHEMA_CONNSTRING_PARSE_FAILED").Wrap(err)
	}
	u.User = url.UserPassword(roleName, password)
	q := u.Query()
	q.Set("search_path", schemaName)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
