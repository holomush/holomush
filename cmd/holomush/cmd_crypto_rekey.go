// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"connectrpc.com/connect"
	"github.com/samber/oops"
	"github.com/spf13/cobra"

	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"
)

// adminClientFactory returns an AdminServiceClient.  In production the
// implementation dials the admin UDS socket.  In tests it returns a
// pre-built httptest client.
type adminClientFactory func() (adminv1connect.AdminServiceClient, error)

// RekeyStreamReader is the narrow streaming interface consumed by streamProgress.
// *connect.ServerStreamForClient[adminv1.RekeyProgress] satisfies this interface.
type RekeyStreamReader interface {
	Receive() bool
	Msg() *adminv1.RekeyProgress
	Err() error
}

// rekeyProgressError is a typed error that carries a server-sent error code
// and message from a RekeyError progress event.  mapToExitCodeErr inspects
// this type to assign sysexits.h exit codes (INV-E23).
type rekeyProgressError struct {
	code string
	msg  string
}

func (e *rekeyProgressError) Error() string {
	return fmt.Sprintf("%s: %s", e.code, e.msg)
}

func (e *rekeyProgressError) Is(target error) bool {
	t, ok := target.(*rekeyProgressError)
	return ok && t.code == e.code
}

// exitCodeError wraps an error and annotates it with a sysexits.h exit code.
// Cobra's RunE handler in NewCryptoCmd unwraps this to call os.Exit if needed;
// for now, cobra's default error handling will print it and exit 1.  Future
// wiring in bead .34 may inspect this type.
type exitCodeError struct {
	exitCode int
	cause    error
}

func (e *exitCodeError) Error() string { return e.cause.Error() }
func (e *exitCodeError) Unwrap() error { return e.cause }

// NewCryptoCmd returns the `holomush crypto` parent command.  All crypto
// operator subcommands live here; they communicate over D's admin UDS.
func NewCryptoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "crypto",
		Short: "Crypto operator commands (admin UDS, host-shell only)",
	}
	bindAdminSocketFlag(cmd)
	factory := defaultAdminClientFactory(cmd)
	cmd.AddCommand(newCryptoRekeyCmd(factory))
	return cmd
}

// defaultAdminClientFactory builds an adminClientFactory that reads the
// --socket flag from the parent command at call time.
func defaultAdminClientFactory(parent *cobra.Command) adminClientFactory {
	return func() (adminv1connect.AdminServiceClient, error) {
		socketPath := adminSocketPathFromConfig(parent)
		return adminClientFromSocket(socketPath), nil
	}
}

// newCryptoRekeyCmd returns the `holomush crypto rekey <ctx-type>:<ctx-id>`
// subcommand.  Sub-subcommands resume, abort, status, and list are stubs in
// this bead; full implementations arrive in beads .32 and .33.
func newCryptoRekeyCmd(client adminClientFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rekey <ctx-type>:<ctx-id>",
		Short: "Forcibly mint a new DEK for a context (destructive)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRekeyFresh(cmd, client, args[0])
		},
	}
	cmd.Flags().String("justification", "", "Required: free-text reason for the rekey")
	cmd.Flags().Bool("dual-control", false, "Require second-operator approval before proceeding")
	cmd.Flags().Bool("no-progress", false, "Suppress streaming progress output")

	// Sub-subcommands — stubs implemented in beads .32 and .33.
	cmd.AddCommand(newRekeyResumeCmd(client))
	cmd.AddCommand(newRekeyAbortCmd(client))
	cmd.AddCommand(newRekeyStatusCmd(client))
	cmd.AddCommand(newRekeyListCmd(client))
	return cmd
}

