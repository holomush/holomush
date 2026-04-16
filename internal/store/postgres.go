// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package store provides storage implementations.
package store

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
)

// ErrSystemInfoNotFound is returned when a system info key doesn't exist.
var ErrSystemInfoNotFound = errors.New("system info key not found")

// poolIface defines the pgxpool methods used by PostgresEventStore.
// This interface enables testing with mocks.
type poolIface interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Close()
}

// connIface defines the pgx.Conn methods used for LISTEN/NOTIFY subscriptions.
// This interface enables testing with mocks.
type connIface interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	WaitForNotification(ctx context.Context) (*pgconn.Notification, error)
	Close(ctx context.Context) error
}

// connectorFunc creates a new database connection for LISTEN/NOTIFY.
// This is a function type to enable test mocking.
type connectorFunc func(ctx context.Context, dsn string) (connIface, error)

// defaultConnector creates a real pgx connection.
func defaultConnector(ctx context.Context, dsn string) (connIface, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, oops.With("dsn_length", len(dsn)).Wrap(err)
	}
	return conn, nil
}

// PostgresEventStore implements EventStore using PostgreSQL.
type PostgresEventStore struct {
	pool      poolIface
	dsn       string        // stored for creating new connections for LISTEN
	connector connectorFunc // for creating LISTEN connections (nil uses default)
}

// NewPostgresEventStore creates a new PostgreSQL event store.
func NewPostgresEventStore(ctx context.Context, dsn string) (*PostgresEventStore, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, oops.With("operation", "parse database config").Wrap(err)
	}
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, oops.With("operation", "connect to database").Wrap(err)
	}
	return &PostgresEventStore{pool: pool, dsn: dsn}, nil
}

// Close closes the database connection pool.
func (s *PostgresEventStore) Close() {
	s.pool.Close()
}

// Pool returns the underlying database connection pool.
// This allows sharing the connection with other repositories.
// Returns nil if the pool is not a *pgxpool.Pool (e.g., in tests with mocks).
func (s *PostgresEventStore) Pool() *pgxpool.Pool {
	if pool, ok := s.pool.(*pgxpool.Pool); ok {
		return pool
	}
	return nil
}

// Append persists an event and notifies subscribers via NOTIFY.
func (s *PostgresEventStore) Append(ctx context.Context, event core.Event) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO events (id, stream, type, actor_kind, actor_id, payload, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		event.ID.String(),
		event.Stream,
		string(event.Type),
		event.Actor.Kind,
		event.Actor.ID,
		event.Payload,
		event.Timestamp,
	)
	if err != nil {
		return oops.With("operation", "append event").With("event_id", event.ID.String()).With("stream", event.Stream).Wrap(err)
	}

	// Notify subscribers of the new event
	// Errors are logged but not returned - event is already persisted, subscribers catch up via Replay
	// Note: Repeated NOTIFY failures indicate a serious connectivity issue that should be investigated
	//
	// Metrics consideration: Adding a Prometheus counter for NOTIFY failures was evaluated but
	// deferred. Current logging is sufficient for debugging, and adding metrics would require
	// threading the observability.Metrics through Store creation. This can be revisited when
	// expanding the observability infrastructure.
	channel := streamToChannel(event.Stream)
	if _, notifyErr := s.pool.Exec(ctx, "SELECT pg_notify($1, $2)", channel, event.ID.String()); notifyErr != nil {
		slog.Error("failed to notify subscribers of event",
			"event_id", event.ID.String(),
			"stream", event.Stream,
			"channel", channel,
			"error", notifyErr)
	}
	return nil
}

