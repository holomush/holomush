// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package testutil provides shared test infrastructure for integration tests.
package testutil

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// PostgresEnv holds a running PostgreSQL container with a non-superuser
// holomush role that has CREATEROLE privilege, matching production deployment.
type PostgresEnv struct {
	Container testcontainers.Container
	ConnStr   string
}

// Terminate stops and removes the PostgreSQL container.
func (e *PostgresEnv) Terminate(ctx context.Context) error {
	if e.Container != nil {
		return e.Container.Terminate(ctx)
	}
	return nil
}

// StartPostgres starts a PostgreSQL 18 container with a non-superuser
// holomush role (LOGIN, CREATEROLE). The returned ConnStr uses
// holomush:holomush credentials against the holomush_test database.
//
// Callers are responsible for running migrations via store.NewMigrator.
func StartPostgres(ctx context.Context) (*PostgresEnv, error) {
	container, err := postgres.Run(ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("holomush_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("start postgres container: %w", err)
	}

	adminConnStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("get admin connection string: %w", err)
	}

	if err := initHolomushRole(ctx, adminConnStr); err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("init holomush role: %w", err)
	}

	connStr, err := replaceCredentials(adminConnStr, "holomush", "holomush")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("build holomush connection string: %w", err)
	}

	return &PostgresEnv{Container: container, ConnStr: connStr}, nil
}

func initHolomushRole(ctx context.Context, adminConnStr string) error {
	conn, err := pgx.Connect(ctx, adminConnStr)
	if err != nil {
		return fmt.Errorf("connect as superuser: %w", err)
	}
	defer conn.Close(ctx)

	stmts := []string{
		"CREATE ROLE holomush LOGIN PASSWORD 'holomush' CREATEROLE",
		"ALTER SCHEMA public OWNER TO holomush",
		"GRANT ALL ON DATABASE holomush_test TO holomush",
	}
	for _, stmt := range stmts {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt, err)
		}
	}
	return nil
}

func replaceCredentials(connStr, user, password string) (string, error) {
	u, err := url.Parse(connStr)
	if err != nil {
		return "", err
	}
	u.User = url.UserPassword(user, password)
	return u.String(), nil
}
