// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"connectrpc.com/connect"
	"github.com/samber/oops"
	"github.com/spf13/cobra"
	"golang.org/x/term"

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
// this type to assign sysexits.h exit codes (INV-CRYPTO-110).
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

	return mapToExitCodeErr(streamProgress(stream, noProgress, cmd.OutOrStdout()))
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

// mapErrToExitCodeForTest maps any error carrying a known oops code or
// rekeyProgressError code to its sysexits.h integer (INV-CRYPTO-110).  Returns 1
// for unknown errors.  This testable helper is callable from tests so the
// exit-code logic is exercisable without invoking os.Exit.
func mapErrToExitCodeForTest(err error) int {
	if err == nil {
		return 0
	}
	// Extract code string — try rekeyProgressError first, then oops.
	code := ""
	var pe *rekeyProgressError
	if errors.As(err, &pe) {
		code = pe.code
	} else if oe, ok := oops.AsOops(err); ok {
		if s, isStr := oe.Code().(string); isStr {
			code = s
		}
	}
	switch code {
	case "DEK_REKEY_PHASE5_TIMEOUT":
		return 75 // EX_TEMPFAIL
	case "DEK_REKEY_ALREADY_IN_PROGRESS", "DEK_REKEY_ARGS_CONFLICT":
		return 73 // EX_CANTCREAT
	case "DEK_REKEY_PHASE7_AUDIT_FAILED":
		return 70 // EX_SOFTWARE
	case "DENY_SESSION_INVALID", "DENY_SESSION_EXPIRED", "DENY_CAPABILITY":
		return 77 // EX_NOPERM
	}
	return 1
}

// mapToExitCodeErr maps a rekey error to an exitCodeError carrying the
// appropriate sysexits.h code per INV-CRYPTO-110.  Unknown errors pass through
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

// --- Sub-subcommands ---

// newRekeyResumeCmd returns `holomush crypto rekey resume <request_id>`.
// --force-destroy bypasses Phase 5 cluster invalidation (INV-CRYPTO-97 gates this
// server-side: checkpoint must be at phase5_timeout).  In non-TTY mode the
// --confirm <context_id> flag is required; missing or empty value exits 64
// (EX_USAGE).  In TTY mode the operator is prompted interactively.
func newRekeyResumeCmd(factory adminClientFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resume <request_id>",
		Short: "Resume an in-flight rekey checkpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRekeyResume(cmd, factory, args[0])
		},
	}
	cmd.Flags().Bool("force-destroy", false, "Bypass Phase 5 cluster invalidation (DESTRUCTIVE — requires phase5_timeout status)")
	cmd.Flags().String("confirm", "", "Required in non-TTY mode with --force-destroy: context_id confirmation token (e.g. scene:01ABC)")
	return cmd
}

