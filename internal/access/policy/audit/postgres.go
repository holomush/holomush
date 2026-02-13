// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/samber/oops"
)

// PostgresWriter implements Writer for PostgreSQL.
type PostgresWriter struct {
	db          *sql.DB
	asyncChan   chan Entry
	stopChan    chan struct{}
	wg          sync.WaitGroup
	batchSize   int
	flushPeriod time.Duration
}

// NewPostgresWriter creates a PostgresWriter with the given database connection.
func NewPostgresWriter(db *sql.DB) *PostgresWriter {
	writer := &PostgresWriter{
		db:          db,
		asyncChan:   make(chan Entry, 1000),
		stopChan:    make(chan struct{}),
		batchSize:   100,
		flushPeriod: 1 * time.Second,
	}

	// Start batch consumer goroutine
	writer.wg.Add(1)
	go writer.batchConsumer()

	return writer
}

// WriteSync performs a synchronous write to the database.
func (w *PostgresWriter) WriteSync(ctx context.Context, entry Entry) error {
	query := `
		INSERT INTO access_audit_log (
			subject, action, resource, effect, policy_id, policy_name,
			attributes, duration_us, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	attributesJSON, err := json.Marshal(entry.Attributes)
	if err != nil {
		return oops.Wrap(err)
	}

	_, err = w.db.ExecContext(ctx, query,
		entry.Subject,
		entry.Action,
		entry.Resource,
		entry.Effect.String(),
		entry.PolicyID,
		entry.PolicyName,
		attributesJSON,
		entry.DurationUS,
		entry.Timestamp,
	)
	if err != nil {
		return oops.With("subject", entry.Subject).
			With("action", entry.Action).
			With("resource", entry.Resource).
			Wrap(err)
	}

	return nil
}

// WriteAsync queues an entry for asynchronous batch writing.
func (w *PostgresWriter) WriteAsync(entry Entry) error {
	select {
	case w.asyncChan <- entry:
		return nil
	default:
		// Channel full - caller should handle
		channelFullCounter.Inc()
		return fmt.Errorf("async channel full")
	}
}

// batchConsumer processes async writes in batches.
func (w *PostgresWriter) batchConsumer() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.flushPeriod)
	defer ticker.Stop()

	var batch []Entry

	flush := func() {
		if len(batch) == 0 {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := w.writeBatch(ctx, batch); err != nil {
			slog.Error("failed to write audit batch", "error", err, "count", len(batch))
			failuresCounter.WithLabelValues("batch_write_failed").Inc()
		}

		batch = batch[:0] // reset batch
	}

	for {
		select {
		case entry := <-w.asyncChan:
			batch = append(batch, entry)
			if len(batch) >= w.batchSize {
				flush()
			}

		case <-ticker.C:
			flush()

		case <-w.stopChan:
			// Drain remaining entries
			for {
				select {
				case entry := <-w.asyncChan:
					batch = append(batch, entry)
				default:
					flush()
					return
				}
			}
		}
	}
}

// writeBatch writes multiple entries in a single transaction.
func (w *PostgresWriter) writeBatch(ctx context.Context, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return oops.Wrap(err)
	}
	defer func() {
		//nolint:errcheck // Rollback error is expected when transaction commits successfully
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO access_audit_log (
			subject, action, resource, effect, policy_id, policy_name,
			attributes, duration_us, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`)
	if err != nil {
		return oops.Wrap(err)
	}
	defer func() {
		//nolint:errcheck // Close error is not critical - statement will be closed when transaction ends
		_ = stmt.Close()
	}()

	for i := range entries {
		entry := &entries[i]
		attributesJSON, err := json.Marshal(entry.Attributes)
		if err != nil {
			slog.Error("failed to marshal attributes", "error", err, "entry", entry)
			continue
		}

		_, err = stmt.ExecContext(ctx,
			entry.Subject,
			entry.Action,
			entry.Resource,
			entry.Effect.String(),
			entry.PolicyID,
			entry.PolicyName,
			attributesJSON,
			entry.DurationUS,
			entry.Timestamp,
		)
		if err != nil {
			slog.Error("failed to insert audit entry", "error", err, "entry", entry)
			// Continue with other entries
		}
	}

	if err := tx.Commit(); err != nil {
		return oops.Wrap(err)
	}

	return nil
}

// Close gracefully shuts down the writer.
func (w *PostgresWriter) Close() error {
	// Signal stop
	close(w.stopChan)

	// Wait for batch consumer to drain
	w.wg.Wait()

	return nil
}
