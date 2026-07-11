// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/idgen"
)

// scopeProbeTimeout bounds how long VerifyAccountScoping waits for the server to
// signal a permissions violation on a forbidden probe. A correctly-scoped
// account raises the violation immediately after the Flush round-trip, so this
// is only the ceiling for the fail-closed conclusion: if no violation surfaces
// within the window the probe was PERMITTED, meaning the account is over-scoped.
const scopeProbeTimeout = 3 * time.Second

// VerifyAccountScoping proves the server's OWN NATS account is not over-scoped
// (CLUSTER-02, D-13c). It is the internal half of the single-principal proof:
// the external verify-scoping.sh proves other principals are locked out; this
// self-check proves the server itself cannot reach BEYOND the granted game-topic
// prefixes (events.>/audit.>/internal.>/_INBOX.>).
//
// It attempts a subscribe AND a publish on a probe subject
// (forbidden.scopecheck.<nonce>) that a correctly-scoped account MUST be denied.
// Detection is fail-closed: if EITHER operation is PERMITTED (no permissions
// violation surfaces within scopeProbeTimeout) the account can reach outside its
// grants and VerifyAccountScoping returns EVENTBUS_ACCOUNT_OVERSCOPED so boot
// refuses (D-13c). When both probes are denied it returns nil.
//
// Enforcement of scoping lives at the NATS account layer, never in the server
// (phase3d Decision 4, RESEARCH Don't-Hand-Roll): this function only observes
// its own account's reach, it does not impose an app-level ACL.
//
// It MUST be called only in external mode — embedded NATS has no account model,
// so its default-open permissions always look over-scoped (which is the correct
// negative fixture, but not a real deployment condition).
func VerifyAccountScoping(ctx context.Context, conn *nats.Conn) error {
	if conn == nil {
		return oops.Code("EVENTBUS_ACCOUNT_OVERSCOPED").
			Errorf("scope check requires a live NATS connection, got nil")
	}

	// forbidden.> is outside every granted prefix, so a correctly-scoped
	// account is denied on it. The nonce keeps concurrent boots from colliding.
	probe := fmt.Sprintf("forbidden.scopecheck.%s", idgen.New().String())

	// Capture permissions violations via the async error handler. A buffered
	// channel absorbs the async dispatch without blocking nats.go's callback
	// goroutine. Save and restore the prior handler: this runs on the shared
	// long-lived connection (event bus, audit projection, cluster, invalidation
	// all share it), so leaving our probe closure installed would orphan any
	// later component that relies on connection-level async error surfacing.
	priorErrorHandler := conn.ErrorHandler()
	defer conn.SetErrorHandler(priorErrorHandler)
	violations := make(chan error, 8)
	conn.SetErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
		if errors.Is(err, nats.ErrPermissionViolation) {
			select {
			case violations <- err:
			default:
			}
		}
	})

	// Probe subscribe: a denied SUB raises a permissions violation.
	subDenied, err := probeDenied(ctx, conn, violations, func() error {
		sub, subErr := conn.SubscribeSync(probe)
		if subErr != nil {
			// A synchronous permission error here is also a denial.
			if errors.Is(subErr, nats.ErrPermissionViolation) {
				return nil
			}
			return oops.Code("EVENTBUS_SCOPE_CHECK_FAILED").
				With("probe", probe).
				With("op", "subscribe").
				Wrap(subErr)
		}
		// Best-effort teardown; a denied sub is auto-removed server-side.
		defer sub.Unsubscribe() //nolint:errcheck // best-effort teardown of the probe subscription
		return conn.Flush()
	})
	if err != nil {
		return err
	}
	if !subDenied {
		return overScoped(probe, "subscribe")
	}

	// Probe publish: a denied PUB raises a permissions violation.
	pubDenied, err := probeDenied(ctx, conn, violations, func() error {
		if pubErr := conn.Publish(probe, []byte("scopecheck-probe")); pubErr != nil {
			if errors.Is(pubErr, nats.ErrPermissionViolation) {
				return nil
			}
			return oops.Code("EVENTBUS_SCOPE_CHECK_FAILED").
				With("probe", probe).
				With("op", "publish").
				Wrap(pubErr)
		}
		return conn.Flush()
	})
	if err != nil {
		return err
	}
	if !pubDenied {
		return overScoped(probe, "publish")
	}

	slog.DebugContext(ctx, "event bus account scope check passed",
		"probe", probe)
	return nil
}

// probeDenied runs op (which issues a forbidden operation and flushes) then
// waits up to scopeProbeTimeout for a permissions violation. It returns true iff
// the operation was DENIED (a violation surfaced). A non-permission error from
// op is propagated as a coded error (the check could not be completed).
func probeDenied(
	ctx context.Context,
	conn *nats.Conn,
	violations <-chan error,
	op func() error,
) (bool, error) {
	// Drain any stale violation from a prior probe so this phase only observes
	// its own.
	select {
	case <-violations:
	default:
	}

	if err := op(); err != nil {
		return false, err
	}

	waitCtx, cancel := context.WithTimeout(ctx, scopeProbeTimeout)
	defer cancel()

	select {
	case <-violations:
		return true, nil
	case <-waitCtx.Done():
		if ctxErr := ctx.Err(); ctxErr != nil {
			// The CALLER's context ended (not just our probe window): surface it
			// rather than falsely concluding over-scoped.
			return false, oops.Code("EVENTBUS_SCOPE_CHECK_FAILED").
				With("reason", "context cancelled during scope probe").
				Wrap(ctxErr)
		}
		// Probe window elapsed with no violation: the operation was PERMITTED.
		conn.Flush() //nolint:errcheck // best-effort flush; the probe window already elapsed
		return false, nil
	}
}

// overScoped builds the fail-closed error returned when a probe beyond the
// granted prefixes was permitted.
func overScoped(probe, op string) error {
	return oops.Code("EVENTBUS_ACCOUNT_OVERSCOPED").
		With("probe", probe).
		With("op", op).
		Errorf("server NATS account can %s outside the granted game-topic prefixes (over-scoped); refusing to boot", op)
}
