// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package gatewaymetrics holds Prometheus metrics shared by the gateway's
// telnet and web surfaces. The gateway is a protocol-translation layer
// (Phase 1.6 thinness): it MUST NOT compute rendering metadata locally;
// instead it reads EventFrame.Rendering populated upstream by the core
// process. Events that arrive without rendering are dropped at the
// gateway and counted via DroppedNilRenderingTotal.
package gatewaymetrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// DroppedNilRenderingTotal counts events the gateway dropped because
// EventFrame.Rendering was nil. A non-zero value indicates an upstream
// invariant violation (INV-EVENTBUS-6): the core process's RenderingPublisher
// failed to stamp rendering metadata before publish, or a publisher
// path bypassed it.
//
// The label set is intentionally bounded by the (host + plugin)-declared
// event type catalog; cardinality is bounded by the number of declared
// event types.
var DroppedNilRenderingTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "holomush",
		Subsystem: "gateway",
		Name:      "dropped_nil_rendering_total",
		Help:      "Number of events dropped at the gateway because Rendering was nil (INV-EVENTBUS-6 violation upstream).",
	},
	[]string{"surface", "event_type"},
)

// Surface label values for DroppedNilRenderingTotal.
const (
	SurfaceTelnet = "telnet"
	SurfaceWeb    = "web"
)
