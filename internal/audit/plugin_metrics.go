// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// pluginAuditFailuresCounter tracks audit logging failures originating
// from the dispatcher's plugin-event flush step (as opposed to the
// engine's own failures tracked by engineAuditFailuresCounter).
//
// Bumped by the dispatcher when auditLogger.Log returns an error for
// a plugin-sourced event. The dispatcher continues processing remaining
// events and returns the user's command response unchanged.
var pluginAuditFailuresCounter = promauto.NewCounter(prometheus.CounterOpts{
	Name: "abac_audit_plugin_failures_total",
	Help: "Total number of audit logging failures at the plugin-flush level",
})

// RecordPluginAuditFailure increments the plugin-level audit failure counter.
// Call this when auditLogger.Log returns an error during dispatcher flush
// of plugin-emitted events.
func RecordPluginAuditFailure() {
	pluginAuditFailuresCounter.Inc()
}
