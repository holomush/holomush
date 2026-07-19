// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/samber/oops"
)

// Orchestrator manages the lifecycle of registered Subsystems.
// It starts them in topological (dependency) order and stops them
// in reverse order.
type Orchestrator struct {
	subsystems     map[SubsystemID]Subsystem
	preparedOrder  []SubsystemID // subsystems whose Prepare was invoked, in invocation order
	activatedOrder []SubsystemID // subsystems whose Activate succeeded, in invocation order
}

// NewOrchestrator returns a new Orchestrator initialized with an empty subsystem registry.
// The returned Orchestrator has an empty subsystems map and no prepared/activated history.
func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		subsystems: make(map[SubsystemID]Subsystem),
	}
}

// Register adds a subsystem. Panics on duplicate registration.
func (o *Orchestrator) Register(s Subsystem) {
	id := s.ID()
	if _, exists := o.subsystems[id]; exists {
		panic("lifecycle: duplicate subsystem registration: " + id.String())
	}
	o.subsystems[id] = s
}

// StartAll topologically sorts subsystems by dependencies and runs two full
// sweeps over that order: every subsystem's Prepare, then every subsystem's
// Activate. This two-sweep shape is the point (D-12 Wave B, D-13.1) — it is
// what makes "no subsystem accepts externally-reachable domain traffic and
// no host-owned domain work loop runs until every subsystem has acquired" a
// structural property of the orchestrator rather than something that
// depends on every DependsOn edge being remembered.
//
// Returns an error on the first Prepare or Activate failure, cycle
// detection, or missing dependency. On a Prepare or Activate failure,
// StartAll rolls back every prepared subsystem (Stop, in reverse order) on
// a fresh bounded context before returning.
func (o *Orchestrator) StartAll(ctx context.Context) error {
	order, err := o.topoSort()
	if err != nil {
		return err
	}

	// Reset prepared/activated history — only subsystems actually swept in
	// this call are tracked.
	o.preparedOrder = nil
	o.activatedOrder = nil

	// Sweep 1: Prepare every subsystem in topological order.
	for _, id := range order {
		sub := o.subsystems[id]
		slog.InfoContext(ctx, "preparing subsystem", "subsystem", id.String())
		start := time.Now()

		// Record the id BEFORE calling Prepare (not after success): a failed
		// Prepare may have partially acquired resources, and the subsystem
		// that just failed is precisely the one most likely to need Stop
		// called on it during rollback (cross-AI review, Bug 1).
		o.preparedOrder = append(o.preparedOrder, id)

		if prepErr := sub.Prepare(ctx); prepErr != nil {
			o.rollback(ctx)
			return oops.
				Code("SUBSYSTEM_START_FAILED").
				With("subsystem", id.String()).
				Wrapf(prepErr, "subsystem %s failed to prepare", id.String())
		}

		slog.InfoContext(
			ctx,
			"subsystem prepared",
			"subsystem", id.String(),
			"duration", time.Since(start).String(),
		)
	}

	// Sweep 2: Activate every subsystem in topological order. This is the
	// global barrier — no Activate runs until every Prepare above has
	// returned successfully.
	for _, id := range order {
		sub := o.subsystems[id]
		slog.InfoContext(ctx, "activating subsystem", "subsystem", id.String())
		start := time.Now()

		if actErr := sub.Activate(ctx); actErr != nil {
			o.rollback(ctx)
			return oops.
				Code("SUBSYSTEM_START_FAILED").
				With("subsystem", id.String()).
				Wrapf(actErr, "subsystem %s failed to activate", id.String())
		}

		o.activatedOrder = append(o.activatedOrder, id)

		slog.InfoContext(
			ctx,
			"subsystem activated",
			"subsystem", id.String(),
			"duration", time.Since(start).String(),
		)
	}

	return nil
}

// rollback tears down every prepared subsystem (a superset of the activated
// set — activatedOrder is a subset of preparedOrder by construction) via
// StopAll, on a FRESH bounded context — never the startup ctx passed to
// StartAll. The startup ctx is exactly what may already be cancelled in the
// failure modes rollback exists for (SIGINT mid-boot, a boot deadline
// elapsing), and StopAll is deadline-aware: handing it an already-dead ctx
// would make it abandon every rollback Stop instantly (cross-AI review,
// Bug 2 — verified). Normal (non-rollback) StopAll callers keep passing
// their own ctx; this fresh-context rule applies only to StartAll's own
// rollback path.
func (o *Orchestrator) rollback(_ context.Context) {
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	o.StopAll(stopCtx)
}