// runRekeyFresh is the testable core of `holomush crypto rekey`.  It validates
// arguments, authenticates the operator, optionally opens a dual-control
// approval, calls the Rekey streaming RPC, and renders the progress stream.
func runRekeyFresh(cmd *cobra.Command, factory adminClientFactory, ctxRef string) error {
	just, _ := cmd.Flags().GetString("justification") //nolint:errcheck // flag defined in newCryptoRekeyCmd; absence is a programmer error
	if just == "" {
		return oops.Code("EX_USAGE").Errorf("--justification is required")
	}

	parts := strings.SplitN(ctxRef, ":", 2)
	if len(parts) != 2 {
		return oops.Code("EX_USAGE").Errorf("context must be <type>:<id>, got %q", ctxRef)
	}
	ctxType, ctxID := parts[0], parts[1]

	dualControl, _ := cmd.Flags().GetBool("dual-control") //nolint:errcheck // flag defined in newCryptoRekeyCmd; absence is a programmer error
	noProgress, _ := cmd.Flags().GetBool("no-progress")   //nolint:errcheck // flag defined in newCryptoRekeyCmd; absence is a programmer error

	client, err := factory()
	if err != nil {
		return oops.Code("CRYPTO_REKEY_CLIENT_FAILED").Wrap(err)
	}

	sessionToken, err := authenticateInteractive(cmd.Context(), client, cmd)
	if err != nil {
		return oops.Code("CRYPTO_REKEY_AUTH_FAILED").Wrap(err)
	}

	var approvalRequestID *string
	if dualControl {
		id, aerr := openApprovalAndWait(cmd.Context(), client, sessionToken, ctxType, ctxID, just)
		if aerr != nil {
			return oops.Code("CRYPTO_REKEY_APPROVAL_FAILED").Wrap(aerr)
		}
		approvalRequestID = &id
	}

	stream, err := client.Rekey(cmd.Context(), connect.NewRequest(&adminv1.RekeyRequest{
		SessionToken:      sessionToken,
		ContextType:       ctxType,
		ContextId:         ctxID,
		Justification:     just,
		ApprovalRequestId: approvalRequestID,
	}))
	if err != nil {
		return mapToExitCodeErr(oops.Code("CRYPTO_REKEY_RPC_FAILED").Wrap(err))
	}

	return streamProgress(stream, noProgress, cmd.OutOrStdout())
}

// streamProgress reads RekeyProgress messages from stream and renders them to
// w.  It returns nil on a Completed event or a non-nil error on any
// RekeyError event or transport failure.
func streamProgress(stream RekeyStreamReader, noProgress bool, w io.Writer) error {
	for stream.Receive() {
		msg := stream.Msg()
		switch e := msg.Event.(type) {
		case *adminv1.RekeyProgress_PhaseStarted:
			if !noProgress {
				fmt.Fprintf(w, "  Phase %s started\n", e.PhaseStarted.GetPhase()) //nolint:errcheck // progress output; write errors are non-fatal
			}
		case *adminv1.RekeyProgress_Phase3Progress:
			if !noProgress {
				fmt.Fprintf(w, "  Phase 3: %d rows rewritten\n", e.Phase3Progress.GetRowsRewritten()) //nolint:errcheck // progress output; write errors are non-fatal
			}
		case *adminv1.RekeyProgress_Phase5Attempt:
			if !noProgress {
				fmt.Fprintf(w, "  Phase 5: attempt %d, missing: %s\n", //nolint:errcheck // progress output; write errors are non-fatal
					e.Phase5Attempt.GetAttemptCount(),
					strings.Join(e.Phase5Attempt.GetMissingMembers(), ", "))
			}
		case *adminv1.RekeyProgress_PhaseCompleted:
			if !noProgress {
				fmt.Fprintf(w, "  Phase %s completed\n", e.PhaseCompleted.GetPhase()) //nolint:errcheck // progress output; write errors are non-fatal
			}
		case *adminv1.RekeyProgress_Completed:
			fmt.Fprintf(w, "Rekey complete: request_id=%s duration=%dms\n", //nolint:errcheck // terminal success line; write errors non-fatal
				hex.EncodeToString(e.Completed.GetRequestId()),
				e.Completed.GetDurationMs())
			return nil
		case *adminv1.RekeyProgress_Error:
			return &rekeyProgressError{
				code: e.Error.GetCode(),
				msg:  e.Error.GetMessage(),
			}
		}
	}
	// Receive returned false — check for transport error.
	if err := stream.Err(); err != nil {
		return oops.Code("CRYPTO_REKEY_STREAM_FAILED").Wrap(err)
	}
	// Clean EOF without a Completed event is unexpected.
	return oops.Code("CRYPTO_REKEY_STREAM_ENDED").Errorf("stream ended without completion event")
}

