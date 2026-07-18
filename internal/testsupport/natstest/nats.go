// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package natstest provides an external-NATS integration harness: a single
// real NATS JetStream node in an ephemeral testcontainer, handing each caller
// its OWN *nats.Conn.
//
// This is the substrate for external-mode (CLUSTER-01..04) integration proofs
// that the embedded eventbustest harness cannot express, because those proofs
// require N HoloMUSH replicas each with an INDEPENDENT connection to one real
// external broker — not the shared in-process connection eventbustest hands out.
// A single external node (not a NATS cluster) suffices: the invariants bind on
// HoloMUSH replica membership and N-of-N acks over subjects, not on NATS's own
// server-side replication (OQ-4).
//
// Production code MUST NOT import this package — it is test-support only, kept
// at depguard parity with eventbustest and quarantinetest.
package natstest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// natsImage is the single-node NATS server image. JetStream is enabled via the
// container command, not a testcontainers module — no NATS module is added to
// go.mod (OQ-4).
const natsImage = "nats:2-alpine"

// NATSEnv holds a running single-node NATS JetStream container and the resolved
// client URL. Callers dial their own connections via Conn.
type NATSEnv struct {
	// Container is the running NATS testcontainer.
	Container testcontainers.Container
	// URL is the dialable nats:// client URL (host + mapped 4222 port).
	URL string
}

// Terminate stops and removes the NATS container. Safe to call on a nil-field
// env (no-op).
func (e *NATSEnv) Terminate(ctx context.Context) error {
	if e.Container != nil {
		if err := e.Container.Terminate(ctx); err != nil {
			return fmt.Errorf("terminate nats container: %w", err)
		}
	}
	return nil
}

// Conn dials a NEW, independent connection to the container and registers its
// close on t.Cleanup. Each call yields a distinct *nats.Conn, so a test can
// build N replicas each with its own connection — the exact multi-node shape
// the shared in-process eventbustest connection cannot express.
func (e *NATSEnv) Conn(t testing.TB) *nats.Conn {
	t.Helper()
	conn, err := nats.Connect(e.URL)
	if err != nil {
		t.Fatalf("natstest: connect to %s: %v", e.URL, err)
	}
	t.Cleanup(conn.Close)
	return conn
}

// StartNATS starts a single-node NATS JetStream container and returns an env
// whose URL is dialable. It retries container start up to 3 times, reclaiming a
// half-started container between attempts so resource pressure does not
// compound (mirrors the postgres helper's reclaim-on-failure pattern).
//
// The helper provides transport only — it does NOT provision streams or
// consumers. Callers provision what they need over their own connections.
func StartNATS(ctx context.Context) (*NATSEnv, error) {
	var lastErr error
	for attempt := range 3 {
		env, err := startNATSOnce(ctx)
		if err == nil {
			return env, nil
		}
		lastErr = err
		if attempt == 2 {
			break
		}

		backoff := time.Duration(attempt+1) * 250 * time.Millisecond
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("start nats container: %w", ctx.Err())
		case <-timer.C:
		}
	}

	return nil, fmt.Errorf("start nats container: %w", lastErr)
}

func startNATSOnce(ctx context.Context) (*NATSEnv, error) {
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        natsImage,
			Cmd:          []string{"-js", "-sd", "/data"},
			ExposedPorts: []string{"4222/tcp"},
			WaitingFor: wait.ForAll(
				wait.ForListeningPort("4222/tcp"),
				wait.ForLog("Server is ready"),
			).WithDeadline(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		// testcontainers-go returns a non-nil container handle when the
		// wait-until-ready strategy fails; reclaim it before returning so the
		// retry does not pile a fresh container on a leaked half-started one.
		if container != nil {
			_ = container.Terminate(ctx) //nolint:errcheck // best-effort cleanup
		}
		return nil, fmt.Errorf("run nats: %w", err)
	}

	url, err := clientURL(ctx, container)
	if err != nil {
		_ = container.Terminate(ctx) //nolint:errcheck // best-effort cleanup
		return nil, fmt.Errorf("resolve nats client url: %w", err)
	}

	return &NATSEnv{Container: container, URL: url}, nil
}

func clientURL(ctx context.Context, container testcontainers.Container) (string, error) {
	host, err := container.Host(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve container host: %w", err)
	}

	port, err := container.MappedPort(ctx, "4222/tcp")
	if err != nil {
		return "", fmt.Errorf("resolve nats port: %w", err)
	}

	return fmt.Sprintf("nats://%s:%s", host, port.Port()), nil
}
