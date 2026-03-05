// Package database handles PostgreSQL connection pooling and auto database creation.
package database

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"evmwalletbot/config"
)

// EnsureDatabase connects to the always-existing "postgres" maintenance database,
// checks whether the target database (cfg.DBName) exists, and creates it if not.
//
// This must be called BEFORE Connect() so the program never crashes on first run
// with "database does not exist".
func EnsureDatabase(cfg *config.Config) error {
	// Connect to the built-in "postgres" system DB — it always exists.
	mainDSN := buildDSN(cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, "postgres", cfg.DBSSLMode)

	db, err := sql.Open("postgres", mainDSN)
	if err != nil {
		return fmt.Errorf("open maintenance connection: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("cannot reach PostgreSQL server (%s:%d): %w", cfg.DBHost, cfg.DBPort, err)
	}

	// Check whether our target database already exists.
	var exists bool
	err = db.QueryRow(
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
	// The double-quoted identifier handles DB names that contain upper-case or special chars.
	log.Printf("[INFO] Database '%s' not found — creating it now...\n", cfg.DBName)
	_, err = db.Exec(fmt.Sprintf(`CREATE DATABASE "%s"`, cfg.DBName))
	if err != nil {
		return fmt.Errorf("create database '%s': %w", cfg.DBName, err)
	}

	log.Printf("[INFO] Database '%s' created successfully\n", cfg.DBName)
	return nil
}

// Connect opens and validates a PostgreSQL connection pool to cfg.DBName.
// Always call EnsureDatabase() first.
func Connect(cfg *config.Config) (*sql.DB, error) {
	db, err := sql.Open("postgres", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}

	// Pool tuning for high-throughput batch inserts on Azure VPS.
	db.SetMaxOpenConns(30)
	db.SetMaxIdleConns(15)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping '%s': %w", cfg.DBName, err)
	}

	return db, nil
}

// buildDSN constructs a properly-quoted lib/pq keyword=value connection string.
// Values that contain spaces, single-quotes, or backslashes are escaped correctly
// so a complex password never corrupts the DSN.
func buildDSN(host string, port int, user, password, dbname, sslmode string) string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, port,
		quoteDSNValue(user),
		quoteDSNValue(password),
		quoteDSNValue(dbname),
		sslmode,
	)
}

// quoteDSNValue wraps a DSN value in single quotes and escapes embedded
// single-quotes and backslashes per the libpq connection string spec.
func quoteDSNValue(v string) string {
	if v == "" {
		return "''"
	}
	// No special chars? Return bare value (most common case).
	if !strings.ContainsAny(v, " '\\") {
		return v
	}
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `'`, `\'`)
	return "'" + v + "'"
}
