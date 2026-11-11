// Package testdb gives each test a freshly migrated PostgreSQL schema.
//
// Set FINANCE_TEST_DATABASE_URL to a PostgreSQL URL the tests may create
// schemas in (dev only, e.g.
// postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable).
// Without it, database tests skip themselves.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/AGlElectronics/finance/backend/migrations"
)

// Env is the variable that holds the test database URL.
const Env = "FINANCE_TEST_DATABASE_URL"

// New creates a schema, applies every migration in it and returns a pool
// whose search_path points there. The schema is dropped when the test ends.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv(Env)
	if base == "" {
		t.Skip(Env + " not set")
	}
	ctx := context.Background()
	schema := "finance_test_" + strings.ReplaceAll(uuid.NewString()[:13], "-", "")

	admin, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatalf("testdb: connect: %v", err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		admin.Close()
		t.Fatalf("testdb: create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		admin.Close()
	})

	dsn, err := withSearchPath(base, schema)
	if err != nil {
		t.Fatalf("testdb: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("testdb: open: %v", err)
	}
	defer func() { _ = db.Close() }()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("testdb: %v", err)
	}
	if err := goose.UpContext(ctx, db, "."); err != nil {
		t.Fatalf("testdb: migrate: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("testdb: pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func withSearchPath(dsn, schema string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" {
		return "", fmt.Errorf("%s must be a postgres:// URL", Env)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
