// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

// PgListener implements Listener using a dedicated PostgreSQL connection
// for LISTEN/NOTIFY. It internally reconnects with exponential backoff on
// connection failure, keeping the output channel open.
type PgListener struct {
	connStr string
}

// NewPgListener creates a listener that connects to PostgreSQL using connStr.
// The connection is dedicated (not from a pool) to avoid holding pool slots.
func NewPgListener(connStr string) *PgListener {
	return &PgListener{connStr: connStr}
}

// Listen returns a channel that emits pg_notify payloads for the
// "policy_changed" channel. The channel closes only when ctx is cancelled.
// Connection failures are handled internally with exponential backoff.
func (l *PgListener) Listen(ctx context.Context) (<-chan string, error) {
	ch := make(chan string, 16)
	go l.listenLoop(ctx, ch)
	return ch, nil
}

func (l *PgListener) listenLoop(ctx context.Context, ch chan<- string) {
	defer close(ch)

	const (
		initialBackoff = 100 * time.Millisecond
		maxBackoff     = 30 * time.Second
		backoffFactor  = 2.0
	)

	backoff := initialBackoff
	for {
		if ctx.Err() != nil {
			return
		}

		conn, err := pgx.Connect(ctx, l.connStr)
		if err != nil {
			slog.Warn("pg_notify listener: connect failed, retrying",
				"error", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = time.Duration(math.Min(
				float64(backoff)*backoffFactor,
				float64(maxBackoff),
			))
			continue
		}

		// Reset backoff on successful connect
		backoff = initialBackoff

		_, err = conn.Exec(ctx, "LISTEN policy_changed")
		if err != nil {
			slog.Warn("pg_notify listener: LISTEN failed", "error", err)
			_ = conn.Close(ctx) //nolint:errcheck // best-effort cleanup
			continue
		}

		slog.Info("pg_notify listener: connected and listening")

		// Read notifications until error or context cancellation
		for {
			notification, err := conn.WaitForNotification(ctx)
			if err != nil {
				if ctx.Err() != nil {
					_ = conn.Close(ctx) //nolint:errcheck // best-effort cleanup
					return
				}
				slog.Warn("pg_notify listener: notification error, reconnecting",
					"error", err)
				_ = conn.Close(ctx) //nolint:errcheck // best-effort cleanup
				break // reconnect
			}

			select {
			case ch <- notification.Payload:
			case <-ctx.Done():
				_ = conn.Close(ctx) //nolint:errcheck // best-effort cleanup
				return
			}
		}
	}
}
