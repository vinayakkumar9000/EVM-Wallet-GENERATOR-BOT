// Package database handles PostgreSQL connection pooling and auto database creation.
package database

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"evmwalletbot/config"
)

// ponytail: Connection pool lifecycle constants (avoid import cycle with core package)
const (
	MaxConnLifetime   = 5 * time.Minute
	MaxConnIdleTime   = 2 * time.Minute
	HealthCheckPeriod = 1 * time.Minute
)

// EnsureDatabase connects to the always-existing "postgres" maintenance database,
// checks whether the target database (cfg.DBName) exists, and creates it if not.
//
// This must be called BEFORE Connect() so the program never crashes on first run
// with "database does not exist".
func EnsureDatabase(cfg *config.Config) error {
	// Connect to the built-in "postgres" system DB — it always exists.
	mainDSN := buildDSN(cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, "postgres", cfg.DBSSLMode)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, mainDSN)
	if err != nil {
		return fmt.Errorf("open maintenance connection: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("cannot reach PostgreSQL server (%s:%d): %w", cfg.DBHost, cfg.DBPort, err)
	}

	// Check whether our target database already exists.
	var exists bool
	err = pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`,
		cfg.DBName,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check database existence: %w", err)
	}

	if exists {
		log.Printf("[INFO] Database '%s' already exists — will write wallets into it\n", cfg.DBName)
		return nil
	}

	// CREATE DATABASE must run outside any transaction block (PostgreSQL requirement).
	// ponytail: Validate database name to prevent SQL injection and ensure PostgreSQL compliance
	if err := validateDatabaseName(cfg.DBName); err != nil {
		return fmt.Errorf("invalid database name: %w", err)
	}
	
	log.Printf("[INFO] Database '%s' not found — creating it now...\n", cfg.DBName)
	_, err = pool.Exec(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, cfg.DBName))
	if err != nil {
		return fmt.Errorf("create database '%s': %w", cfg.DBName, err)
	}

	log.Printf("[INFO] Database '%s' created successfully\n", cfg.DBName)
	return nil
}

// Connect opens and validates a PostgreSQL connection pool to cfg.DBName.
// Always call EnsureDatabase() first.
func Connect(cfg *config.Config) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}

	// Pool tuning for high-throughput batch inserts.
	// ponytail: Now configurable via DB_MAX_CONNS and DB_MIN_CONNS environment variables
	poolConfig.MaxConns = int32(cfg.DBMaxConns)
	poolConfig.MinConns = int32(cfg.DBMinConns)
	poolConfig.MaxConnLifetime = MaxConnLifetime
	poolConfig.MaxConnIdleTime = MaxConnIdleTime
	poolConfig.HealthCheckPeriod = HealthCheckPeriod

	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping '%s': %w", cfg.DBName, err)
	}

	return pool, nil
}

// buildDSN constructs a pgx connection string.
// pgx uses standard PostgreSQL connection URIs or keyword=value format.
func buildDSN(host string, port int, user, password, dbname, sslmode string) string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode,
	)
}

// validateDatabaseName ensures the database name follows PostgreSQL identifier rules.
// ponytail: Strict validation prevents edge cases and improves error messages.
// PostgreSQL identifiers: alphanumeric + underscore, max 63 chars, must start with letter/underscore.
func validateDatabaseName(name string) error {
	if len(name) == 0 {
		return fmt.Errorf("database name cannot be empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("database name too long (max 63 chars): %s", name)
	}
	// PostgreSQL identifier rules: start with letter/underscore, then alphanumeric/underscore
	if !regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`).MatchString(name) {
		return fmt.Errorf("invalid database name '%s': must start with letter/underscore and contain only letters, numbers, and underscores", name)
	}
	return nil
}