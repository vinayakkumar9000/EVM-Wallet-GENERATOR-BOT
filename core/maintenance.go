// Package core — database maintenance and health monitoring.
package core

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HealthMetrics represents database health information for a single table.
type HealthMetrics struct {
	TableName      string
	TotalSize      int64
	IndexSize      int64
	DeadTuples     int64
	LiveTuples     int64
	LastVacuum     *time.Time
	LastAutovacuum *time.Time
}

// CollectHealthMetrics gathers database health information for monitoring.
// ponytail: Uses PostgreSQL system catalogs (pg_stat_user_tables, pg_class).
// No new dependencies, just stdlib queries.
func CollectHealthMetrics(pool *pgxpool.Pool) ([]HealthMetrics, error) {
	ctx := context.Background()

	// Query health metrics from PostgreSQL system catalogs
	rows, err := pool.Query(ctx, `
		SELECT 
			schemaname || '.' || relname AS table_name,
			pg_total_relation_size(relid) AS total_size,
			pg_indexes_size(relid) AS index_size,
			n_dead_tup AS dead_tuples,
			n_live_tup AS live_tuples,
			last_vacuum,
			last_autovacuum
		FROM pg_stat_user_tables
		WHERE schemaname = 'public'
		ORDER BY pg_total_relation_size(relid) DESC
	`)
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
			&m.DeadTuples,
			&m.LiveTuples,
			&m.LastVacuum,
			&m.LastAutovacuum,
		)
		if err != nil {
			return nil, fmt.Errorf("scan health metrics: %w", err)
		}
		metrics = append(metrics, m)
	}

	return metrics, rows.Err()
}

// RecordHealthMetrics stores current health metrics in database_health table.
func RecordHealthMetrics(pool *pgxpool.Pool) error {
	metrics, err := CollectHealthMetrics(pool)
	if err != nil {
		return err
	}

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, m := range metrics {
		_, err := tx.Exec(ctx, `
			INSERT INTO database_health (
				table_name, total_size, index_size, 
				dead_tuples, live_tuples, 
				last_vacuum, last_autovacuum
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, m.TableName, m.TotalSize, m.IndexSize,
			m.DeadTuples, m.LiveTuples,
			m.LastVacuum, m.LastAutovacuum)
		if err != nil {
			return fmt.Errorf("insert health record: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// PrintHealthMetrics displays database health information.
func PrintHealthMetrics(metrics []HealthMetrics) {
	fmt.Println("\n  ╔═══════════════════════════════════════════════════════════════════════╗")
	fmt.Println("  ║                    DATABASE HEALTH METRICS                            ║")
	fmt.Println("  ╠═══════════════════════════════════════════════════════════════════════╣")

	for _, m := range metrics {
		fmt.Printf("  ║  Table: %-58s ║\n", m.TableName)
		fmt.Printf("  ║    Total Size    : %-47s ║\n", formatBytes(m.TotalSize))
		fmt.Printf("  ║    Index Size    : %-47s ║\n", formatBytes(m.IndexSize))
		fmt.Printf("  ║    Live Tuples   : %-47d ║\n", m.LiveTuples)
		fmt.Printf("  ║    Dead Tuples   : %-47d ║\n", m.DeadTuples)

		if m.DeadTuples > 0 && m.LiveTuples > 0 {
			bloatPct := float64(m.DeadTuples) / float64(m.LiveTuples+m.DeadTuples) * 100
			status := "OK"
			if bloatPct > 20 {
				status = "NEEDS VACUUM"
			}
			fmt.Printf("  ║    Bloat         : %.1f%% (%s)%*s ║\n", 
				bloatPct, status, 47-len(fmt.Sprintf("%.1f%% (%s)", bloatPct, status)), "")
		}

		if m.LastVacuum != nil {
			fmt.Printf("  ║    Last Vacuum   : %-47s ║\n", 
				m.LastVacuum.Format("2006-01-02 15:04:05"))
		}
		if m.LastAutovacuum != nil {
			fmt.Printf("  ║    Last Auto     : %-47s ║\n", 
				m.LastAutovacuum.Format("2006-01-02 15:04:05"))
		}
		fmt.Println("  ╠═══════════════════════════════════════════════════════════════════════╣")
	}

	fmt.Println("  ╚═══════════════════════════════════════════════════════════════════════╝\n")
}

// GetHealthSummary returns a quick health check summary.
func GetHealthSummary(pool *pgxpool.Pool) (string, error) {
	metrics, err := CollectHealthMetrics(pool)
	if err != nil {
		return "", err
	}

	var totalSize, totalDead, totalLive int64
	needsVacuum := 0

	for _, m := range metrics {
		totalSize += m.TotalSize
		totalDead += m.DeadTuples
		totalLive += m.LiveTuples

		if m.DeadTuples > 0 && m.LiveTuples > 0 {
			bloatPct := float64(m.DeadTuples) / float64(m.LiveTuples+m.DeadTuples) * 100
			if bloatPct > 20 {
				needsVacuum++
			}
		}
	}

	status := "HEALTHY"
	if needsVacuum > 0 {
		status = fmt.Sprintf("NEEDS ATTENTION (%d tables need vacuum)", needsVacuum)
	}

	return fmt.Sprintf("Total DB Size: %s | Dead Tuples: %d | Status: %s",
		formatBytes(totalSize), totalDead, status), nil
}