// StopAll stops every prepared subsystem — preparedOrder is a superset of
// activatedOrder, so one reverse walk correctly tears down subsystems that
// were prepared but never activated, partially prepared, or fully activated
// (D-13.1: this is why three methods, Prepare/Activate/Stop, suffice; no
// separate Unprepare/Deactivate is needed) — in reverse order, honoring
// ctx's deadline (LOW-7). If ctx is done before every subsystem has been
// stopped, StopAll logs the remaining SubsystemIDs at error level and
// returns immediately rather than waiting further — silent abandonment is
// worse than a slow shutdown (T-07-53), but an unbounded shutdown is worse
// than a bounded one.
//
// Each subsystem's Stop runs in its own goroutine so that one Stop which
// ignores its own ctx and hangs past the deadline cannot hold up the loop;
// StopAll races that goroutine against ctx.Done(). The per-Stop result
// channel MUST be buffered (capacity 1): once StopAll abandons a Stop and
// moves on, nothing will ever receive from that channel, so an unbuffered
// send would block the goroutine forever — a permanent leak instead of one
// that ends the moment the abandoned Stop actually returns.
//
// This design assumes StopAll's callers are terminal. In production StopAll
// is called exactly once at process exit (the deferred call in
// cmd/holomush/core.go), plus at most once more on StartAll's rollback path
// above — which always builds its OWN fresh bounded context, never the
// (possibly already-cancelled) startup ctx, so a cancelled boot cannot
// abandon rollback cleanup. A subsystem whose Stop routinely outlives the
// deadline is a bug in that subsystem, not a tolerated steady state: the
// Subsystem interface already contracts that Stop "MUST NOT block
// indefinitely" (subsystem.go) — this method simply stops trusting that
// contract blindly and bounds the damage when it is violated.
//
// Reusing an Orchestrator after a rollback that timed out — calling
// StartAll again, or registering/starting replacement subsystems on the
// same instance — is UNSUPPORTED while abandoned Stop goroutines may still
// be running: an abandoned teardown can mutate subsystem state after
// rollback returns. Production never does this (a failed StartAll is
// followed by process exit; there is no in-tree retry path). Tests that
// exercise rollback must either use stubs whose Stop honors its ctx, or
// discard the Orchestrator instance afterward.
func (o *Orchestrator) StopAll(ctx context.Context) {
	for i := len(o.preparedOrder) - 1; i >= 0; i-- {
		if ctx.Err() != nil {
			logAbandonedSubsystems(ctx, o.preparedOrder[:i+1])
			return
		}

		id := o.preparedOrder[i]
		sub := o.subsystems[id]
		slog.InfoContext(ctx, "stopping subsystem", "subsystem", id.String())

		result := make(chan error, 1) // buffered one-shot: see doc comment above
		go func() {
			result <- sub.Stop(ctx)
		}()

		select {
		case stopErr := <-result:
			if stopErr != nil {
				slog.ErrorContext(
					ctx,
					"subsystem stop error",
					"subsystem", id.String(),
					"error", stopErr,
				)
			}
		case <-ctx.Done():
			logAbandonedSubsystems(ctx, o.preparedOrder[:i+1])
			return
		}
	}
}

// logAbandonedSubsystems logs, at error level, the SubsystemIDs StopAll did
// not get to stop before ctx's deadline elapsed (T-07-53: silent
// abandonment is worse than a slow shutdown).
func logAbandonedSubsystems(ctx context.Context, remaining []SubsystemID) {
	ids := make([]string, len(remaining))
	for i, id := range remaining {
		ids[i] = id.String()
	}
	slog.ErrorContext(
		ctx,
		"stopall deadline exceeded, abandoning remaining subsystems",
		"remaining_subsystems", ids,
	)
}

// topoSort performs Kahn's algorithm for topological sorting.
func (o *Orchestrator) topoSort() ([]SubsystemID, error) {
	// Build in-degree map and adjacency list
	inDegree := make(map[SubsystemID]int)
	dependents := make(map[SubsystemID][]SubsystemID)

	for id := range o.subsystems {
		inDegree[id] = 0
	}

	for id, sub := range o.subsystems {
		for _, dep := range sub.DependsOn() {
			if _, exists := o.subsystems[dep]; !exists {
				return nil, fmt.Errorf("subsystem %s has missing dependency: %s", id.String(), dep.String())
			}
			inDegree[id]++
			dependents[dep] = append(dependents[dep], id)
		}
	}

	// Seed queue with zero-dependency subsystems
	var queue []SubsystemID
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	sort.Slice(queue, func(i, j int) bool { return queue[i] < queue[j] })

	var order []SubsystemID
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		order = append(order, current)

		var newReady []SubsystemID
		for _, dependent := range dependents[current] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				newReady = append(newReady, dependent)
			}
		}
		sort.Slice(newReady, func(i, j int) bool { return newReady[i] < newReady[j] })
		queue = append(queue, newReady...)
	}

	if len(order) != len(o.subsystems) {
		return nil, fmt.Errorf("dependency cycle detected among subsystems")
	}

	return order, nil
}
