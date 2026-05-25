// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"log/slog"
)

// GatedSubcommand is a structural ABAC gate for plugin subcommands.
// The gate is called unconditionally before the Handler — it is impossible
// to invoke the Handler without first passing the access-control check.
//
// Fields:
//   - Name: human-readable subcommand name (used in error messages).
//   - Action: the ABAC action string passed to HostEvaluator.Evaluate.
//   - ResourceRef: derives the resource string from the subcommand args.
//     If it returns an error the gate and handler are both skipped.
//   - Handler: the business logic, called only when the gate allows.
type GatedSubcommand struct {
	Name        string
	Action      string
	ResourceRef func(args string) (string, error)
	Handler     func(ctx context.Context, req CommandRequest, args string) (*CommandResponse, error)
}

// Run executes the gated subcommand in order:
//  1. Resolve the resource reference — on error return CommandError (gate skipped).
//  2. Evaluate access — on engine error return CommandFailure; on deny return CommandError.
//  3. Run the Handler.
func (g GatedSubcommand) Run(
	ctx context.Context,
	ev HostEvaluator,
	req CommandRequest,
	args string,
) (*CommandResponse, error) {
	// Step 1: resolve resource reference.
	resource, refErr := g.ResourceRef(args)
	if refErr != nil {
		return Errorf("%s: %v", g.Name, refErr), nil
	}

	// Step 2: evaluate ABAC gate.
	dec, evalErr := ev.Evaluate(ctx, g.Action, resource)
	if evalErr != nil {
		slog.ErrorContext(ctx, "plugin subcommand permission check failed",
			"subcommand", g.Name, "action", g.Action, "error", evalErr)
		return Failuref("permission check failed: %v", evalErr), nil
	}
	if !dec.Allowed {
		reason := dec.Reason
		if reason == "" {
			reason = "you are not permitted to do that"
		}
		return Errorf("%s", reason), nil
	}

	// Step 3: run handler.
	return g.Handler(ctx, req, args)
}
