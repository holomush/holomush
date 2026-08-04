// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package blocklist

import (
	"context"
	"log/slog"
	"time"

	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/lifecycle"
)

// Compile-time pins.
//
// The first is the one that matters: it fixes *Cache — the LIVE matcher — as
// the type a charname.Gate can be built over, so a refactor that changed
// Cache.Match's shape (or quietly moved production onto a *Snapshot) would
// fail to build rather than fail silently at runtime.
var (
	_ charname.BlockList  = (*Cache)(nil)
	_ charname.BlockList  = (*Snapshot)(nil)
	_ lifecycle.Subsystem = (*Subsystem)(nil)
)

// Source supplies both halves of the block list's database access: the raw
// value read and the two-signal change indicator. It is resolved LAZILY —
// through SubsystemConfig.Source — because the subsystem is constructed at
// wiring time, before the database subsystem has a pool.
//
// *store.PostgresEventStore satisfies it.
type Source interface {
	RawGetter
	VersionQuerier
}

// SubsystemConfig configures the block-list subsystem.
type SubsystemConfig struct {
	// Source resolves the settings reader. It is called during Prepare and on
	// every poll, never at construction.
	Source func() Source
	// Key defaults to DefaultKey.
	Key string
	// Interval is the poll interval; it defaults to 10s.
	Interval time.Duration
	// Registry, when non-nil, receives this subsystem's health tracker so an
	// operator can see poll failures in the readiness surface.
	Registry *lifecycle.ReadinessRegistry
}

// Subsystem owns the character-name block list: boot validation in Prepare,
// the settings poll loop in Activate, and its shutdown in Stop.
//
// # Why this is its own subsystem
//
// A poll loop needs a real Activate that starts it and a real Stop that ends
// it. Attaching it to the one-shot Prepare-time seeder whose Activate and Stop
// are documented no-ops would produce a loop that either never starts or never
// shuts down — an unenforced block list, or a goroutine outliving shutdown.
type Subsystem struct {
	cfg     SubsystemConfig
	cache   *Cache
	tracker *lifecycle.HealthTracker

	pollerCancel context.CancelFunc
	pollerDone   chan struct{}
}

// NewSubsystem creates the block-list subsystem. The cache is built HERE, not
// in Prepare, so Matcher() returns the same live value from construction
// onward — which is what lets cmd/holomush hand one matcher to both
// composition roots before anything has been prepared.
func NewSubsystem(cfg SubsystemConfig) *Subsystem {
	if cfg.Key == "" {
		cfg.Key = DefaultKey
	}
	return &Subsystem{
		cfg: cfg,
		cache: NewCache(func() RawGetter {
			return cfg.Source()
		}, cfg.Key),
		tracker: lifecycle.NewHealthTracker(lifecycle.TrackerConfig{
			SubsystemName: lifecycle.SubsystemCharacterNameBlockList.String(),
		}),
	}
}

// ID returns SubsystemCharacterNameBlockList.
func (s *Subsystem) ID() lifecycle.SubsystemID {
	return lifecycle.SubsystemCharacterNameBlockList
}

// DependsOn returns [SubsystemDatabase] — the poller needs the pool and
// nothing else.
func (s *Subsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}
}

// Matcher returns the LIVE matcher as a charname.BlockList.
//
// The returned value is safe to hold from construction onward and is stable
// across Prepare: it is the cache, never a snapshot. A snapshot handed over at
// boot would freeze the list for the process lifetime while the poller
// refreshed something nothing reads.
//
// Its snapshot is EMPTY — matching nothing — until Prepare succeeds. That is
// exactly why the bootstrap subsystem's dependency edge on this subsystem is
// required rather than optional: bootstrap's Prepare may create the initial
// admin character, and that admission must not run against an uncompiled list.
func (s *Subsystem) Matcher() charname.BlockList { return s.cache }

// Prepare loads, validates and compiles the configured list once. An
// uncompilable pattern or a malformed settings value is a HARD startup
// failure whose error names the offending entry (D-15): the whole list is
// validated, the error is returned, and startup aborts. It is never logged and
// continued past — a server running with a silently-disabled block list is the
// state this validation exists to prevent.
//
// Prepare is idempotent: re-running it simply recompiles the current value.
func (s *Subsystem) Prepare(ctx context.Context) error {
	if err := s.cache.Reload(ctx); err != nil {
		s.tracker.RecordFailure("boot validation failed: " + err.Error())
		return err
	}

	if s.cfg.Registry != nil {
		s.cfg.Registry.Register(lifecycle.SubsystemCharacterNameBlockList, s.tracker)
	}

	s.tracker.RecordSuccess()
	slog.InfoContext(ctx, "character-name block list prepared",
		"key", s.cfg.Key, "patterns", s.cache.Snapshot().Len())
	return nil
}

// Activate starts the poll loop on a background context so it outlives the
// startup context; shutdown is driven by Stop, not by the caller's ctx.
//
// Idempotent: a repeated Activate does not launch a second goroutine.
func (s *Subsystem) Activate(ctx context.Context) error {
	if s.pollerCancel != nil {
		return nil
	}

	poller, err := NewPoller(PollerConfig{
		Querier:  s.cfg.Source(),
		Reloader: s.cache,
		Tracker:  s.tracker,
		Key:      s.cfg.Key,
		Interval: s.cfg.Interval,
	})
	if err != nil {
		return err
	}

	pollerCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.pollerCancel = cancel
	s.pollerDone = done

	go func() {
		defer close(done)
		poller.Run(pollerCtx)
	}()

	slog.InfoContext(ctx, "character-name block-list poller activated",
		"key", s.cfg.Key, "interval", poller.Interval())
	return nil
}

// Stop cancels the poll loop and WAITS for its goroutine to return before
// returning itself. A Stop that returned while the loop still polled would be
// a leak that surfaces only as a flaky or hung shutdown, months later.
//
// Safe in all three subsystem states (never activated, partially prepared,
// fully activated) and idempotent.
func (s *Subsystem) Stop(ctx context.Context) error {
	if s.pollerCancel != nil {
		s.pollerCancel()
		s.pollerCancel = nil

		select {
		case <-s.pollerDone:
		case <-ctx.Done():
			// The caller's shutdown budget expired. Say so rather than
			// blocking the whole shutdown sweep; the loop is already
			// cancelled and returns on its next select.
			slog.WarnContext(ctx, "character-name block-list poller did not exit within the shutdown budget",
				"key", s.cfg.Key)
		}
	}

	// Unregistered unconditionally: Prepare registers, so a
	// prepared-but-never-activated subsystem must still unregister here or a
	// retried Prepare panics on a duplicate registration.
	if s.cfg.Registry != nil {
		s.cfg.Registry.Unregister(lifecycle.SubsystemCharacterNameBlockList)
	}
	return nil
}
