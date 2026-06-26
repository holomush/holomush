// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package testutil provides shared test infrastructure for integration tests.
package testutil

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/holomush/holomush/internal/store"
)

// PostgresEnv holds a running PostgreSQL container with a non-superuser
// holomush role that has CREATEROLE privilege, matching production deployment.
type PostgresEnv struct {
	Container    testcontainers.Container
	ConnStr      string
	AdminConnStr string

	templateOnce sync.Once
	templateErr  error
}

var (
	sharedOnce   sync.Once
	sharedEnv    *PostgresEnv
	sharedErr    error
	templateName = "holomush_template"
)

// Terminate stops and removes the PostgreSQL container.
func (e *PostgresEnv) Terminate(ctx context.Context) error {
	if e.Container != nil {
		return fmt.Errorf("terminate container: %w", e.Container.Terminate(ctx))
	}
	return nil
}

// StartPostgres starts a dedicated PostgreSQL 18 container with a
// non-superuser holomush role (LOGIN, CREATEROLE). The returned
// ConnStr uses holomush:holomush credentials.
//
// Most tests should use SharedPostgres + FreshDatabase instead,
// which shares a single container per test binary and creates
// per-test databases via template copy. Use StartPostgres only
// when a test needs complete process-level container isolation.
//
// Callers are responsible for running migrations via store.NewMigrator.
func StartPostgres(ctx context.Context) (*PostgresEnv, error) {
	var lastErr error
	for attempt := range 3 {
		env, err := startPostgresOnce(ctx)
		if err == nil {
			return env, nil
		}
		lastErr = err
		if attempt == 2 {
			break
		}

		backoff := time.Duration(attempt+1) * 250 * time.Millisecond
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("start postgres container: %w", ctx.Err())
		case <-timer.C:
		}
	}

	return nil, fmt.Errorf("start postgres container: %w", lastErr)
}

func startPostgresOnce(ctx context.Context) (*PostgresEnv, error) {
	container, err := postgres.Run(
		ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("holomush_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		// Durability tuning for ephemeral test containers. The container is
		// torn down at process exit and never recovered, so crash-safety
		// (fsync, WAL full-page writes) and commit durability buy nothing —
		// they only slow down the once-per-binary template migration build and
		// every per-test write. Disabling them speeds startup and writes with
		// zero coverage/behavior impact. NEVER use these flags in production.
		testcontainers.WithCmd(
			"postgres",
			"-c", "fsync=off",
			"-c", "synchronous_commit=off",
			"-c", "full_page_writes=off",
		),
		testcontainers.WithWaitStrategyAndDeadline(
			2*time.Minute,
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			wait.ForListeningPort("5432/tcp"),
		),
	)
	if err != nil {
		// testcontainers-go returns a non-nil container handle when the
		// wait-until-ready strategy fails (generic.go: `return c, err`).
		// Reclaim it before returning so StartPostgres's retry does not pile
		// a fresh container on top of a leaked half-started one — the
		// resource pressure that causes the mapped-port timeout in the first
		// place (holomush-tmrv).
		if container != nil {
			_ = container.Terminate(ctx) //nolint:errcheck // best-effort cleanup
		}
		return nil, fmt.Errorf("run postgres: %w", err)
	}

	adminConnStr, err := adminConnectionString(ctx, container)
	if err != nil {
		_ = container.Terminate(ctx) //nolint:errcheck // best-effort cleanup
		return nil, fmt.Errorf("get admin connection string: %w", err)
	}

	err = initHolomushRole(ctx, adminConnStr)
	if err != nil {
		_ = container.Terminate(ctx) //nolint:errcheck // best-effort cleanup
		return nil, fmt.Errorf("init holomush role: %w", err)
	}

	connStr, err := replaceCredentials(adminConnStr, "holomush", "holomush")
	if err != nil {
		_ = container.Terminate(ctx) //nolint:errcheck // best-effort cleanup
		return nil, fmt.Errorf("build holomush connection string: %w", err)
	}

	return &PostgresEnv{Container: container, ConnStr: connStr, AdminConnStr: adminConnStr}, nil
}

func adminConnectionString(ctx context.Context, container *postgres.PostgresContainer) (string, error) {
	if connStr, err := container.ConnectionString(ctx, "sslmode=disable"); err == nil {
		return connStr, nil
	}

	host, err := container.Host(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve container host: %w", err)
	}

	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		port, err = container.MappedPort(ctx, "5432")
		if err != nil {
			return "", fmt.Errorf("resolve postgres port: %w", err)
		}
	}

	adminURL := &url.URL{
		Scheme: "postgres",
		Host:   fmt.Sprintf("%s:%s", host, port.Port()),
		Path:   "holomush_test",
	}
	adminURL.User = url.UserPassword("postgres", "postgres")
	q := adminURL.Query()
	q.Set("sslmode", "disable")
	adminURL.RawQuery = q.Encode()
	return adminURL.String(), nil
}