// mapToExitCodeErr maps a rekey error to an exitCodeError carrying the
// appropriate sysexits.h code per INV-E23.  Unknown errors pass through
// unchanged.
func mapToExitCodeErr(err error) error {
	var pe *rekeyProgressError
	if !errors.As(err, &pe) {
		return err
	}
	switch pe.code {
	case "DEK_REKEY_PHASE5_TIMEOUT":
		return &exitCodeError{exitCode: 75, cause: pe} // EX_TEMPFAIL
	case "DEK_REKEY_ALREADY_IN_PROGRESS", "DEK_REKEY_ARGS_CONFLICT":
		return &exitCodeError{exitCode: 73, cause: pe} // EX_CANTCREAT
	case "DEK_REKEY_PHASE7_AUDIT_FAILED":
		return &exitCodeError{exitCode: 70, cause: pe} // EX_SOFTWARE
	case "DENY_SESSION_INVALID", "DENY_SESSION_EXPIRED", "DENY_CAPABILITY":
		return &exitCodeError{exitCode: 77, cause: pe} // EX_NOPERM
	}
	return err
}

// openApprovalAndWait opens a dual-control approval request and blocks until
// the second operator approves.  Returns the approval request ID on success.
// Stub in bead .31 — full implementation is in bead .34 (production wiring).
func openApprovalAndWait(
	_ context.Context,
	_ adminv1connect.AdminServiceClient,
	_, _, _, _ string,
) (string, error) {
	return "", oops.Code("CRYPTO_REKEY_DUAL_CONTROL_NOT_IMPLEMENTED").
		Errorf("dual-control not yet wired (bead .34)")
}

// --- Stub sub-subcommands (full implementations in beads .32 and .33) ---

// newRekeyResumeCmd is a stub for `holomush crypto rekey resume <request_id>`.
// Full implementation is in bead .32.
func newRekeyResumeCmd(_ adminClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "resume <request_id>",
		Short: "Resume an interrupted rekey (stub — bead .32)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			return oops.Code("NOT_IMPLEMENTED").Errorf("rekey resume not yet implemented (bead .32)")
		},
	}
}

// newRekeyAbortCmd is a stub for `holomush crypto rekey abort <request_id>`.
// Full implementation is in bead .33.
func newRekeyAbortCmd(_ adminClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "abort <request_id>",
		Short: "Abort an in-flight rekey (stub — bead .33)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			return oops.Code("NOT_IMPLEMENTED").Errorf("rekey abort not yet implemented (bead .33)")
		},
	}
}

// newRekeyStatusCmd is a stub for `holomush crypto rekey status <request_id>`.
// Full implementation is in bead .33.
func newRekeyStatusCmd(_ adminClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "status <request_id>",
		Short: "Show rekey checkpoint details (stub — bead .33)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			return oops.Code("NOT_IMPLEMENTED").Errorf("rekey status not yet implemented (bead .33)")
		},
	}
}

// newRekeyListCmd is a stub for `holomush crypto rekey list`.
// Full implementation is in bead .33.
func newRekeyListCmd(_ adminClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List rekey checkpoints (stub — bead .33)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return oops.Code("NOT_IMPLEMENTED").Errorf("rekey list not yet implemented (bead .33)")
		},
	}
}
