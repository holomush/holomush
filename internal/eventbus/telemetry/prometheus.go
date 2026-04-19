// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"net/url"
	"strconv"

	"github.com/nats-io/prometheus-nats-exporter/exporter"
	"github.com/samber/oops"
)

const (
	// exporterServerID is the label the exporter attaches to scraped
	// metrics for our embedded server. It must be stable so Prometheus
	// series don't churn across restarts.
	exporterServerID = "holomush-embedded"

	// exporterJszAll scrapes all JetStream metrics (stream + consumer +
	// account). The exporter treats an empty filter as "disabled".
	exporterJszAll = "all"

	// exporterLoopbackAddr binds the scrape endpoint to loopback only —
	// the embedded exporter is meant for local Prometheus scrapes from
	// the same host (sidecar or node exporter pattern).
	exporterLoopbackAddr = "127.0.0.1"
)

// StartNATSExporter starts the prometheus-nats-exporter against the embedded
// NATS server's HTTP monitoring endpoint.
//
//   - monitorPort MUST be the actual bound port of the embedded server's
//     HTTP monitoring listener (read back via server.MonitorAddr().Port
//     when HTTPPort was specified as -1 for a random port).
//   - exporterPort is where the exporter listens for Prometheus scrapes.
//     0 selects an ephemeral port on loopback. Production deployments
//     typically pin this to 7777.
//
// Returns the exporter handle so the caller can Stop it on shutdown.
// codecov:ignore — exporter construction branches are exercised through
// the integration-style prometheus_test.go below; the early-return validation
// is cheap to cover but the AddServer / Start rollback branches require a
// real NATS server.
func StartNATSExporter(monitorPort, exporterPort int) (*exporter.NATSExporter, error) {
	if monitorPort <= 0 {
		return nil, oops.Code("EVENTBUS_EXPORTER_INVALID_MONITOR_PORT").
			With("monitor_port", monitorPort).
			Errorf("NATS monitor port must be > 0 to enable the Prometheus exporter")
	}

	opts := exporter.GetDefaultExporterOptions()
	opts.ListenAddress = exporterLoopbackAddr
	opts.ListenPort = exporterPort
	opts.GetVarz = true
	opts.GetConnz = true
	opts.GetSubz = true
	opts.GetJszFilter = exporterJszAll

	exp := exporter.NewExporter(opts)
	serverURL := (&url.URL{
		Scheme: "http",
		Host:   exporterLoopbackAddr + ":" + strconv.Itoa(monitorPort),
	}).String()
	if err := exp.AddServer(exporterServerID, serverURL); err != nil {
		return nil, oops.Code("EVENTBUS_EXPORTER_ADD_SERVER_FAILED").
			With("monitor_port", monitorPort).
			Wrap(err)
	}
	if err := exp.Start(); err != nil {
		return nil, oops.Code("EVENTBUS_EXPORTER_START_FAILED").
			With("monitor_port", monitorPort).
			With("exporter_port", exporterPort).
			Wrap(err)
	}
	return exp, nil
}