// runRekeyResume is the testable core of `holomush crypto rekey resume`.
// It handles --force-destroy confirmation (TTY or --confirm), authenticates,
// and calls the RekeyResume streaming RPC.
func runRekeyResume(cmd *cobra.Command, factory adminClientFactory, requestIDStr string) error {
	forceDestroy, _ := cmd.Flags().GetBool("force-destroy") //nolint:errcheck // flag defined in newRekeyResumeCmd; absence is a programmer error
	confirmFlag, _ := cmd.Flags().GetString("confirm")      //nolint:errcheck // flag defined in newRekeyResumeCmd; absence is a programmer error

	// Parse request_id before touching network.
	requestIDBytes, err := parseRequestID(requestIDStr)
	if err != nil {
		return oops.Code("CRYPTO_REKEY_RESUME_INVALID_REQUEST_ID").Wrap(err)
	}

	client, err := factory()
	if err != nil {
		return oops.Code("CRYPTO_REKEY_RESUME_CLIENT_FAILED").Wrap(err)
	}

	sessionToken, err := authenticateInteractive(cmd.Context(), client, cmd)
	if err != nil {
		return oops.Code("CRYPTO_REKEY_RESUME_AUTH_FAILED").Wrap(err)
	}

	if forceDestroy {
		in := cmd.InOrStdin()
		isTTY := func() bool {
			if f, ok := in.(*os.File); ok {
				return term.IsTerminal(int(f.Fd())) //nolint:gosec // G115: stdin fd is small and platform-bounded; safe
			}
			return false
		}()

		// Short-circuit: non-TTY without --confirm is an immediate usage error;
		// don't make a network call to learn that.
		if !isTTY && confirmFlag == "" {
			return &exitCodeError{
				exitCode: 64,
				cause:    fmt.Errorf("--confirm required in non-TTY mode: provide --confirm <context_type>:<context_id> for --force-destroy"),
			}
		}

		// Fetch the checkpoint's context_type:context_id so the confirmation
		// gate verifies operator intent against a value they cannot trivially
		// guess from the request_id alone.
		statusRes, statusErr := client.RekeyStatus(cmd.Context(), connect.NewRequest(&adminv1.RekeyStatusRequest{
			SessionToken: sessionToken,
			RequestId:    requestIDBytes,
		}))
		if statusErr != nil {
			return mapToExitCodeErr(oops.Code("CRYPTO_REKEY_RESUME_STATUS_LOOKUP_FAILED").Wrap(statusErr))
		}
		expected := fmt.Sprintf("%s:%s", statusRes.Msg.GetContextType(), statusRes.Msg.GetContextId())

		if !isTTY {
			if confirmFlag != expected {
				return &exitCodeError{
					exitCode: 64,
					cause:    fmt.Errorf("--confirm must equal the checkpoint's context (expected %q for --force-destroy)", expected),
				}
			}
		} else {
			typed, promptErr := promptForceDestroyConfirm(in, cmd.OutOrStdout(), expected)
			if promptErr != nil {
				return &exitCodeError{exitCode: 64, cause: promptErr}
			}
			if typed != expected {
				return &exitCodeError{
					exitCode: 64,
					cause:    fmt.Errorf("force-destroy confirmation does not match checkpoint context %q", expected),
				}
			}
		}
	}

	stream, err := client.RekeyResume(cmd.Context(), connect.NewRequest(&adminv1.RekeyResumeRequest{
		SessionToken: sessionToken,
		RequestId:    requestIDBytes,
		ForceDestroy: forceDestroy,
	}))
	if err != nil {
		return mapToExitCodeErr(oops.Code("CRYPTO_REKEY_RESUME_RPC_FAILED").Wrap(err))
	}

	return mapToExitCodeErr(streamProgress(stream, false, cmd.OutOrStdout()))
}

// promptForceDestroyConfirm prints the force-destroy warning to w and reads
// the operator's typed context confirmation from r. The expected value is
// "<context_type>:<context_id>" derived from RekeyStatus. Returns the typed
// value (caller compares to expected), or an error if the operator abandons
// the prompt.
//
// This function is exercised only on interactive TTY paths; non-TTY callers
// use the --confirm flag directly.
func promptForceDestroyConfirm(r io.Reader, w io.Writer, expected string) (string, error) {
	fmt.Fprintf(w, "\n⚠  DESTRUCTIVE: --force-destroy bypasses Phase 5 cluster invalidation.\n") //nolint:errcheck // prompt output; write failure is non-fatal
	fmt.Fprintf(w, "   Replicas with stale DEK caches will return DEK_NOT_FOUND on cache\n")     //nolint:errcheck // prompt output; write failure is non-fatal
	fmt.Fprintf(w, "   miss until they restart and resync. This event will be recorded\n")       //nolint:errcheck // prompt output; write failure is non-fatal
	fmt.Fprintf(w, "   in the rekey audit chain with force_destroy=true.\n\n")                   //nolint:errcheck // prompt output; write failure is non-fatal
	fmt.Fprintf(w, "   Type %q to confirm: ", expected)                                          //nolint:errcheck // prompt output; write failure is non-fatal

	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("reading confirmation: %w", err)
		}
		return "", fmt.Errorf("no confirmation input received")
	}
	typed := strings.TrimSpace(scanner.Text())
	if typed == "" {
		return "", fmt.Errorf("confirmation cancelled: empty input")
	}
	return typed, nil
}

