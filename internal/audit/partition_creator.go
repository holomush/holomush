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
	for _, t := range partitionMonths(time.Now().UTC(), months) {
		name, start, end := partitionRange(t)

		// access_audit_log.timestamp is BIGINT epoch-ns (post-gfo6 Phase 4).
		// Partition bounds are int64 ns; range_end is exclusive.
		query := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF access_audit_log FOR VALUES FROM (%d) TO (%d)`,
			name,
			start.UnixNano(),
			end.UnixNano(),
		)

		if _, err := c.pool.Exec(ctx, query); err != nil {
			return oops.
				With("partition", name).
				With("range_start_ns", start.UnixNano()).
				With("range_end_ns", end.UnixNano()).
				Errorf("creating partition: %w", err)
		}
	}
	return nil
}

// partitionMonths returns the first day of each month to partition: the month
// containing now, then months-1 following months.
func partitionMonths(now time.Time, months int) []time.Time {
	// Step from day 1: AddDate normalizes an overflowed day (Oct 31 + 1 month
	// is Dec 1), which would skip a month on the 29th to 31st.
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	out := make([]time.Time, 0, max(months, 0))
	for i := 0; i < months; i++ {
		out = append(out, first.AddDate(0, i, 0))
	}
	return out
}

// partitionRange returns the partition name and date boundaries for the month
// containing t. Start is inclusive, end is exclusive (first day of next month).
func partitionRange(t time.Time) (name string, start, end time.Time) {
	start = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	end = start.AddDate(0, 1, 0)
	name = fmt.Sprintf("access_audit_log_%04d_%02d", t.Year(), t.Month())
	return name, start, end
}
