// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package commandquery owns the single ABAC-filtered enumeration of registered
// commands for a subject. The Lua hostfunc bridge, the binary host.v1 CommandRegistryService
// handler, and the CoreService RPC are intended to delegate here so there is
// exactly one command-visibility filter (design spec INV-COMMAND-1); the delegating
// adapters land in later tasks of the recognized-command-chip plan.
package commandquery

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/observability"
	"github.com/holomush/holomush/pkg/errutil"
)

// maxEngineErrors trips the circuit breaker after repeated engine failures so a
// degraded engine does not incur O(commands*capabilities) calls. Ported from the
// former hostfunc implementation.
const maxEngineErrors = 3

// Registry is the read-only registry view the querier needs.
type Registry interface {
	All() []command.CommandEntry
	Get(name string) (command.CommandEntry, bool)
}

// AliasLister exposes the system/manifest alias map (tiers 1+2).
type AliasLister interface {
	ListSystemAliases() map[string]string
}

// Summary is the per-command metadata used by enumeration.
type Summary struct {
	Name   string
	Help   string
	Usage  string
	Source string
}

// Detail is the full per-command help payload.
type Detail struct {
	Name         string
	Help         string
	Usage        string
	HelpText     string
	Source       string
	Capabilities []command.Capability
}

// Result is the ABAC-filtered enumeration plus the alias map for visible commands.
type Result struct {
	Commands   []Summary
	Aliases    map[string]string // alias → canonical command name, restricted to visible commands
	Incomplete bool              // true when engine errors hid some commands
}

// Querier is the single command-visibility filter.
type Querier struct {
	registry Registry
	engine   types.AccessPolicyEngine
	aliases  AliasLister
}

// New constructs a Querier. All three dependencies are required for full results;
// a nil engine yields Incomplete results limited to no-capability commands.
func New(registry Registry, engine types.AccessPolicyEngine, aliases AliasLister) *Querier {
	return &Querier{registry: registry, engine: engine, aliases: aliases}
}

// Available returns the commands the subject may execute, the alias map for those
// commands, and an Incomplete flag. subject MUST be a formatted subject string
// (e.g. access.CharacterSubject(id)).
func (q *Querier) Available(ctx context.Context, subject string) (Result, error) {
	all := q.registry.All()
	visible := make(map[string]struct{}, len(all))
	out := make([]Summary, 0, len(all))

	var hadEngineError bool
	var engineErrorCount int
	circuitTripped := false

	for i := range all {
		if len(all[i].GetCapabilities()) == 0 {
			out = append(out, summaryOf(all[i]))
			visible[all[i].Name] = struct{}{}
			continue
		}
		if q.engine == nil {
			hadEngineError = true
			continue
		}
		if circuitTripped {
			continue
		}
		allowed, hadError := q.canExecute(ctx, subject, all[i])
		if hadError {
			hadEngineError = true
			engineErrorCount++
			if engineErrorCount >= maxEngineErrors {
				slog.WarnContext(
					ctx, "command list circuit breaker tripped",
					"engine_failures", engineErrorCount,
					"threshold", maxEngineErrors,
				)
				circuitTripped = true
			}
		}
		if allowed {
			out = append(out, summaryOf(all[i]))
			visible[all[i].Name] = struct{}{}
		}
	}

	aliasMap := map[string]string{}
	if q.aliases != nil {
		for alias, cmd := range q.aliases.ListSystemAliases() {
			if _, ok := visible[cmd]; ok {
				aliasMap[alias] = cmd
			}
		}
	}

	return Result{Commands: out, Aliases: aliasMap, Incomplete: hadEngineError}, nil
}

// Help returns the full help detail for one command after an access check.
func (q *Querier) Help(ctx context.Context, subject, name string) (Detail, error) {
	cmd, found := q.registry.Get(name)
	if !found {
		return Detail{}, oops.Code("NOT_FOUND").With("command", name).Errorf("command not found")
	}
	if len(cmd.GetCapabilities()) > 0 {
		if q.engine == nil {
			return Detail{}, oops.Code("UNAVAILABLE").Errorf("access engine not available")
		}
		allowed, hadError := q.canExecute(ctx, subject, cmd)
		if hadError {
			return Detail{}, oops.Code("UNAVAILABLE").With("command", name).Errorf("access check failed")
		}
		if !allowed {
			return Detail{}, oops.Code("PERMISSION_DENIED").With("command", name).Errorf("access denied")
		}
	}
	return Detail{
		Name: cmd.Name, Help: cmd.Help, Usage: cmd.Usage,
		HelpText: cmd.HelpText, Source: cmd.Source,
		Capabilities: cmd.GetCapabilities(),
	}, nil
}

func summaryOf(e command.CommandEntry) Summary {
	return Summary{Name: e.Name, Help: e.Help, Usage: e.Usage, Source: e.Source}
}

// canExecute ports the two-layer ABAC check from the former hostfunc impl
// (internal/plugin/hostfunc/commands.go:175-216).
func (q *Querier) canExecute(ctx context.Context, subject string, cmd command.CommandEntry) (allowed, hadError bool) {
	req, reqErr := types.NewAccessRequest(subject, "execute", "command:"+cmd.Name, nil)
	if reqErr != nil {
		errutil.LogErrorContext(ctx, "command access request failed", reqErr, "subject", subject, "command", cmd.Name)
		observability.RecordEngineFailure("command_capability_engine_error")
		return false, true
	}
	decision, evalErr := q.engine.Evaluate(ctx, req)
	if evalErr != nil {
		errutil.LogErrorContext(ctx, "command access evaluation failed", evalErr, "subject", subject, "command", cmd.Name)
		observability.RecordEngineFailure("command_capability_engine_error")
		return false, true
	}
	if !decision.IsAllowed() {
		if decision.IsInfraFailure() {
			return false, true
		}
		return false, false
	}
	for _, capability := range cmd.GetCapabilities() {
		ok, err := q.engine.CanPerformAction(ctx, subject, capability.Action, capability.Resource, capability.EffectiveScope())
		if err != nil {
			errutil.LogErrorContext(ctx, "capability pre-flight failed", err, "subject", subject, "action", capability.Action, "resource", capability.Resource)
			observability.RecordEngineFailure("command_capability_engine_error")
			return false, true
		}
		if !ok {
			return false, hadError
		}
	}
	return true, hadError
}
