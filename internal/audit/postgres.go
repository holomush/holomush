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

	"github.com/holomush/holomush/internal/idgen"
)

// PostgresWriter implements Writer for PostgreSQL.
type PostgresWriter struct {
	db          *sql.DB
	asyncChan   chan Event
	stopChan    chan struct{}
	wg          sync.WaitGroup
	batchSize   int
	flushPeriod time.Duration
}

// NewPostgresWriter creates a PostgresWriter with the given database connection.
func NewPostgresWriter(db *sql.DB) *PostgresWriter {
	writer := &PostgresWriter{
		db:          db,
		asyncChan:   make(chan Event, 1000),
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
func (w *PostgresWriter) WriteSync(ctx context.Context, event Event) error {
	query := `
		INSERT INTO access_audit_log (
			id, subject, action, resource, effect, event_id, event_name,
			message, source, component, attributes, duration_us, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	attributesJSON, err := json.Marshal(event.Attributes)
	if err != nil {
		return oops.Wrap(err)
	}

	_, err = w.db.ExecContext(
		ctx, query,
		idgen.New().String(),
		event.Subject,
		event.Action,
		event.Resource,
		event.Effect.String(),
		event.ID,
		event.Name,
		event.Message,
		string(event.Source),
		event.Component,
		attributesJSON,
		event.DurationUS,
		event.Timestamp.UnixNano(), // pgnanos-exempt: SQL-cast boundary for BIGINT timestamp column
	)
	if err != nil {
		return oops.With("subject", event.Subject).
			With("action", event.Action).
			With("resource", event.Resource).
			Wrap(err)
	}

	return nil
}

// WriteAsync queues an event for asynchronous batch writing.
func (w *PostgresWriter) WriteAsync(event Event) error {
	select {
	case w.asyncChan <- event:
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

	var batch []Event

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
		case event := <-w.asyncChan:
			batch = append(batch, event)
			if len(batch) >= w.batchSize {
				flush()
			}

		case <-ticker.C:
			flush()

		case <-w.stopChan:
			// Drain remaining events
			for {
				select {
				case event := <-w.asyncChan:
					batch = append(batch, event)
				default:
					flush()
					return
				}
			}
		}
	}
}

// writeBatch writes multiple events in a single transaction.
func (w *PostgresWriter) writeBatch(ctx context.Context, events []Event) error {
	if len(events) == 0 {
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
			id, subject, action, resource, effect, event_id, event_name,
			message, source, component, attributes, duration_us, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`)
	if err != nil {
		return oops.Wrap(err)
	}
	defer func() {
		//nolint:errcheck // Close error is not critical - statement will be closed when transaction ends
		_ = stmt.Close()
	}()

	for i := range events {
		event := &events[i]
		attributesJSON, err := json.Marshal(event.Attributes)
		if err != nil {
			slog.Error("failed to marshal attributes", "error", err, "event", event)
			continue
		}

		_, err = stmt.ExecContext(
			ctx,
			idgen.New().String(),
			event.Subject,
			event.Action,
			event.Resource,
			event.Effect.String(),
			event.ID,
			event.Name,
			event.Message,
			string(event.Source),
			event.Component,
			attributesJSON,
			event.DurationUS,
			event.Timestamp.UnixNano(), // pgnanos-exempt: SQL-cast boundary for BIGINT timestamp column
		)
		if err != nil {
			slog.Error("failed to insert audit event", "error", err, "event", event)
			// Continue with other events
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