// --- Sub-subcommands: abort, status, list ---

// newRekeyAbortCmd returns `holomush crypto rekey abort <request_id>`.
// Authenticates the operator and calls the RekeyAbort unary RPC.
func newRekeyAbortCmd(factory adminClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "abort <request_id>",
		Short: "Abort an in-flight rekey",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRekeyAbort(cmd, factory, args[0])
		},
	}
}

// runRekeyAbort is the testable core of `holomush crypto rekey abort`.
// It parses the request_id, authenticates, and calls RekeyAbort.
func runRekeyAbort(cmd *cobra.Command, factory adminClientFactory, requestIDStr string) error {
	requestIDBytes, err := parseRequestID(requestIDStr)
	if err != nil {
		return oops.Code("CRYPTO_REKEY_ABORT_INVALID_REQUEST_ID").Wrap(err)
	}
	client, err := factory()
	if err != nil {
		return oops.Code("CRYPTO_REKEY_ABORT_CLIENT_FAILED").Wrap(err)
	}
	sessionToken, err := authenticateInteractive(cmd.Context(), client, cmd)
	if err != nil {
		return oops.Code("CRYPTO_REKEY_ABORT_AUTH_FAILED").Wrap(err)
	}
	res, err := client.RekeyAbort(cmd.Context(), connect.NewRequest(&adminv1.RekeyAbortRequest{
		SessionToken: sessionToken,
		RequestId:    requestIDBytes,
	}))
	if err != nil {
		return oops.Code("CRYPTO_REKEY_ABORT_RPC_FAILED").Wrap(err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Aborted at %s; audit event id=%x\n", //nolint:errcheck // terminal success line; write errors non-fatal
		res.Msg.GetAbortedAt().AsTime().UTC().Format("2006-01-02T15:04:05Z"),
		res.Msg.GetAuditEventId())
	return nil
}

// newRekeyStatusCmd returns `holomush crypto rekey status <request_id>`.
// Authenticates the operator and calls the RekeyStatus unary RPC.
func newRekeyStatusCmd(factory adminClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "status <request_id>",
		Short: "Show rekey checkpoint details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRekeyStatus(cmd, factory, args[0])
		},
	}
}

// runRekeyStatus is the testable core of `holomush crypto rekey status`.
// It parses the request_id, authenticates, and calls RekeyStatus.
func runRekeyStatus(cmd *cobra.Command, factory adminClientFactory, requestIDStr string) error {
	requestIDBytes, err := parseRequestID(requestIDStr)
	if err != nil {
		return oops.Code("CRYPTO_REKEY_STATUS_INVALID_REQUEST_ID").Wrap(err)
	}
	client, err := factory()
	if err != nil {
		return oops.Code("CRYPTO_REKEY_STATUS_CLIENT_FAILED").Wrap(err)
	}
	sessionToken, err := authenticateInteractive(cmd.Context(), client, cmd)
	if err != nil {
		return oops.Code("CRYPTO_REKEY_STATUS_AUTH_FAILED").Wrap(err)
	}
	res, err := client.RekeyStatus(cmd.Context(), connect.NewRequest(&adminv1.RekeyStatusRequest{
		SessionToken: sessionToken,
		RequestId:    requestIDBytes,
	}))
	if err != nil {
		return oops.Code("CRYPTO_REKEY_STATUS_RPC_FAILED").Wrap(err)
	}
	printRekeyStatus(cmd.OutOrStdout(), res.Msg)
	return nil
}

