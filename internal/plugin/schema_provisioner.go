// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

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

	ddl := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schemaName)
	if _, err := sp.pool.Exec(ctx, ddl); err != nil {
		return "", oops.Code("SCHEMA_CREATE_FAILED").
			With("plugin", pluginName).
			With("schema", schemaName).
			Wrap(err)
	}

	slog.Info("provisioned plugin schema", "plugin", pluginName, "schema", schemaName)

	connStr, err := scopedConnString(sp.baseConnString, schemaName)
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

// scopedConnString returns a copy of baseConnString with search_path set to
// the given schema name.
func scopedConnString(baseConnString, schemaName string) (string, error) {
	u, err := url.Parse(baseConnString)
	if err != nil {
		return "", oops.Code("SCHEMA_CONNSTRING_PARSE_FAILED").Wrap(err)
	}
	q := u.Query()
	q.Set("search_path", schemaName)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
