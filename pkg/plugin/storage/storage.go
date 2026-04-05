// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package storage provides database utilities for binary plugins that
// declare storage: postgres in their manifest.
package storage

import (
	"context"
	"embed"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
)

// Connect opens a connection pool to the plugin's schema-isolated database.
func Connect(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, oops.Code("PLUGIN_DB_CONNECT_FAILED").Wrap(err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, oops.Code("PLUGIN_DB_PING_FAILED").Wrap(err)
	}
	return pool, nil
}

// RunMigrations runs embedded SQL migrations against the plugin's schema.
// Migrations MUST be named sequentially: 000001_name.up.sql, 000002_name.up.sql.
// Only .up.sql files are executed. Tracks applied migrations in plugin_migrations table.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, migrations embed.FS) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS plugin_migrations (
			version INTEGER PRIMARY KEY,
			name    TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return oops.Code("PLUGIN_MIGRATION_TABLE_FAILED").Wrap(err)
	}

	var currentVersion int
	err = pool.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM plugin_migrations").Scan(&currentVersion)
	if err != nil {
		return oops.Code("PLUGIN_MIGRATION_VERSION_FAILED").Wrap(err)
	}

	entries, err := migrations.ReadDir(".")
	if err != nil {
		return oops.Code("PLUGIN_MIGRATION_READ_FAILED").Wrap(err)
	}

	var upFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			upFiles = append(upFiles, e.Name())
		}
	}
	sort.Strings(upFiles)

	for _, name := range upFiles {
		version := parseMigrationVersion(name)
		if version <= currentVersion {
			continue
		}
		sql, readErr := migrations.ReadFile(name)
		if readErr != nil {
			return oops.Code("PLUGIN_MIGRATION_READ_FAILED").With("file", name).Wrap(readErr)
		}
		if _, execErr := pool.Exec(ctx, string(sql)); execErr != nil {
			return oops.Code("PLUGIN_MIGRATION_EXEC_FAILED").With("file", name).Wrap(execErr)
		}
		if _, trackErr := pool.Exec(ctx,
			"INSERT INTO plugin_migrations (version, name) VALUES ($1, $2)",
			version, name,
		); trackErr != nil {
			return oops.Code("PLUGIN_MIGRATION_TRACK_FAILED").With("file", name).Wrap(trackErr)
		}
	}
	return nil
}

// ParseSchemaFromConnString extracts the schema name from a connection
// string's search_path parameter.
func ParseSchemaFromConnString(connString string) (string, error) {
	u, err := url.Parse(connString)
	if err != nil {
		return "", oops.Code("PLUGIN_CONNSTRING_PARSE_FAILED").Wrap(err)
	}
	sp := u.Query().Get("search_path")
	if sp == "" {
		return "", oops.Code("PLUGIN_MISSING_SEARCH_PATH").
			Errorf("connection string missing search_path parameter")
	}
	return sp, nil
}

func parseMigrationVersion(name string) int {
	parts := strings.SplitN(name, "_", 2)
	if len(parts) == 0 {
		return 0
	}
	var v int
	_, _ = fmt.Sscanf(parts[0], "%d", &v) //nolint:errcheck // parse failure leaves v=0, which is the desired fallback
	return v
}
