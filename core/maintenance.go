// Package core — database health monitoring and maintenance.
package core

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"evmwalletbot/storage"
	"evmwalletbot/storage/postgres"
)

// HealthMetrics represents health statistics for a database table.
type HealthMetrics struct {
	TableName      string
	TotalSize      int64
	IndexSize      int64
	LiveTuples     int64
	DeadTuples     int64
	LastVacuum     *time.Time
	LastAutovacuum *time.Time
	BloatPercent   float64
}

// RunHealthCheck collects metrics, displays them, and records to database when supported.
func RunHealthCheck(ctx context.Context, store storage.Storage) error {
	if err := store.HealthCheck(ctx); err != nil {
		return fmt.Errorf("storage health check: %w", err)
	}

	pgStore, ok := store.(*postgres.PostgresStorage)
	if !ok {
		stats, err := store.GetStats(ctx)
		if err != nil {
			return fmt.Errorf("load stats: %w", err)
		}
		fmt.Printf(`
  ┌──────────────────────────────────────────────────────┐
  │                STORAGE HEALTH                        │
  ├──────────────────────────────────────────────────────┤
  │  Backend          : %-33s │
  │  Status           : %-33s │
  │  Total wallets    : %-33d │
  │  Storage size     : %-33s │
  └──────────────────────────────────────────────────────┘
`,
			store.StorageType(),
			"healthy",
			stats.TotalWallets,
			FormatBytes(stats.DBSizeBytes),
		)
		log.Println("[INFO] Health check complete")
		return nil
	}

	log.Println("[INFO] Collecting database health metrics...")
	metrics, err := CollectHealthMetrics(ctx, pgStore.Pool())
	if err != nil {
		return fmt.Errorf("collect metrics: %w", err)
	}

	PrintHealthMetrics(metrics)

	log.Println("[INFO] Recording metrics to database_health table...")
	if err := RecordHealthMetrics(ctx, pgStore.Pool(), metrics); err != nil {
		return fmt.Errorf("record metrics: %w", err)
	}

	log.Println("[INFO] Health check complete")
	return nil
}

// CollectHealthMetrics queries PostgreSQL system catalogs for table health data.
func CollectHealthMetrics(ctx context.Context, pool *pgxpool.Pool) ([]HealthMetrics, error) {
	query := `
		SELECT 
			schemaname || '.' || relname AS table_name,
			pg_total_relation_size(relid) AS total_size,
			pg_indexes_size(relid) AS index_size,
			n_live_tup AS live_tuples,
			n_dead_tup AS dead_tuples,
			last_vacuum,
			last_autovacuum
		FROM pg_stat_user_tables
		WHERE schemaname = 'public'
		ORDER BY pg_total_relation_size(relid) DESC
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query health metrics: %w", err)
	}
	defer rows.Close()

	var metrics []HealthMetrics
	for rows.Next() {
		var m HealthMetrics
		err := rows.Scan(
			&m.TableName,
			&m.TotalSize,
			&m.IndexSize,
			&m.LiveTuples,
			&m.DeadTuples,
			&m.LastVacuum,
			&m.LastAutovacuum,
		)
		if err != nil {
			return nil, fmt.Errorf("scan health metrics: %w", err)
		}

		if m.LiveTuples > 0 {
			m.BloatPercent = float64(m.DeadTuples) / float64(m.LiveTuples+m.DeadTuples) * 100
		}

		metrics = append(metrics, m)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return metrics, nil
}

// RecordHealthMetrics saves health metrics to database_health table for historical tracking.
func RecordHealthMetrics(ctx context.Context, pool *pgxpool.Pool, metrics []HealthMetrics) error {
	if len(metrics) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, m := range metrics {
		batch.Queue(`
			INSERT INTO database_health (
				table_name, total_size, index_size, 
				dead_tuples, live_tuples, 
				last_vacuum, last_autovacuum
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, m.TableName, m.TotalSize, m.IndexSize,
			m.DeadTuples, m.LiveTuples,
			m.LastVacuum, m.LastAutovacuum)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < len(metrics); i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("record health metrics batch: %w", err)
		}
	}

	return nil
}

// PrintHealthMetrics displays health metrics in a formatted table.
func PrintHealthMetrics(metrics []HealthMetrics) {
	if len(metrics) == 0 {
		fmt.Println("\n[INFO] No health metrics available")
		return
	}

	top := "  ╔═══════════════════════════════════════════════════════════════════════════════╗"
	line := "  ├───────────────────────────────────────────────────────────────────────────────┤"
	bot := "  ╚═══════════════════════════════════════════════════════════════════════════════╝"
	title := "  ║                          DATABASE HEALTH METRICS                             ║"

	fmt.Println()
	fmt.Println(top)
	fmt.Println(title)
	fmt.Println(line)

	for _, m := range metrics {
		fmt.Printf("  ║  Table: %-68s ║\n", m.TableName)
		fmt.Println(line)
		fmt.Printf("  ║    Total Size      : %-54s ║\n", FormatBytes(m.TotalSize))
		fmt.Printf("  ║    Index Size      : %-54s ║\n", FormatBytes(m.IndexSize))
		fmt.Printf("  ║    Data Size       : %-54s ║\n", FormatBytes(m.TotalSize-m.IndexSize))
		fmt.Printf("  ║    Live Tuples     : %-54d ║\n", m.LiveTuples)
		fmt.Printf("  ║    Dead Tuples     : %-54d ║\n", m.DeadTuples)

		bloatStatus := fmt.Sprintf("%.1f%%", m.BloatPercent)
		if m.BloatPercent > 20 {
			bloatStatus += " ⚠️  HIGH - Consider VACUUM"
		} else if m.BloatPercent > 10 {
			bloatStatus += " ⚡ MODERATE"
		} else {
			bloatStatus += " ✓ HEALTHY"
		}
		fmt.Printf("  ║    Bloat           : %-54s ║\n", bloatStatus)

		if m.LastVacuum != nil {
			fmt.Printf("  ║    Last VACUUM     : %-54s ║\n", m.LastVacuum.Format("2006-01-02 15:04:05"))
		} else {
			fmt.Printf("  ║    Last VACUUM     : %-54s ║\n", "Never")
		}

		if m.LastAutovacuum != nil {
			fmt.Printf("  ║    Last Autovacuum : %-54s ║\n", m.LastAutovacuum.Format("2006-01-02 15:04:05"))
		} else {
			fmt.Printf("  ║    Last Autovacuum : %-54s ║\n", "Never")
		}

		fmt.Println(line)
	}

	fmt.Println(bot)
	fmt.Println()

	needsVacuum := false
	for _, m := range metrics {
		if m.BloatPercent > 20 {
			needsVacuum = true
			break
		}
	}

	if needsVacuum {
		log.Println("[WARN] Some tables have high bloat (>20%). Consider running VACUUM ANALYZE.")
		log.Println("[INFO] To vacuum: psql -d <database> -c 'VACUUM ANALYZE;'")
	}
}