// Replay returns events from a stream after the given ID.
func (s *PostgresEventStore) Replay(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]core.Event, error) {
	var rows pgx.Rows
	var err error

	if afterID.Compare(ulid.ULID{}) == 0 {
		rows, err = s.pool.Query(ctx,
			`SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			 FROM events WHERE stream = $1 ORDER BY id LIMIT $2`,
			stream, limit)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			 FROM events WHERE stream = $1 AND id > $2 ORDER BY id LIMIT $3`,
			stream, afterID.String(), limit)
	}
	if err != nil {
		return nil, oops.With("operation", "query events").With("stream", stream).Wrap(err)
	}
	defer rows.Close()

	var events []core.Event
	for rows.Next() {
		var e core.Event
		var idStr string
		var typeStr string
		if scanErr := rows.Scan(&idStr, &e.Stream, &typeStr, &e.Actor.Kind, &e.Actor.ID, &e.Payload, &e.Timestamp); scanErr != nil {
			return nil, oops.With("operation", "scan event row").With("stream", stream).Wrap(scanErr)
		}
		e.ID, err = ulid.Parse(idStr)
		if err != nil {
			return nil, oops.With("operation", "parse event ID").With("stream", stream).With("raw_id", idStr).Wrap(err)
		}
		e.Type = core.EventType(typeStr)
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.With("operation", "iterate events").With("stream", stream).Wrap(err)
	}
	return events, nil
}

// LastEventID returns the most recent event ID for a stream.
func (s *PostgresEventStore) LastEventID(ctx context.Context, stream string) (ulid.ULID, error) {
	var idStr string
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM events WHERE stream = $1 ORDER BY id DESC LIMIT 1`,
		stream).Scan(&idStr)
	if errors.Is(err, pgx.ErrNoRows) {
		return ulid.ULID{}, core.ErrStreamEmpty
	}
	if err != nil {
		return ulid.ULID{}, oops.With("operation", "query last event ID").With("stream", stream).Wrap(err)
	}
	id, err := ulid.Parse(idStr)
	if err != nil {
		return ulid.ULID{}, oops.With("operation", "parse last event ID").With("stream", stream).With("raw_id", idStr).Wrap(err)
	}
	return id, nil
}

// GetSystemInfo retrieves a system info value by key.
func (s *PostgresEventStore) GetSystemInfo(ctx context.Context, key string) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx,
		`SELECT value FROM holomush_system_info WHERE key = $1`,
		key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", oops.With("key", key).Wrap(ErrSystemInfoNotFound)
	}
	if err != nil {
		return "", oops.With("operation", "get system info").With("key", key).Wrap(err)
	}
	return value, nil
}

