// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
)

// PostgresPartitionCreator creates monthly audit log partitions.
// It implements policy.BootstrapPartitionCreator.
type PostgresPartitionCreator struct {
	pool *pgxpool.Pool
}

// NewPostgresPartitionCreator creates a partition creator backed by the given pool.
func NewPostgresPartitionCreator(pool *pgxpool.Pool) *PostgresPartitionCreator {
	return &PostgresPartitionCreator{pool: pool}
}

// EnsurePartitions creates monthly partitions for the current month plus the
// specified number of future months. Uses IF NOT EXISTS for idempotency.
// Partition naming follows spec convention: access_audit_log_YYYY_MM.
func (c *PostgresPartitionCreator) EnsurePartitions(ctx context.Context, months int) error {
	now := time.Now().UTC()
	for i := 0; i < months; i++ {
		t := now.AddDate(0, i, 0)
		name, start, end := partitionRange(t)

		query := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF access_audit_log FOR VALUES FROM ('%s') TO ('%s')`,
			name,
			start.Format("2006-01-02"),
			end.Format("2006-01-02"),
		)

		if _, err := c.pool.Exec(ctx, query); err != nil {
			return oops.
				With("partition", name).
				With("range_start", start.Format("2006-01-02")).
				With("range_end", end.Format("2006-01-02")).
				Errorf("creating partition: %w", err)
		}
	}
	return nil
}

// partitionRange returns the partition name and date boundaries for the month
// containing t. Start is inclusive, end is exclusive (first day of next month).
func partitionRange(t time.Time) (name string, start, end time.Time) {
	start = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	end = start.AddDate(0, 1, 0)
	name = fmt.Sprintf("access_audit_log_%04d_%02d", t.Year(), t.Month())
	return name, start, end
}
