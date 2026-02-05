// Copyright 2026 HoloMUSH Contributors

package command

import "time"

// MetricsRecorder tracks command execution metrics for a single dispatch.
type MetricsRecorder struct {
	startTime     time.Time
	commandName   string
	commandSource string
	status        string
}

// NewMetricsRecorder initializes a recorder for a single dispatch.
func NewMetricsRecorder() *MetricsRecorder {
	return &MetricsRecorder{startTime: time.Now()}
}

// SetCommandName sets the command name for metrics.
func (m *MetricsRecorder) SetCommandName(name string) {
	m.commandName = name
}

// SetCommandSource sets the command source for metrics.
func (m *MetricsRecorder) SetCommandSource(source string) {
	m.commandSource = source
}

// SetStatus sets the execution status for metrics.
func (m *MetricsRecorder) SetStatus(status string) {
	m.status = status
}

// Record writes the collected metrics if command name is available.
func (m *MetricsRecorder) Record() {
	if m.commandName == "" {
		return
	}

	RecordCommandExecution(m.commandName, m.commandSource, m.status)
	RecordCommandDuration(m.commandName, m.commandSource, time.Since(m.startTime))
}
