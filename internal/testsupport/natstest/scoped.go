// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package natstest

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Scoped-account credentials for the single application principal shipped in
// deploy/nats/cluster-server.conf — the "operational sibling" of
// holomush-server.account.conf that ALSO enables JetStream (store_dir) and
// grants the $JS.API.>/$JS.ACK.> subjects the server needs to declare and
// drain the EVENTS stream, so a full eventbus.Subsystem.Start (dial +
// EnsureStream + VerifyAccountScoping) succeeds against it — unlike the
// accounts-only proof template, which grants no JetStream API access. These
// are the file's documented smoke/dev placeholders (a real deploy uses
// nsc/JWT — see deploy/nats/README.md).
const (
	ScopedServerUser     = "holomush-server"
	ScopedServerPassword = "holomush-server-smoke"
)

// scopedServerConfPath resolves the shipped deploy/nats/cluster-server.conf's
// absolute host path via runtime.Caller, so StartScopedNATS mounts the REAL
// deploy artifact into the container rather than a test-local copy.
func scopedServerConfPath() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed to resolve natstest/scoped.go's path")
	}
	// internal/testsupport/natstest -> internal/testsupport -> internal -> repo root
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..",
		"deploy", "nats", "cluster-server.conf"), nil
}

// StartScopedNATS starts a single-node NATS JetStream container loading the
// shipped deploy/nats/cluster-server.conf (CLUSTER-02 single-principal
// subject scoping + JetStream enablement — the same config
// compose.cluster.yaml's nats service runs). The returned env's URL is a BARE
// connect URL (no credentials embedded) — dial with
// nats.UserInfo(ScopedServerUser, ScopedServerPassword), or embed "user:pass@"
// directly in a derived URL (natsdial.go's "user:pass-in-URL works
// implicitly" mechanism, matching deploy/nats/cluster-config.yaml's
// event_bus.url), to authenticate as the correctly-scoped server principal.
//
// Use this instead of StartNATS whenever a test needs a full
// eventbus.Subsystem.Start to succeed with Mode: ModeExternal —
// VerifyAccountScoping (07-09) now runs inside Subsystem.Start itself and
// refuses a plain unscoped node with EVENTBUS_ACCOUNT_OVERSCOPED (StartNATS's
// own doc: "provides transport only... does NOT provision streams or
// consumers" — nor accounts).
func StartScopedNATS(ctx context.Context) (*NATSEnv, error) {
	confPath, err := scopedServerConfPath()
	if err != nil {
		return nil, fmt.Errorf("resolve scoped server conf: %w", err)
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: natsImage,
			Files: []testcontainers.ContainerFile{
				{
					HostFilePath:      confPath,
					ContainerFilePath: "/etc/nats/cluster-server.conf",
					FileMode:          0o444,
				},
			},
			// JetStream store_dir is set in the config, so no -js/-sd flags are
			// needed (mirrors compose.cluster.yaml's nats service command).
			Cmd:          []string{"-c", "/etc/nats/cluster-server.conf"},
			ExposedPorts: []string{"4222/tcp"},
			WaitingFor: wait.ForAll(
				wait.ForListeningPort("4222/tcp"),
				wait.ForLog("Server is ready"),
			).WithDeadline(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		if container != nil {
			_ = container.Terminate(ctx) //nolint:errcheck // best-effort cleanup
		}
		return nil, fmt.Errorf("run scoped nats: %w", err)
	}

	url, err := clientURL(ctx, container)
	if err != nil {
		_ = container.Terminate(ctx) //nolint:errcheck // best-effort cleanup
		return nil, fmt.Errorf("resolve scoped nats client url: %w", err)
	}

	return &NATSEnv{Container: container, URL: url}, nil
}

// ScopedURL embeds the scoped server credentials into bareURL
// ("nats://host:port" -> "nats://holomush-server:holomush-server-smoke@host:port"),
// the "user:pass-in-URL works implicitly" mechanism natsdial.go documents —
// the same shape deploy/nats/cluster-config.yaml's event_bus.url uses.
func ScopedURL(bareURL string) string {
	const prefix = "nats://"
	if !strings.HasPrefix(bareURL, prefix) {
		return bareURL
	}
	return prefix + ScopedServerUser + ":" + ScopedServerPassword + "@" + strings.TrimPrefix(bareURL, prefix)
}
