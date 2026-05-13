// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/samber/oops"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// newAdminReadStreamCmd returns the `holomush admin read-stream` subcommand.
// It dials the admin UDS socket, authenticates the operator, and streams
// EventFrame payloads from the server-side AdminReadStream RPC.
func newAdminReadStreamCmd(factory adminClientFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read-stream",
		Short: "Operator break-glass read of the event stream (admin UDS, host-shell only)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAdminReadStream(cmd, factory)
		},
	}

	cmd.Flags().String("justification", "", "Required: free-text reason for the read")
	if err := cmd.MarkFlagRequired("justification"); err != nil {
		// MarkFlagRequired only fails if the flag name is wrong — programmer error.
		panic(fmt.Sprintf("cmd_admin_read_stream: MarkFlagRequired: %v", err))
	}
	cmd.Flags().StringArray("context", nil, "Context ref in type:id1[:id2] format (repeatable)")
	cmd.Flags().String("since", "", "RFC3339 start of window (default: server-chosen)")
	cmd.Flags().String("until", "", "RFC3339 end of window (default: server-chosen)")
	cmd.Flags().Bool("dual-control", false, "Require second-operator approval before streaming begins")
	cmd.Flags().String("output", "text", "Output format: text or json")

	return cmd
}

// runAdminReadStream is the testable core of `admin read-stream`.  It builds
// the request, dials, authenticates, streams responses, and returns an error
// carrying the appropriate sysexits.h code on failure.
func runAdminReadStream(cmd *cobra.Command, factory adminClientFactory) error {
	just, _ := cmd.Flags().GetString("justification") //nolint:errcheck // flag defined in newAdminReadStreamCmd; absence is a programmer error

	ctxStrs, _ := cmd.Flags().GetStringArray("context") //nolint:errcheck // flag defined in newAdminReadStreamCmd
	ctxRefs := make([]*adminv1.ContextRef, 0, len(ctxStrs))
	for _, s := range ctxStrs {
		ref, err := parseContextFlag(s)
		if err != nil {
			return &exitCodeError{
				exitCode: 64, // EX_USAGE
				cause:    oops.Code("ADMIN_READSTREAM_BAD_CONTEXT").With("input", s).Wrap(err),
			}
		}
		ctxRefs = append(ctxRefs, ref)
	}

	sinceStr, _ := cmd.Flags().GetString("since") //nolint:errcheck // flag defined in newAdminReadStreamCmd
	untilStr, _ := cmd.Flags().GetString("until")  //nolint:errcheck // flag defined in newAdminReadStreamCmd
	dualControl, _ := cmd.Flags().GetBool("dual-control") //nolint:errcheck // flag defined in newAdminReadStreamCmd
	output, _ := cmd.Flags().GetString("output") //nolint:errcheck // flag defined in newAdminReadStreamCmd

	req := &adminv1.AdminReadStreamRequest{
		Justification: just,
		Context:       ctxRefs,
		DualControl:   dualControl,
	}

	if sinceStr != "" {
		t, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			return &exitCodeError{
				exitCode: 64, // EX_USAGE
				cause:    oops.Code("ADMIN_READSTREAM_BAD_SINCE").Wrap(err),
			}
		}
		req.Since = timestamppb.New(t)
	}

	if untilStr != "" {
		t, err := time.Parse(time.RFC3339, untilStr)
		if err != nil {
			return &exitCodeError{
				exitCode: 64, // EX_USAGE
				cause:    oops.Code("ADMIN_READSTREAM_BAD_UNTIL").Wrap(err),
			}
		}
		req.Until = timestamppb.New(t)
	}

	client, err := factory()
	if err != nil {
		return oops.Code("ADMIN_READSTREAM_CLIENT_FAILED").Wrap(err)
	}

	sessionToken, err := authenticateInteractive(cmd.Context(), client, cmd)
	if err != nil {
		return oops.Code("ADMIN_READSTREAM_AUTH_FAILED").Wrap(err)
	}
	req.SessionToken = sessionToken

	stream, err := client.AdminReadStream(cmd.Context(), connect.NewRequest(req))
	if err != nil {
		return &exitCodeError{
			exitCode: exitCodeForError(err),
			cause:    err,
		}
	}

	stdout := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()

	var terminatedBy adminv1.ReadFinished_TerminatedBy
	for stream.Receive() {
		msg := stream.Msg()
		renderFrame(msg, output, stdout, stderr)
		if f := msg.GetFinished(); f != nil {
			terminatedBy = f.GetTerminatedBy()
			code := exitCodeForTerminatedBy(terminatedBy)
			if code != 0 {
				return &exitCodeError{
					exitCode: code,
					cause:    fmt.Errorf("stream terminated: %s", terminatedBy),
				}
			}
			return nil
		}
	}
	if err := stream.Err(); err != nil {
		return &exitCodeError{
			exitCode: exitCodeForError(err),
			cause:    err,
		}
	}
	// Stream ended without a ReadFinished frame — treat as clean if no error.
	return nil
}

