// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package resilience_test

import (
	"context"
	"testing"

	dockerclient "github.com/moby/moby/client"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/testsupport/natstest"
)

// startExternalNATS boots a fresh single-node NATS JetStream container and
// registers its teardown on the spec. Copied from the eventbus_external suite —
// the transport-only helper that hands each replica its own connection.
func startExternalNATS(ctx context.Context) *natstest.NATSEnv {
	env, err := natstest.StartNATS(ctx)
	Expect(err).NotTo(HaveOccurred(), "StartNATS should return a running container")
	DeferCleanup(func() {
		_ = env.Terminate(context.Background())
	})
	return env
}

// startReplica boots one in-process CoreServer replica against the shared broker
// at url. Replica 1 passes connStr="" so it CREATES the fresh per-test database;
// replica 2+ passes replica 1's ConnStr() so it JOINS the same database. extra
// options compose after the external-NATS + shared-DB seams (e.g. a suite could
// add WithInTreePlugins for a heavier scenario).
func startReplica(t *testing.T, url, connStr string, extra ...integrationtest.StartOption) *integrationtest.Server {
	t.Helper()
	opts := []integrationtest.StartOption{integrationtest.WithExternalNATS(url)}
	if connStr != "" {
		opts = append(opts, integrationtest.WithSharedDatabase(connStr))
	}
	opts = append(opts, extra...)
	return integrationtest.Start(t, opts...)
}

// pauseBroker freezes the NATS container with `docker pause` — SIGSTOP on every
// process in the container. Networking stays intact and the mapped port is
// stable, so replicas' connections are not dropped: publishes and acks simply
// stall until unpauseBroker resumes the process. This is the primary broker-flap
// primitive for the dual-write window experiment (plan 03); Stop/Start restart
// fidelity is a later experiment. Verified against pinned testcontainers-go
// v0.43.0 (DockerClient embeds *docker/client.Client, exposing ContainerPause).
func pauseBroker(ctx context.Context, env *natstest.NATSEnv) {
	GinkgoHelper()
	cli, err := testcontainers.NewDockerClientWithOpts(ctx)
	Expect(err).NotTo(HaveOccurred(), "pauseBroker: docker client")
	Expect(cli.ContainerPause(ctx, env.Container.GetContainerID(), dockerclient.ContainerPauseOptions{})).
		To(Succeed(), "pauseBroker: ContainerPause")
}

// unpauseBroker resumes a container frozen by pauseBroker (`docker unpause` —
// SIGCONT). Paired with pauseBroker around the window under test.
func unpauseBroker(ctx context.Context, env *natstest.NATSEnv) {
	GinkgoHelper()
	cli, err := testcontainers.NewDockerClientWithOpts(ctx)
	Expect(err).NotTo(HaveOccurred(), "unpauseBroker: docker client")
	Expect(cli.ContainerUnpause(ctx, env.Container.GetContainerID(), dockerclient.ContainerUnpauseOptions{})).
		To(Succeed(), "unpauseBroker: ContainerUnpause")
}
