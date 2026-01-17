package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/control"
)

// ProcessStatus holds the status information for a process.
type ProcessStatus struct {
	Component     string `json:"component"`
	Running       bool   `json:"running"`
	Health        string `json:"health,omitempty"`
	PID           int    `json:"pid,omitempty"`
	UptimeSeconds int64  `json:"uptime_seconds,omitempty"`
	Error         string `json:"error,omitempty"`
}

// statusConfig holds configuration for the status command.
type statusConfig struct {
	jsonOutput bool
}

// newStatusCmd creates the status subcommand with all flags configured.
func newStatusCmd() *cobra.Command {
	cfg := &statusConfig{}

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show status of running HoloMUSH processes",
		Long:  `Show the health and status of running gateway and core processes.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd, cfg)
		},
	}

	// Register flags
	cmd.Flags().BoolVar(&cfg.jsonOutput, "json", false, "output status as JSON")

	return cmd
}

// runStatus executes the status command.
func runStatus(cmd *cobra.Command, cfg *statusConfig) error {
	// Query both core and gateway processes
	statuses := map[string]ProcessStatus{
		"core":    queryProcessStatus("core"),
		"gateway": queryProcessStatus("gateway"),
	}

	// Format and output the results
	var output string
	var err error

	if cfg.jsonOutput {
		output, err = formatStatusJSON(statuses)
		if err != nil {
			return fmt.Errorf("failed to format JSON: %w", err)
		}
	} else {
		output = formatStatusTable(statuses)
	}

	cmd.Println(output)
	return nil
}

// queryProcessStatus queries the control socket for a process and returns its status.
func queryProcessStatus(component string) ProcessStatus {
	status := ProcessStatus{
		Component: component,
	}

	// Get socket path
	socketPath, err := control.SocketPath(component)
	if err != nil {
		status.Error = fmt.Sprintf("failed to get socket path: %v", err)
		return status
	}

	// Check if socket file exists
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		status.Error = "socket not found"
		return status
	}

	// Create HTTP client for Unix socket
	client := createUnixHTTPClient(socketPath)

	// Query health endpoint
	healthResp, err := client.Get("http://localhost/health")
	if err != nil {
		status.Error = fmt.Sprintf("failed to connect: %v", err)
		return status
	}
	defer func() { _ = healthResp.Body.Close() }()

	var health control.HealthResponse
	if err := json.NewDecoder(healthResp.Body).Decode(&health); err != nil {
		status.Error = fmt.Sprintf("failed to decode health response: %v", err)
		return status
	}

	// Query status endpoint for more details
	statusResp, err := client.Get("http://localhost/status")
	if err != nil {
		// Health succeeded but status failed - still consider running
		status.Running = true
		status.Health = health.Status
		return status
	}
	defer func() { _ = statusResp.Body.Close() }()

	var controlStatus control.StatusResponse
	if err := json.NewDecoder(statusResp.Body).Decode(&controlStatus); err != nil {
		// Health succeeded but status decode failed - still consider running
		status.Running = true
		status.Health = health.Status
		return status
	}

	// Process is running and responding
	status.Running = controlStatus.Running
	status.Health = health.Status
	status.PID = controlStatus.PID
	status.UptimeSeconds = controlStatus.UptimeSeconds

	return status
}

// createUnixHTTPClient creates an HTTP client that connects via Unix socket.
func createUnixHTTPClient(socketPath string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 2 * time.Second,
	}
}

// formatStatusTable formats the status as a human-readable table.
func formatStatusTable(statuses map[string]ProcessStatus) string {
	var buf []byte
	w := tabwriter.NewWriter((*byteWriter)(&buf), 0, 0, 2, ' ', 0)

	// Header
	_, _ = fmt.Fprintln(w, "PROCESS\tSTATUS\tHEALTH\tPID\tUPTIME")
	_, _ = fmt.Fprintln(w, "-------\t------\t------\t---\t------")

	// Process rows in consistent order
	for _, component := range []string{"core", "gateway"} {
		status := statuses[component]
		if status.Running {
			uptime := formatUptime(status.UptimeSeconds)
			_, _ = fmt.Fprintf(w, "%s\trunning\t%s\t%d\t%s\n",
				component, status.Health, status.PID, uptime)
		} else {
			reason := "not running"
			if status.Error != "" {
				reason = status.Error
			}
			_, _ = fmt.Fprintf(w, "%s\tstopped\t-\t-\t%s\n", component, reason)
		}
	}

	_ = w.Flush()
	return string(buf)
}

// formatStatusJSON formats the status as JSON.
func formatStatusJSON(statuses map[string]ProcessStatus) (string, error) {
	data, err := json.MarshalIndent(statuses, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal status: %w", err)
	}
	return string(data), nil
}

// formatUptime formats seconds into a human-readable duration.
func formatUptime(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%dm %ds", seconds/60, seconds%60)
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

// byteWriter is a simple writer that appends to a byte slice.
type byteWriter []byte

func (w *byteWriter) Write(p []byte) (int, error) {
	*w = append(*w, p...)
	return len(p), nil
}