// printRekeyStatus renders a RekeyStatusResponse to w in human-readable form.
func printRekeyStatus(w io.Writer, r *adminv1.RekeyStatusResponse) {
	fmt.Fprintf(w, "request_id:    %x\n", r.GetRequestId())                        //nolint:errcheck // display output; write errors non-fatal
	fmt.Fprintf(w, "context:       %s:%s\n", r.GetContextType(), r.GetContextId()) //nolint:errcheck // display output; write errors non-fatal
	fmt.Fprintf(w, "status:        %s\n", r.GetStatus())                           //nolint:errcheck // display output; write errors non-fatal
	if r.GetForceDestroy() {
		fmt.Fprintf(w, "force_destroy: true\n") //nolint:errcheck // display output; write errors non-fatal
	}
	if len(r.GetPhase5MissingMembers()) > 0 {
		fmt.Fprintf(w, "missing:       %s\n", strings.Join(r.GetPhase5MissingMembers(), ", ")) //nolint:errcheck // display output; write errors non-fatal
	}
}

// newRekeyListCmd returns `holomush crypto rekey list [--include-terminal] [--context <pattern>]`.
// Authenticates the operator and streams RekeyStatusResponse rows from RekeyList.
func newRekeyListCmd(factory adminClientFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List rekey checkpoints",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRekeyList(cmd, factory)
		},
	}
	cmd.Flags().Bool("include-terminal", false, "Include complete/aborted checkpoints")
	cmd.Flags().String("context", "", "Filter by context-id LIKE pattern")
	return cmd
}

// runRekeyList is the testable core of `holomush crypto rekey list`.
// It authenticates, issues a streaming RekeyList RPC, and renders each row.
func runRekeyList(cmd *cobra.Command, factory adminClientFactory) error {
	includeTerminal, _ := cmd.Flags().GetBool("include-terminal") //nolint:errcheck // flag defined in newRekeyListCmd; absence is a programmer error
	ctxPattern, _ := cmd.Flags().GetString("context")             //nolint:errcheck // flag defined in newRekeyListCmd; absence is a programmer error

	client, err := factory()
	if err != nil {
		return oops.Code("CRYPTO_REKEY_LIST_CLIENT_FAILED").Wrap(err)
	}
	sessionToken, err := authenticateInteractive(cmd.Context(), client, cmd)
	if err != nil {
		return oops.Code("CRYPTO_REKEY_LIST_AUTH_FAILED").Wrap(err)
	}

	req := &adminv1.RekeyListRequest{
		SessionToken:    sessionToken,
		IncludeTerminal: includeTerminal,
	}
	if ctxPattern != "" {
		req.ContextPattern = &ctxPattern
	}

	stream, err := client.RekeyList(cmd.Context(), connect.NewRequest(req))
	if err != nil {
		return oops.Code("CRYPTO_REKEY_LIST_RPC_FAILED").Wrap(err)
	}

	w := cmd.OutOrStdout()
	printRekeyListHeader(w)
	for stream.Receive() {
		printRekeyListRow(w, stream.Msg())
	}
	if err := stream.Err(); err != nil {
		return oops.Code("CRYPTO_REKEY_LIST_STREAM_FAILED").Wrap(err)
	}
	return nil
}

// printRekeyListHeader prints the column header for rekey list output.
func printRekeyListHeader(w io.Writer) {
	fmt.Fprintf(w, "%-32s  %-16s  %-24s  %s\n", "REQUEST_ID", "CONTEXT", "STATUS", "STARTED_AT") //nolint:errcheck // display output; write errors non-fatal
}

// printRekeyListRow prints a single RekeyStatusResponse row.
func printRekeyListRow(w io.Writer, r *adminv1.RekeyStatusResponse) {
	contextStr := r.GetContextType() + ":" + r.GetContextId()
	startedAt := ""
	if ts := r.GetStartedAt(); ts != nil {
		startedAt = ts.AsTime().UTC().Format("2006-01-02T15:04:05Z")
	}
	fmt.Fprintf(w, "%-32x  %-16s  %-24s  %s\n", r.GetRequestId(), contextStr, r.GetStatus(), startedAt) //nolint:errcheck // display output; write errors non-fatal
}
