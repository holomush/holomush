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
	subsystems map[SubsystemID]Subsystem
	startOrder []SubsystemID // populated by StartAll
}

// NewOrchestrator returns a new Orchestrator initialized with an empty subsystem registry.
// The returned Orchestrator has an empty subsystems map and a nil startOrder.
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

// StartAll topologically sorts subsystems by dependencies and starts
// them in order. Returns an error on the first Start failure, cycle
// detection, or missing dependency.
func (o *Orchestrator) StartAll(ctx context.Context) error {
	order, err := o.topoSort()
	if err != nil {
		return err
	}

	// Reset startOrder — only subsystems that successfully start are tracked.
	o.startOrder = nil

	for _, id := range order {
		sub := o.subsystems[id]
		slog.InfoContext(ctx, "starting subsystem", "subsystem", id.String())
		start := time.Now()

		if startErr := sub.Start(ctx); startErr != nil {
			// Rollback: stop already-started subsystems in reverse order.
			o.StopAll(ctx)
			return oops.
				Code("SUBSYSTEM_START_FAILED").
				With("subsystem", id.String()).
				Wrapf(startErr, "subsystem %s failed to start", id.String())
		}

		o.startOrder = append(o.startOrder, id)

		slog.InfoContext(
			ctx,
			"subsystem started",
			"subsystem", id.String(),
			"duration", time.Since(start).String(),
		)
	}

	return nil
}

// StopAll stops subsystems in reverse start order.
func (o *Orchestrator) StopAll(ctx context.Context) {
	for i := len(o.startOrder) - 1; i >= 0; i-- {
		id := o.startOrder[i]
		sub := o.subsystems[id]
		slog.InfoContext(ctx, "stopping subsystem", "subsystem", id.String())

		if err := sub.Stop(ctx); err != nil {
			slog.ErrorContext(
				ctx,
				"subsystem stop error",
				"subsystem", id.String(),
				"error", err,
			)
		}
	}
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
