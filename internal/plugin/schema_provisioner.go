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

// Init opens the admin connection pool used for DDL operations and validates
// that the connected role has the CREATEROLE privilege required for
// per-plugin role provisioning.
func (sp *SchemaProvisioner) Init(ctx context.Context) error {
	pool, err := pgxpool.New(ctx, sp.baseConnString)
	if err != nil {
		return oops.Code("SCHEMA_POOL_INIT_FAILED").Wrap(err)
	}
	err = pool.Ping(ctx)
	if err != nil {
		pool.Close()
		return oops.Code("SCHEMA_POOL_PING_FAILED").Wrap(err)
	}

	var currentUser string
	var hasCreaterole bool
	err = pool.QueryRow(ctx,
		"SELECT current_user, rolcreaterole FROM pg_roles WHERE rolname = current_user",
	).Scan(&currentUser, &hasCreaterole)
	if err != nil {
		pool.Close()
		return oops.Code("SCHEMA_ROLE_NOT_FOUND").
			Wrap(fmt.Errorf("cannot query current role privileges: %w", err))
	}
	if !hasCreaterole {
		pool.Close()
		return oops.Code("SCHEMA_INSUFFICIENT_PRIVILEGES").
			Errorf("server database role %q lacks CREATEROLE privilege; run: ALTER ROLE %s CREATEROLE",
				currentUser, currentUser)
	}

	slog.Info("schema provisioner initialized", "role", currentUser, "createrole", true)

	sp.pool = pool
	return nil
}

// ProvisionSchema creates a per-plugin PostgreSQL role and schema with
// full isolation. The plugin role:
//   - Has LOGIN (can connect)
//   - Has no SUPERUSER or CREATEROLE privileges
//   - Owns its schema (can CREATE tables)
//   - Has REVOKE ALL on public schema (cannot read server data)
//
// Returns a connection string scoped to the plugin's schema.
func (sp *SchemaProvisioner) ProvisionSchema(ctx context.Context, pluginName string) (string, error) {
	schemaName := pluginSchemaName(pluginName)
	roleName := pluginRoleName(pluginName)

	password, err := generatePassword()
	if err != nil {
		return "", oops.Code("SCHEMA_PASSWORD_FAILED").
			With("plugin", pluginName).Wrap(err)
	}

	err = sp.ensureRole(ctx, roleName, password)
	if err != nil {
		return "", oops.Code("SCHEMA_ROLE_FAILED").
			With("plugin", pluginName).
			With("role", roleName).Wrap(err)
	}

	err = sp.execDDL(ctx, pluginName, schemaName, roleName)
	if err != nil {
		return "", err
	}

	slog.Info("provisioned plugin schema with isolated role",
		"plugin", pluginName, "schema", schemaName, "role", roleName)

	connStr, err := pluginConnString(sp.baseConnString, schemaName, roleName, password)
	if err != nil {
		return "", oops.Code("SCHEMA_CONNSTRING_FAILED").
			With("plugin", pluginName).Wrap(err)
	}
	return connStr, nil
}

// execDDL runs the schema creation, ownership, grant, and revoke statements.
func (sp *SchemaProvisioner) execDDL(ctx context.Context, pluginName, schemaName, roleName string) error {
	schemaID := pgx.Identifier{schemaName}
	roleID := pgx.Identifier{roleName}

	ddl := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schemaID.Sanitize())
	if _, err := sp.pool.Exec(ctx, ddl); err != nil {
		return oops.Code("SCHEMA_CREATE_FAILED").
			With("plugin", pluginName).
			With("schema", schemaName).Wrap(err)
	}

	ownerDDL := fmt.Sprintf("ALTER SCHEMA %s OWNER TO %s", schemaID.Sanitize(), roleID.Sanitize())
	if _, err := sp.pool.Exec(ctx, ownerDDL); err != nil {
		return oops.Code("SCHEMA_OWNER_FAILED").
			With("plugin", pluginName).Wrap(err)
	}

	grantDDL := fmt.Sprintf("GRANT USAGE, CREATE ON SCHEMA %s TO %s",
		schemaID.Sanitize(), roleID.Sanitize())
	if _, err := sp.pool.Exec(ctx, grantDDL); err != nil {
		return oops.Code("SCHEMA_GRANT_FAILED").
			With("plugin", pluginName).Wrap(err)
	}

	revokeDDL := fmt.Sprintf("REVOKE ALL ON SCHEMA public FROM %s", roleID.Sanitize())
	if _, err := sp.pool.Exec(ctx, revokeDDL); err != nil {
		return oops.Code("SCHEMA_REVOKE_FAILED").
			With("plugin", pluginName).Wrap(err)
	}

	return nil
}

// PurgeSchema drops all objects owned by the plugin role and then drops
// the role itself. This permanently destroys the plugin's data.
func (sp *SchemaProvisioner) PurgeSchema(ctx context.Context, pluginName string) error {
	roleName := pluginRoleName(pluginName)
	roleID := pgx.Identifier{roleName}

	dropOwned := fmt.Sprintf("DROP OWNED BY %s", roleID.Sanitize())
	if _, err := sp.pool.Exec(ctx, dropOwned); err != nil {
		return oops.Code("SCHEMA_DROP_OWNED_FAILED").
			With("plugin", pluginName).
			With("role", roleName).Wrap(err)
	}

	dropRole := fmt.Sprintf("DROP ROLE IF EXISTS %s", roleID.Sanitize())
	if _, err := sp.pool.Exec(ctx, dropRole); err != nil {
		return oops.Code("SCHEMA_DROP_ROLE_FAILED").
			With("plugin", pluginName).
			With("role", roleName).Wrap(err)
	}

	slog.Info("purged plugin schema and role", "plugin", pluginName, "role", roleName)
	return nil
}

// ensureRole creates the plugin role if it doesn't exist, or refreshes
// the ephemeral password if it does. After creation, grants the new role
// to the current user so that ALTER SCHEMA OWNER TO succeeds on
// PostgreSQL 16+ (where CREATEROLE no longer implies membership).
func (sp *SchemaProvisioner) ensureRole(ctx context.Context, roleName, password string) error {
	roleID := pgx.Identifier{roleName}

	var exists bool
	err := sp.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)",
		roleName,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check role existence: %w", err)
	}

	if exists {
		ddl := fmt.Sprintf("ALTER ROLE %s PASSWORD '%s'", roleID.Sanitize(), password)
		if _, err := sp.pool.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("refresh role password: %w", err)
		}
	} else {
		ddl := fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s'", roleID.Sanitize(), password)
		if _, err := sp.pool.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("create role: %w", err)
		}

		// Grant the new role to the admin user so ALTER SCHEMA OWNER TO
		// works (PG 16+ requires explicit membership).
		var currentUser string
		if err := sp.pool.QueryRow(ctx, "SELECT current_user").Scan(&currentUser); err != nil {
			return fmt.Errorf("query current user: %w", err)
		}
		grantDDL := fmt.Sprintf("GRANT %s TO %s",
			roleID.Sanitize(), pgx.Identifier{currentUser}.Sanitize())
		if _, err := sp.pool.Exec(ctx, grantDDL); err != nil {
			return fmt.Errorf("grant role to admin: %w", err)
		}
	}
	return nil
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
