// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// Pill is the process-termination interface invoked when this member
// receives a poison-pill message. Production wiring uses os.Exit(125);
// test wiring records the trigger on a channel; dev wiring panics.
//
// Trigger MUST log a structured error entry and increment
// replica_poisoned_total{member_id, reason, source_id} BEFORE any
// termination action. Production deployments MUST run under a
// supervisor that interprets exit code 125 as restart-eligible
// (systemd Restart=on-failure, k8s restartPolicy=Always, docker
// restart=on-failure).
//
// ctx is provided so implementations can flush context-bound
// telemetry (e.g., open spans). Implementations MUST bound flush
// time (default 1s) since the cluster has already decided this
// process is done.
type Pill interface {
	Trigger(ctx context.Context, reason PillReason, sourceID MemberID)
}

// pillFlushTimeout caps how long Trigger waits to flush telemetry
// before terminating. Bounded so a stuck telemetry pipeline can't
// block process exit indefinitely.
const pillFlushTimeout = 1 * time.Second

// PillEvent is recorded by TestPill for assertion purposes.
type PillEvent struct {
	Reason   PillReason
	SourceID MemberID
	At       time.Time
}

// productionPill calls os.Exit(125) after flushing telemetry.
type productionPill struct {
	self    MemberID
	logger  *slog.Logger
	metrics *PillMetrics
	exitFn  func(int) // seam for tests; production = os.Exit
}

// NewProductionPill constructs the production Pill that exits with
// code 125 after best-effort telemetry flush.
func NewProductionPill(self MemberID, logger *slog.Logger, metrics *PillMetrics) Pill {
	return &productionPill{
		self:    self,
		logger:  logger,
		metrics: metrics,
		exitFn:  os.Exit,
	}
}

func (p *productionPill) Trigger(ctx context.Context, reason PillReason, sourceID MemberID) {
	p.logger.ErrorContext(ctx,
		"pill received; terminating",
		"self", string(p.self),
		"reason", string(reason),
		"source_id", string(sourceID),
	)
	if p.metrics != nil {
		p.metrics.PoisonedTotal.WithLabelValues(string(p.self), string(reason), string(sourceID)).Inc()
	}
	// TODO(T11+): flush open telemetry spans bounded by pillFlushTimeout
	// before os.Exit. defer cancel() does not run because os.Exit bypasses
	// defers; the timeout exists for future flush calls only.
	_ = pillFlushTimeout // silence unused-const lint until flush calls land
	p.exitFn(125)
}

// testPill records each Trigger call on a channel and does NOT exit.
type testPill struct {
	events chan PillEvent
}

// NewTestPill constructs a Pill that records pill triggers on the
// returned channel for test assertions. Trigger does NOT exit; the
// test verifies behavior and continues.
func NewTestPill() (pill Pill, events <-chan PillEvent) {
	p := &testPill{events: make(chan PillEvent, 16)}
	return p, p.events
}

func (p *testPill) Trigger(_ context.Context, reason PillReason, sourceID MemberID) {
	select {
	case p.events <- PillEvent{Reason: reason, SourceID: sourceID, At: time.Now()}:
	default:
		// channel full; drop. Tests should drain promptly.
	}
}

// devPill panics with a recoverable message; the running `holomush
// dev` process catches the panic and surfaces the error in the
// foreground.
type devPill struct {
	self   MemberID
	logger *slog.Logger
}

// NewDevPill constructs a dev-mode Pill that panics on Trigger.
func NewDevPill(self MemberID, logger *slog.Logger) Pill {
	return &devPill{self: self, logger: logger}
}

func (p *devPill) Trigger(ctx context.Context, reason PillReason, sourceID MemberID) {
	p.logger.ErrorContext(ctx,
		"dev pill received",
		"self", string(p.self),
		"reason", string(reason),
		"source_id", string(sourceID),
	)
	panic(fmt.Sprintf("cluster: pill received reason=%s source=%s", reason, sourceID))
}