func initHolomushRole(ctx context.Context, adminConnStr string) error {
	conn, err := pgx.Connect(ctx, adminConnStr)
	if err != nil {
		return fmt.Errorf("connect as superuser: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }() //nolint:errcheck // best-effort cleanup

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
		return "", fmt.Errorf("parse connection string: %w", err)
	}
	u.User = url.UserPassword(user, password)
	return u.String(), nil
}

// SharedPostgres returns a process-wide singleton PostgresEnv. The container
// is started once and reused across all tests in the same test binary.
// The container is NOT terminated on cleanup — it lives for the process.
func SharedPostgres(t testing.TB) *PostgresEnv {
	t.Helper()
	sharedOnce.Do(func() {
		sharedEnv, sharedErr = StartPostgres(context.Background())
	})
	if sharedErr != nil {
		t.Fatalf("SharedPostgres: %v", sharedErr)
	}
	return sharedEnv
}

// FreshDatabase creates a new test database from a pre-migrated template.
// The template is created once per process via ensureTemplate. The returned
// connection string uses holomush credentials. The database is dropped on
// test cleanup.
func FreshDatabase(t testing.TB, env *PostgresEnv) string {
	t.Helper()
	ensureTemplate(t, env)

	dbName := randomDBName(t)
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, env.AdminConnStr)
	if err != nil {
		t.Fatalf("FreshDatabase: connect as superuser: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }() //nolint:errcheck // best-effort cleanup

	createSQL := ddlCreateFromTemplate(dbName, templateName)
	_, err = conn.Exec(ctx, createSQL)
	if err != nil {
		t.Fatalf("FreshDatabase: create database %s: %v", dbName, err)
	}

	t.Cleanup(func() {
		dropDatabase(t, env.AdminConnStr, dbName)
	})

	connStr, err := replaceDatabase(env.ConnStr, dbName)
	if err != nil {
		t.Fatalf("FreshDatabase: build connection string: %v", err)
	}
	return connStr
}

// RawDatabase creates a blank database with no migrations applied.
// The returned connection string uses postgres superuser credentials.
// The database is dropped on test cleanup.
func RawDatabase(t testing.TB, env *PostgresEnv) string {
	t.Helper()

	dbName := randomDBName(t)
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, env.AdminConnStr)
	if err != nil {
		t.Fatalf("RawDatabase: connect as superuser: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }() //nolint:errcheck // best-effort cleanup

	createSQL := ddlCreateDB(dbName)
	_, err = conn.Exec(ctx, createSQL)
	if err != nil {
		t.Fatalf("RawDatabase: create database %s: %v", dbName, err)
	}

	t.Cleanup(func() {
		dropDatabase(t, env.AdminConnStr, dbName)
	})

	connStr, err := replaceDatabase(env.AdminConnStr, dbName)
	if err != nil {
		t.Fatalf("RawDatabase: build connection string: %v", err)
	}
	return connStr
}

func ensureTemplate(t testing.TB, env *PostgresEnv) {
	t.Helper()
	env.templateOnce.Do(func() {
		ctx := context.Background()

		conn, err := pgx.Connect(ctx, env.AdminConnStr)
		if err != nil {
			env.templateErr = fmt.Errorf("connect as superuser: %w", err)
			return
		}
		defer func() { _ = conn.Close(ctx) }() //nolint:errcheck // best-effort cleanup

		createSQL := ddlCreateDB(templateName)
		_, err = conn.Exec(ctx, createSQL)
		if err != nil {
			env.templateErr = fmt.Errorf("create template database: %w", err)
			return
		}

		tmplConnStr, err := replaceDatabase(env.ConnStr, templateName)
		if err != nil {
			env.templateErr = fmt.Errorf("build template connection string: %w", err)
			return
		}

		migrator, migErr := store.NewMigrator(tmplConnStr)
		if migErr != nil {
			env.templateErr = fmt.Errorf("create migrator: %w", migErr)
			return
		}
		if migErr = migrator.Up(); migErr != nil {
			migrator.Close() //nolint:errcheck // best-effort cleanup
			env.templateErr = fmt.Errorf("run migrations: %w", migErr)
			return
		}
		migrator.Close() //nolint:errcheck // best-effort cleanup

		alterSQL := ddlMarkTemplate(templateName)
		_, err = conn.Exec(ctx, alterSQL)
		if err != nil {
			env.templateErr = fmt.Errorf("mark template: %w", err)
			return
		}
	})
	if env.templateErr != nil {
		t.Fatalf("ensureTemplate: %v", env.templateErr)
	}
}

// dropDatabase connects as superuser and force-drops the named database.
// Errors are logged, not fatal, since this runs in t.Cleanup.
func dropDatabase(t testing.TB, adminConnStr, dbName string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, adminConnStr)
	if err != nil {
		t.Logf("dropDatabase cleanup: connect: %v", err)
		return
	}
	defer func() { _ = conn.Close(ctx) }() //nolint:errcheck // best-effort cleanup
	dropSQL := ddlDropDB(dbName)
	_, err = conn.Exec(ctx, dropSQL)
	if err != nil {
		t.Logf("dropDatabase cleanup: drop %s: %v", dbName, err)
	}
}

// DDL builder functions. PostgreSQL DDL statements do not support parameterized
// queries ($1), so we build SQL strings here. All identifiers are either the
// constant templateName or hex-encoded crypto/rand output from randomDBName(),
// so there is no injection risk.

func ddlCreateDB(name string) string {
	return fmt.Sprintf("CREATE DATABASE %s OWNER holomush", name)
}

func ddlCreateFromTemplate(name, tmpl string) string {
	return fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s OWNER holomush", name, tmpl)
}

func ddlDropDB(name string) string {
	return fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", name)
}

func ddlMarkTemplate(name string) string {
	return fmt.Sprintf("ALTER DATABASE %s IS_TEMPLATE true", name)
}

func randomDBName(t testing.TB) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("randomDBName: crypto/rand failed: %v", err)
	}
	return fmt.Sprintf("test_%x", b)
}

func replaceDatabase(connStr, dbName string) (string, error) {
	u, err := url.Parse(connStr)
	if err != nil {
		return "", fmt.Errorf("parse connection string: %w", err)
	}
	u.Path = "/" + dbName
	return u.String(), nil
}