// parseContextFlag converts "scene:01H..." → &ContextRef{Type:"scene", IDs:["01H..."]}.
// The first ':'-delimited token is the type; the rest are IDs.
// Returns an error for empty strings or strings without a colon.
func parseContextFlag(s string) (*adminv1.ContextRef, error) {
	if s == "" {
		return nil, fmt.Errorf("context flag must not be empty")
	}
	idx := strings.Index(s, ":")
	if idx < 0 {
		return nil, fmt.Errorf("context flag %q must be in <type>:<id>[:<id>...] form", s)
	}
	typ := s[:idx]
	rest := s[idx+1:]
	if typ == "" {
		return nil, fmt.Errorf("context flag %q: type token must not be empty", s)
	}
	if rest == "" {
		return nil, fmt.Errorf("context flag %q: at least one id is required", s)
	}
	ids := strings.Split(rest, ":")
	return &adminv1.ContextRef{
		Type: typ,
		Ids:  ids,
	}, nil
}

// exitCodeForError maps oops error codes (deep-extracted) to sysexits.h codes.
// Returns 70 (EX_SOFTWARE) for unrecognised errors.
func exitCodeForError(err error) int {
	if err == nil {
		return 0
	}
	code := ""
	if oe, ok := oops.AsOops(err); ok {
		if s, isStr := oe.Code().(string); isStr {
			code = s
		}
	}
	switch code {
	case "DENY_OPERATOR_READ_WINDOW_TOO_LARGE",
		"DENY_OPERATOR_READ_TIME_INVERTED",
		"DENY_OPERATOR_READ_JUSTIFICATION_EMPTY",
		"DENY_OPERATOR_READ_JUSTIFICATION_TOO_LONG":
		return 65 // EX_DATAERR

	case "DENY_AUDIT_PRE_DATA_PUBLISH":
		return 70 // EX_SOFTWARE

	case "DENY_OPERATOR_CAPABILITY":
		return 77 // EX_NOPERM

	case "READSTREAM_DEADLINE_EXCEEDED",
		"READSTREAM_DUAL_CONTROL_TIMEOUT":
		return 75 // EX_TEMPFAIL

	case "DENY_SESSION_INVALID":
		return 77 // EX_NOPERM
	}
	// Match any DENY_OPERATOR_READ_CONTEXT_* prefix.
	if strings.HasPrefix(code, "DENY_OPERATOR_READ_CONTEXT_") {
		return 65 // EX_DATAERR
	}
	return 70 // EX_SOFTWARE (default)
}

// exitCodeForTerminatedBy maps a ReadFinished_TerminatedBy value to a
// sysexits.h exit code.  Client-initiated terminates (EOF, disconnect) are
// clean exits (0); server-side failures carry non-zero codes.
func exitCodeForTerminatedBy(t adminv1.ReadFinished_TerminatedBy) int {
	switch t {
	case adminv1.ReadFinished_TERMINATED_BY_CLIENT_EOF,
		adminv1.ReadFinished_TERMINATED_BY_CLIENT_DISCONNECT:
		return 0

	case adminv1.ReadFinished_TERMINATED_BY_DEADLINE_EXCEEDED:
		return 75 // EX_TEMPFAIL

	case adminv1.ReadFinished_TERMINATED_BY_SERVER_ERROR:
		return 70 // EX_SOFTWARE

	case adminv1.ReadFinished_TERMINATED_BY_DUAL_CONTROL_TIMEOUT:
		return 75 // EX_TEMPFAIL

	case adminv1.ReadFinished_TERMINATED_BY_AUDIT_EMIT_FAILURE:
		return 70 // EX_SOFTWARE

	default: // TERMINATED_BY_UNSPECIFIED and any future unknown values
		return 70 // EX_SOFTWARE
	}
}

