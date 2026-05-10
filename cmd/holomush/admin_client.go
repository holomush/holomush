// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"
	"github.com/samber/oops"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"

	"github.com/holomush/holomush/internal/xdg"
)

// adminSocketPathFromConfig returns the admin socket path from the --socket flag
// or falls back to xdg.RuntimeDir()/admin.sock.
func adminSocketPathFromConfig(cmd *cobra.Command) string {
	if f := cmd.Flags().Lookup("socket"); f != nil && f.Changed {
		return f.Value.String()
	}
	runtimeDir, err := xdg.RuntimeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(runtimeDir, "admin.sock")
}

// bindAdminSocketFlag registers a --socket flag on cmd so operators can
// override the default UDS path.
func bindAdminSocketFlag(cmd *cobra.Command) {
	cmd.Flags().String("socket", "", "admin socket path (default: XDG runtime dir / admin.sock)")
}

// adminClientFromSocket returns a ConnectRPC AdminServiceClient whose HTTP
// transport dials the UDS at socketPath.
func adminClientFromSocket(socketPath string) adminv1connect.AdminServiceClient {
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}
	return adminv1connect.NewAdminServiceClient(httpClient, "http://admin")
}

// authenticateInteractive prompts for operator username, password (silent on
// TTY), and TOTP code, then calls Authenticate. Returns the session token.
//
// All three reads share a single bufio.Reader so buffered-ahead data from the
// first read is available to subsequent reads. (Creating a new bufio.Reader
// per-prompt would consume data from the underlying Reader into each buffer,
// leaving the next prompt with EOF from the exhausted wire reader.)
func authenticateInteractive(
	ctx context.Context,
	client adminv1connect.AdminServiceClient,
	cmd *cobra.Command,
) (string, error) {
	in := cmd.InOrStdin()
	out := cmd.OutOrStdout()

	// Single shared buffered reader — all three prompts draw from this.
	r := bufio.NewReader(in)

	username, err := adminPromptLine(r, out, "Operator username: ")
	if err != nil {
		return "", oops.Code("ADMIN_AUTH_PROMPT_USERNAME_FAILED").Wrap(err)
	}

	password, err := adminReadPassword(in, r, out, "password: ")
	if err != nil {
		return "", oops.Code("ADMIN_AUTH_PROMPT_PASSWORD_FAILED").Wrap(err)
	}

	totpCode, err := adminPromptLine(r, out, "TOTP code: ")
	if err != nil {
		return "", oops.Code("ADMIN_AUTH_PROMPT_TOTP_FAILED").Wrap(err)
	}

	resp, err := client.Authenticate(ctx, connect.NewRequest(&adminv1.AuthenticateRequest{
		Username: username,
		Password: password,
		TotpCode: totpCode,
	}))
	if err != nil {
		return "", err // bubble connect.Error; caller renders
	}
	return resp.Msg.GetSessionToken(), nil
}

// adminPromptLine prints label then reads one line from r (bufio.Reader).
// Tolerates EOF-with-partial-data (piped stdin without trailing newline).
func adminPromptLine(r *bufio.Reader, out io.Writer, label string) (string, error) {
	if _, err := fmt.Fprint(out, label); err != nil {
		return "", err
	}
	line, err := r.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// adminReadPassword reads a secret. If in is a real TTY file, reads without
// echo via term.ReadPassword. Otherwise falls back to the shared bufio.Reader
// r (which already owns the buffered stream, so reading from in directly would
// re-read from the wrong position).
func adminReadPassword(in io.Reader, r *bufio.Reader, out io.Writer, prompt string) (string, error) {
	if _, err := fmt.Fprint(out, prompt); err != nil {
		return "", err
	}
	if inFile, ok := in.(*os.File); ok {
		fd := int(inFile.Fd()) //nolint:gosec // G115: stdin fd is bounded; conversion safe
		if term.IsTerminal(fd) {
			buf, err := term.ReadPassword(fd)
			if err != nil {
				return "", err
			}
			if _, werr := fmt.Fprintln(out); werr != nil {
				return "", werr
			}
			return string(buf), nil
		}
	}
	// Non-TTY: read from the shared bufio.Reader so we don't lose buffered data.
	line, err := r.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