// SetSystemInfo sets a system info value (upsert).
func (s *PostgresEventStore) SetSystemInfo(ctx context.Context, key, value string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO holomush_system_info (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()`,
		key, value)
	if err != nil {
		return oops.With("operation", "set system info").With("key", key).Wrap(err)
	}
	return nil
}

// InitGameID ensures a game_id exists, generating one if needed.
func (s *PostgresEventStore) InitGameID(ctx context.Context) (string, error) {
	gameID, err := s.GetSystemInfo(ctx, "game_id")
	if err == nil {
		return gameID, nil
	}
	// Only generate new ID if key genuinely doesn't exist
	if !errors.Is(err, ErrSystemInfoNotFound) {
		return "", oops.With("operation", "check existing game_id").Wrap(err)
	}

	// Generate new game_id
	gameID = core.NewULID().String()
	if err := s.SetSystemInfo(ctx, "game_id", gameID); err != nil {
		return "", err
	}
	return gameID, nil
}

// maxReplayTailCount is the server-side cap for ReplayTail count parameter.
const maxReplayTailCount = 500

// ReplayTail returns up to count most recent events on stream, ascending by
// event ID. If notBefore is non-zero, events with timestamps before it are
// excluded. If beforeID is non-zero, events with id >= beforeID are excluded
// (cursor-based pagination). Count is capped at 500.
func (s *PostgresEventStore) ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]core.Event, error) {
	if count > maxReplayTailCount {
		count = maxReplayTailCount
	}
	if count <= 0 {
		return nil, nil
	}

	zeroID := ulid.ULID{}
	hasNotBefore := !notBefore.IsZero()
	hasBeforeID := beforeID != zeroID

	var rows pgx.Rows
	var err error

	switch {
	case !hasNotBefore && !hasBeforeID:
		rows, err = s.pool.Query(ctx,
			`SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			 FROM (
			     SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			     FROM events WHERE stream = $1
			     ORDER BY id DESC LIMIT $2
			 ) sub ORDER BY id ASC`,
			stream, count)
	case hasNotBefore && !hasBeforeID:
		rows, err = s.pool.Query(ctx,
			`SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			 FROM (
			     SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			     FROM events WHERE stream = $1 AND created_at >= $2
			     ORDER BY id DESC LIMIT $3
			 ) sub ORDER BY id ASC`,
			stream, notBefore, count)
	case !hasNotBefore && hasBeforeID:
		rows, err = s.pool.Query(ctx,
			`SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			 FROM (
			     SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			     FROM events WHERE stream = $1 AND id < $2
			     ORDER BY id DESC LIMIT $3
			 ) sub ORDER BY id ASC`,
			stream, beforeID.String(), count)
	case hasNotBefore && hasBeforeID:
		rows, err = s.pool.Query(ctx,
			`SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			 FROM (
			     SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			     FROM events WHERE stream = $1 AND created_at >= $2 AND id < $3
			     ORDER BY id DESC LIMIT $4
			 ) sub ORDER BY id ASC`,
			stream, notBefore, beforeID.String(), count)
	}
	if err != nil {
		return nil, oops.With("operation", "replay tail").With("stream", stream).Wrap(err)
	}
	defer rows.Close()

	var events []core.Event
	for rows.Next() {
		var e core.Event
		var idStr string
		var typeStr string
		if scanErr := rows.Scan(&idStr, &e.Stream, &typeStr, &e.Actor.Kind, &e.Actor.ID, &e.Payload, &e.Timestamp); scanErr != nil {
			return nil, oops.With("operation", "scan replay tail row").With("stream", stream).Wrap(scanErr)
		}
		parsed, parseErr := ulid.Parse(idStr)
		if parseErr != nil {
			return nil, oops.With("operation", "parse replay tail event ID").With("raw_id", idStr).Wrap(parseErr)
		}
		e.ID = parsed
		e.Type = core.EventType(typeStr)
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.With("operation", "iterate replay tail").With("stream", stream).Wrap(err)
	}
	return events, nil
}

// streamToChannel converts a stream name to a PostgreSQL notification channel name.
// Replaces colons and hyphens with underscores since PG channel names must be valid identifiers.
func streamToChannel(stream string) string {
	s := strings.ReplaceAll(stream, ":", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return "events_" + s
}

// Subscribe starts listening for events on the given stream via PostgreSQL LISTEN/NOTIFY.
// Returns a channel of event IDs and an error channel. The caller should use Replay()
// to fetch full events by ID. Channels are closed when context is cancelled.
func (s *PostgresEventStore) Subscribe(ctx context.Context, stream string) (eventCh <-chan ulid.ULID, errCh <-chan error, err error) {
	// Create a dedicated connection for LISTEN (can't use pooled connections)
	connector := s.connector
	if connector == nil {
		connector = defaultConnector
	}
	conn, err := connector(ctx, s.dsn)
	if err != nil {
		return nil, nil, oops.With("operation", "connect for subscription").With("stream", stream).Wrap(err)
	}

	channel := streamToChannel(stream)

	// Start listening on the channel
	// Use pgx.Identifier to safely quote the channel name, preventing SQL injection
	_, err = conn.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize())
	if err != nil {
		if closeErr := conn.Close(ctx); closeErr != nil {
			slog.Error("failed to close connection during cleanup - connection will leak", "error", closeErr)
		}
		return nil, nil, oops.With("operation", "listen").With("channel", channel).Wrap(err)
	}

	events := make(chan ulid.ULID, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)
		defer func() {
			if closeErr := conn.Close(context.Background()); closeErr != nil {
				slog.Error("failed to close subscription connection - connection will leak", "error", closeErr, "stream", stream)
			}
		}()

		for {
			notification, err := conn.WaitForNotification(ctx)
			if err != nil {
				// Context cancelled is normal shutdown
				if ctx.Err() != nil {
					return
				}
				errs <- oops.With("operation", "wait for notification").With("stream", stream).Wrap(err)
				return
			}

			// Parse event ID from notification payload
			eventID, err := ulid.Parse(notification.Payload)
			if err != nil {
				errs <- oops.With("operation", "parse event ID from notification").With("payload", notification.Payload).Wrap(err)
				return
			}

			select {
			case events <- eventID:
			case <-ctx.Done():
				return
			}
		}
	}()

	return events, errs, nil
}

// streamCtrl is a control message sent to the notification loop to
// issue LISTEN/UNLISTEN on the conn it exclusively owns.
type streamCtrl struct {
	ctx    context.Context //nolint:containedctx // request-scoped context for LISTEN/UNLISTEN
	stream string
	add    bool
	done   chan error
}

// pgSubscription implements core.Subscription using a single dedicated
// pgx.Conn with multi-channel LISTEN. This is Variant A: all notifications
// from all streams arrive on one PG connection in commit order, which
// structurally enforces invariant I-14.
//
// The notification loop is the sole owner of conn. AddStream/RemoveStream
// communicate via ctrlCh and wake the loop by cancelling the current
// WaitForNotification context.
type pgSubscription struct {
	conn       connIface
	parentCtx  context.Context //nolint:containedctx // lifetime scoped to subscription
	notifCh    chan core.StreamNotification
	errCh      chan error
	ctrlCh     chan streamCtrl
	cancelWait context.CancelFunc // cancels the current WaitForNotification
	waitMu     sync.Mutex         // protects cancelWait
	cancel     context.CancelFunc // cancels parentCtx
	loopDone   chan struct{}      // closed when notificationLoop exits

	// streams/channels are only accessed inside notificationLoop (sole owner).
	streams  map[string]string // stream name -> channel name
	channels map[string]string // channel name -> stream name (reverse lookup)
}

// SubscribeSession opens a dedicated PG connection for a session-wide
// subscription. The returned Subscription supports dynamic AddStream/
// RemoveStream while delivering notifications in strict commit order
// across all subscribed streams (invariant I-14).
func (s *PostgresEventStore) SubscribeSession(ctx context.Context) (core.Subscription, error) {
	connector := s.connector
	if connector == nil {
		connector = defaultConnector
	}
	conn, err := connector(ctx, s.dsn)
	if err != nil {
		return nil, oops.With("operation", "connect for session subscription").Wrap(err)
	}

	subCtx, cancel := context.WithCancel(ctx)

	ps := &pgSubscription{
		conn:      conn,
		parentCtx: subCtx,
		streams:   make(map[string]string),
		channels:  make(map[string]string),
		notifCh:   make(chan core.StreamNotification, 256),
		errCh:     make(chan error, 1),
		ctrlCh:    make(chan streamCtrl, 16),
		cancel:    cancel,
		loopDone:  make(chan struct{}),
	}

	go ps.notificationLoop()
	return ps, nil
}

// notificationLoop is the sole goroutine that touches conn. It processes
// control messages (LISTEN/UNLISTEN) and reads PG notifications, forwarding
// them to notifCh. Runs until parentCtx is cancelled or an unrecoverable
// error occurs.
func (ps *pgSubscription) notificationLoop() {
	defer close(ps.notifCh)
	defer close(ps.errCh)
	defer close(ps.loopDone)

	for {
		// 1. Drain all pending control messages (non-blocking).
		ps.processCtrlMessages()

		// 2. Check if we should exit.
		if ps.parentCtx.Err() != nil {
			return
		}

		// 3. Create a cancellable context for WaitForNotification so
		//    AddStream/RemoveStream can wake us.
		waitCtx, waitCancel := context.WithCancel(ps.parentCtx)
		ps.waitMu.Lock()
		ps.cancelWait = waitCancel
		ps.waitMu.Unlock()

		// 3b. Re-check for ctrl messages that arrived between step 1 and
		//     the cancelWait assignment. If any are pending, cancel immediately
		//     to avoid blocking in WaitForNotification while a ctrl sits in ctrlCh.
		if len(ps.ctrlCh) > 0 {
			waitCancel()
			continue
		}

		// 4. Block on the next PG notification.
		notification, err := ps.conn.WaitForNotification(waitCtx)
		waitCancel() // release resources

		if err != nil {
			// Interrupted by AddStream/RemoveStream — loop back to
			// process the control message.
			if waitCtx.Err() != nil && ps.parentCtx.Err() == nil {
				continue
			}
			// Parent context cancelled — clean exit.
			if ps.parentCtx.Err() != nil {
				return
			}
			// Real error from PG.
			ps.errCh <- oops.With("operation", "wait for session notification").Wrap(err)
			return
		}

		// 5. Parse and dispatch the notification.
		eventID, err := ulid.Parse(notification.Payload)
		if err != nil {
			ps.errCh <- oops.With("operation", "parse event ID from session notification").
				With("payload", notification.Payload).Wrap(err)
			return
		}

		stream, ok := ps.channels[notification.Channel]
		if !ok {
			// Notification for a channel we already unlistened but was
			// buffered. Safe to skip.
			continue
		}

		select {
		case ps.notifCh <- core.StreamNotification{Stream: stream, EventID: eventID}:
		case <-ps.parentCtx.Done():
			return
		}
	}
}

// processCtrlMessages drains all pending control messages from ctrlCh,
// executing LISTEN/UNLISTEN on conn (which we exclusively own).
func (ps *pgSubscription) processCtrlMessages() {
	for {
		select {
		case ctrl := <-ps.ctrlCh:
			ctrl.done <- ps.handleCtrl(ctrl)
		default:
			return
		}
	}
}

// handleCtrl executes a single LISTEN or UNLISTEN on conn.
// Uses the request's context for the SQL exec so callers can timeout.
func (ps *pgSubscription) handleCtrl(ctrl streamCtrl) error {
	// Use the request context if provided, falling back to parentCtx.
	execCtx := ctrl.ctx
	if execCtx == nil {
		execCtx = ps.parentCtx
	}

	if ctrl.add {
		if _, exists := ps.streams[ctrl.stream]; exists {
			return nil // idempotent
		}
		channel := streamToChannel(ctrl.stream)
		_, err := ps.conn.Exec(execCtx, "LISTEN "+pgx.Identifier{channel}.Sanitize())
		if err != nil {
			return oops.With("operation", "listen").With("channel", channel).With("stream", ctrl.stream).Wrap(err)
		}
		ps.streams[ctrl.stream] = channel
		ps.channels[channel] = ctrl.stream
		return nil
	}

	// Remove.
	channel, exists := ps.streams[ctrl.stream]
	if !exists {
		return nil
	}
	_, err := ps.conn.Exec(execCtx, "UNLISTEN "+pgx.Identifier{channel}.Sanitize())
	if err != nil {
		return oops.With("operation", "unlisten").With("channel", channel).With("stream", ctrl.stream).Wrap(err)
	}
	delete(ps.streams, ctrl.stream)
	delete(ps.channels, channel)
	return nil
}

// AddStream starts LISTENing on the PG channel for the given stream.
// Idempotent: if the stream is already added, returns nil immediately.
// The request is processed by the notification loop which owns the conn.
func (ps *pgSubscription) AddStream(ctx context.Context, stream string) error {
	done := make(chan error, 1)
	ctrl := streamCtrl{ctx: ctx, stream: stream, add: true, done: done}

	select {
	case ps.ctrlCh <- ctrl:
	case <-ctx.Done():
		return oops.With("operation", "add stream").With("stream", stream).Wrap(ctx.Err())
	case <-ps.parentCtx.Done():
		return errors.New("subscription closed")
	}

	// Wake the notification loop if it's blocked in WaitForNotification.
	ps.waitMu.Lock()
	if ps.cancelWait != nil {
		ps.cancelWait()
	}
	ps.waitMu.Unlock()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return oops.With("operation", "add stream").With("stream", stream).Wrap(ctx.Err())
	case <-ps.parentCtx.Done():
		return errors.New("subscription closed")
	}
}

// RemoveStream stops LISTENing on the PG channel for the given stream.
// The request is processed by the notification loop which owns the conn.
func (ps *pgSubscription) RemoveStream(ctx context.Context, stream string) error {
	done := make(chan error, 1)
	ctrl := streamCtrl{ctx: ctx, stream: stream, add: false, done: done}

	select {
	case ps.ctrlCh <- ctrl:
	case <-ctx.Done():
		return oops.With("operation", "remove stream").With("stream", stream).Wrap(ctx.Err())
	case <-ps.parentCtx.Done():
		return errors.New("subscription closed")
	}

	// Wake the notification loop if it's blocked in WaitForNotification.
	ps.waitMu.Lock()
	if ps.cancelWait != nil {
		ps.cancelWait()
	}
	ps.waitMu.Unlock()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return oops.With("operation", "remove stream").With("stream", stream).Wrap(ctx.Err())
	case <-ps.parentCtx.Done():
		return errors.New("subscription closed")
	}
}

// Notifications returns the unified notification channel.
func (ps *pgSubscription) Notifications() <-chan core.StreamNotification {
	return ps.notifCh
}

// Errors returns the error channel.
func (ps *pgSubscription) Errors() <-chan error {
	return ps.errCh
}

// Close releases the dedicated PG connection and cancels the notification loop.
func (ps *pgSubscription) Close() error {
	ps.cancel()
	<-ps.loopDone // wait for the notification loop to exit
	if err := ps.conn.Close(context.Background()); err != nil {
		slog.Error("failed to close session subscription connection", "error", err)
		return oops.With("operation", "close session subscription connection").Wrap(err)
	}
	return nil
}