// renderFrame prints one AdminReadStreamResponse frame to stdout/stderr.
//
//   - PendingApproval → stderr: "pending: request_id=... expires_at=..."
//   - ReadStarted     → stderr: "started: request_id=... policy_hash=... window=[since,until] contexts=..."
//   - Event (corev1.EventFrame) →
//     if metadata_only: stdout: "[redacted: <reason>] subject=... type=... timestamp=..."
//     else: stdout: <payload> + metadata line
//   - ReadFinished    → stderr: "finished: terminated_by=... events_scanned=... decrypt_fail_count=..."
func renderFrame(frame *adminv1.AdminReadStreamResponse, _ string, stdout, stderr io.Writer) {
	switch {
	case frame.GetPendingApproval() != nil:
		pa := frame.GetPendingApproval()
		expiresAt := ""
		if pa.GetExpiresAt() != nil {
			expiresAt = pa.GetExpiresAt().AsTime().UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(stderr, "pending: request_id=%x expires_at=%s\n", //nolint:errcheck // status output; write errors non-fatal
			pa.GetRequestId(), expiresAt)

	case frame.GetStarted() != nil:
		s := frame.GetStarted()
		since, until := "", ""
		if s.GetResolvedSince() != nil {
			since = s.GetResolvedSince().AsTime().UTC().Format(time.RFC3339)
		}
		if s.GetResolvedUntil() != nil {
			until = s.GetResolvedUntil().AsTime().UTC().Format(time.RFC3339)
		}
		ctxStrs := make([]string, 0, len(s.GetResolvedContexts()))
		for _, c := range s.GetResolvedContexts() {
			ctxStrs = append(ctxStrs, c.GetType()+":"+strings.Join(c.GetIds(), ":"))
		}
		fmt.Fprintf(stderr, "started: request_id=%s policy_hash=%s window=[%s,%s] contexts=%s\n", //nolint:errcheck // status output; write errors non-fatal
			s.GetRequestId(),
			hex.EncodeToString(s.GetPolicyHash()),
			since, until,
			strings.Join(ctxStrs, " "))

	case frame.GetEvent() != nil:
		ef := frame.GetEvent()
		renderEventFrame(ef, stdout)

	case frame.GetFinished() != nil:
		f := frame.GetFinished()
		fmt.Fprintf(stderr, "finished: terminated_by=%s events_scanned=%d decrypt_fail_count=%d\n", //nolint:errcheck // status output; write errors non-fatal
			f.GetTerminatedBy(),
			f.GetEventsScanned(),
			f.GetDecryptFailCount())
	}
}

// renderEventFrame renders a single corev1.EventFrame to stdout.
// Uses typed fields directly — no Payload-length heuristic.
func renderEventFrame(ef *corev1.EventFrame, stdout io.Writer) {
	ts := ""
	if ef.GetTimestamp() != nil {
		ts = ef.GetTimestamp().AsTime().UTC().Format(time.RFC3339Nano)
	}

	if ef.GetMetadataOnly() {
		// Typed redaction: read no_plaintext_reason directly from proto field.
		reason := ef.GetNoPlaintextReason().String()
		fmt.Fprintf(stdout, "[redacted: %s] subject=%s type=%s timestamp=%s\n", //nolint:errcheck // event output; write errors non-fatal
			reason,
			ef.GetStream(),
			ef.GetType(),
			ts)
		return
	}

	// Plaintext delivery.
	fmt.Fprintf(stdout, "%s", ef.GetPayload())             //nolint:errcheck // event payload; write errors non-fatal
	if len(ef.GetPayload()) > 0 && ef.GetPayload()[len(ef.GetPayload())-1] != '\n' {
		fmt.Fprintln(stdout) //nolint:errcheck // ensure newline after payload
	}
	fmt.Fprintf(stdout, "  id=%s subject=%s type=%s timestamp=%s\n", //nolint:errcheck // event metadata; write errors non-fatal
		ef.GetId(), ef.GetStream(), ef.GetType(), ts)
}

// defaultAdminReadStreamClientFactory builds an adminClientFactory that reads
// --socket from the parent command at call time.
func defaultAdminReadStreamClientFactory(parent *cobra.Command) adminClientFactory {
	return func() (adminv1connect.AdminServiceClient, error) {
		socketPath := adminSocketPathFromConfig(parent)
		return adminClientFromSocket(socketPath), nil
	}
}
