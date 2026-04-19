// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/telemetry"
)

const (
	exporterScrapeTimeout  = 5 * time.Second
	exporterScrapeInterval = 25 * time.Millisecond
	natsReadyTimeout       = 10 * time.Second
)

// startMonitoredNATS boots an embedded NATS server with a random monitoring
// port and returns the bound monitor port. Uses DontListen=true for the
// client-facing listener (same pattern as the production subsystem), and
// HTTPPort=-1 to request a random monitor port we can read back via
// MonitorAddr(). Registers t.Cleanup for shutdown.
func startMonitoredNATS(t *testing.T) int {
	t.Helper()
	opts := &natsserver.Options{
		ServerName: "holomush-telemetry-test",
		DontListen: true,
		NoSigs:     true,
		// HTTPPort=-1 asks the server to pick a free port for the monitoring
		// HTTP endpoint. MonitorAddr() reads it back once bound.
		HTTPPort: -1,
		HTTPHost: "127.0.0.1",
	}
	srv, err := natsserver.NewServer(opts)
	require.NoError(t, err, "NewServer")
	go srv.Start()
	t.Cleanup(func() {
		srv.Shutdown()
		srv.WaitForShutdown()
	})
	require.True(t, srv.ReadyForConnections(natsReadyTimeout), "embedded NATS never became ready")

	addr := srv.MonitorAddr()
	require.NotNil(t, addr, "MonitorAddr must be non-nil when HTTPPort is set")
	require.Positive(t, addr.Port, "monitor port must be bound")
	return addr.Port
}

// freeLoopbackPort picks a free TCP port on 127.0.0.1 by briefly binding a
// listener and closing it. The returned port is used to start the exporter
// HTTP server so tests can scrape a known URL. There's a trivial TOCTOU
// window between Close() and the exporter's Listen(), but loopback port
// reuse rarely loses on CI runners, and we retry the scrape.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	return port
}

func TestStartNATSExporterServesMetrics(t *testing.T) {
	monitorPort := startMonitoredNATS(t)
	exporterPort := freeLoopbackPort(t)

	exp, err := telemetry.StartNATSExporter(monitorPort, exporterPort)
	require.NoError(t, err, "StartNATSExporter must succeed against a real embedded monitor port")
	t.Cleanup(func() { exp.Stop() })

	// The exporter spawns its HTTP server in a goroutine; poll the scrape
	// endpoint until it returns 200 OK carrying known NATS metric names.
	// time.Sleep is forbidden in this package — use time.After for pacing.
	url := fmt.Sprintf("http://127.0.0.1:%d/metrics", exporterPort)
	deadline := time.Now().Add(exporterScrapeTimeout)
	// We assert on two markers that must appear when GetVarz + GetJszFilter
	// are enabled: the varz core metric and the jetstream_server marker.
	// Pick stable series names that don't change across exporter versions.
	const varzMarker = "gnatsd_varz_start"
	const jszMarker = "jetstream_server_total_streams"
	var lastErr error
	var body string
	for time.Now().Before(deadline) {
		body, lastErr = scrape(t, url)
		if lastErr == nil && strings.Contains(body, varzMarker) && strings.Contains(body, jszMarker) {
			break
		}
		<-time.After(exporterScrapeInterval)
	}
	require.NoError(t, lastErr, "scrape exporter")
	assert.Contains(t, body, varzMarker,
		"exporter must expose varz metrics after Start")
	assert.Contains(t, body, jszMarker,
		"exporter must expose JetStream metrics when GetJszFilter=all")
}

func TestStartNATSExporterRejectsZeroMonitorPort(t *testing.T) {
	exp, err := telemetry.StartNATSExporter(0, 0)
	require.Error(t, err, "zero monitor port is a programmer error")
	assert.Nil(t, exp, "no exporter handle should be returned on validation failure")
	assert.Contains(t, err.Error(), "monitor port", "error message should be actionable")
}

func TestStartNATSExporterRejectsNegativeMonitorPort(t *testing.T) {
	exp, err := telemetry.StartNATSExporter(-1, 0)
	require.Error(t, err)
	assert.Nil(t, exp)
}

// scrape issues an HTTP GET against url with a short per-request timeout.
// Using NewRequestWithContext + validated-literal loopback URL avoids the
// gosec G107 "http with variable URL" false positive (the host is always
// 127.0.0.1 and the port came from startMonitoredNATS / freeLoopbackPort).
func scrape(t *testing.T, url string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), exporterScrapeInterval*4)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", err
	}
	// #nosec G107 -- URL host is always 127.0.0.1 on a port we just bound.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
