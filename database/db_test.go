package database

import (
	"testing"

	"evmwalletbot/config"
)

// TestValidateDatabaseName verifies database name validation
func TestValidateDatabaseName(t *testing.T) {
	tests := []struct {
		name    string
		dbName  string
		wantErr bool
	}{
		{
			name:    "valid simple name",
			dbName:  "walletdb",
			wantErr: false,
		},
		{
			name:    "valid with underscore",
			dbName:  "wallet_db",
			wantErr: false,
		},
		{
			name:    "valid with numbers",
			dbName:  "wallet_db_123",
			wantErr: false,
		},
		{
			name:    "valid starting with underscore",
			dbName:  "_walletdb",
			wantErr: false,
		},
		{
			name:    "valid uppercase",
			dbName:  "WalletDB",
			wantErr: false,
		},
		{
			name:    "empty name",
			dbName:  "",
			wantErr: true,
		},
		{
			name:    "too long (64 chars)",
			dbName:  "a123456789012345678901234567890123456789012345678901234567890123",
			wantErr: true,
		},
		{
			name:    "max valid length (63 chars)",
			dbName:  "a12345678901234567890123456789012345678901234567890123456789012",
			wantErr: false,
		},
		{
			name:    "starts with number",
			dbName:  "1walletdb",
			wantErr: true,
		},
		{
			name:    "contains hyphen",
			dbName:  "wallet-db",
			wantErr: true,
		},
		{
			name:    "contains space",
			dbName:  "wallet db",
			wantErr: true,
		},
		{
			name:    "contains special char",
			dbName:  "wallet@db",
			wantErr: true,
		},
		{
			name:    "contains dot",
			dbName:  "wallet.db",
			wantErr: true,
		},
		{
			name:    "SQL injection attempt",
			dbName:  "wallet'; DROP TABLE users--",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDatabaseName(tt.dbName)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateDatabaseName(%q) error = %v, wantErr %v", tt.dbName, err, tt.wantErr)
			}
		})
	}
}

// TestBuildDSN verifies DSN construction
func TestBuildDSN(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     int
		user     string
		password string
		dbname   string
		sslmode  string
		want     string
	}{
		{
			name:     "basic DSN",
			host:     "localhost",
			port:     5432,
			user:     "postgres",
			password: "secret",
			dbname:   "walletdb",
			sslmode:  "disable",
			want:     "host=localhost port=5432 user=postgres password=secret dbname=walletdb sslmode=disable",
		},
		{
			name:     "remote host",
			host:     "db.example.com",
			port:     5433,
			user:     "admin",
			password: "pass123",
			dbname:   "production",
			sslmode:  "require",
			want:     "host=db.example.com port=5433 user=admin password=pass123 dbname=production sslmode=require",
		},
		{
			name:     "empty password",
			host:     "localhost",
			port:     5432,
			user:     "postgres",
			password: "",
			dbname:   "testdb",
			sslmode:  "disable",
			want:     "host=localhost port=5432 user=postgres password= dbname=testdb sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDSN(tt.host, tt.port, tt.user, tt.password, tt.dbname, tt.sslmode)
			if got != tt.want {
				t.Errorf("buildDSN() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestConfigDSN verifies Config.DSN() method
func TestConfigDSN(t *testing.T) {
	cfg := &config.Config{
		DBHost:     "localhost",
		DBPort:     5432,
		DBUser:     "testuser",
		DBPassword: "testpass",
		DBName:     "testdb",
		DBSSLMode:  "disable",
	}

	dsn := cfg.DSN()
	expected := "host=localhost port=5432 user=testuser password=testpass dbname=testdb sslmode=disable"
	
	if dsn != expected {
		t.Errorf("Config.DSN() = %q, want %q", dsn, expected)
	}
}

// BenchmarkValidateDatabaseName measures validation performance
func BenchmarkValidateDatabaseName(b *testing.B) {
	validName := "wallet_db_123"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateDatabaseName(validName)
	}
}

// BenchmarkBuildDSN measures DSN construction performance
func BenchmarkBuildDSN(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildDSN("localhost", 5432, "postgres", "secret", "walletdb", "disable")
	}
}
