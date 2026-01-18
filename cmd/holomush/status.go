package main

import (
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/holomush/holomush/internal/control"
	controlv1 "github.com/holomush/holomush/internal/proto/holomush/control/v1"
	"github.com/holomush/holomush/internal/xdg"
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
	jsonOutput  bool
	coreAddr    string
	gatewayAddr string
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
	cmd.Flags().StringVar(&cfg.coreAddr, "core-addr", defaultCoreControlAddr, "core control gRPC address")
	cmd.Flags().StringVar(&cfg.gatewayAddr, "gateway-addr", defaultGatewayControlAddr, "gateway control gRPC address")

	return cmd
}

// runStatus executes the status command.
func runStatus(cmd *cobra.Command, cfg *statusConfig) error {
	// Query both core and gateway processes
	statuses := map[string]ProcessStatus{
		"core":    queryProcessStatusGRPC("core", cfg.coreAddr),
		"gateway": queryProcessStatusGRPC("gateway", cfg.gatewayAddr),
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

// queryProcessStatusGRPC queries the control gRPC server for a process and returns its status.
func queryProcessStatusGRPC(component, addr string) ProcessStatus {
	status := ProcessStatus{
		Component: component,
	}

	// Get certs directory
	certsDir, err := xdg.CertsDir()
	if err != nil {
		status.Error = fmt.Sprintf("failed to get certs directory: %v", err)
		return status
	}

	// Extract game_id from CA certificate for ServerName verification
	gameID, err := control.ExtractGameIDFromCA(certsDir)
	if err != nil {
		status.Error = fmt.Sprintf("failed to extract game_id from CA: %v", err)
		return status
	}

	// Load TLS config for client with game_id for proper ServerName verification
	tlsConfig, err := control.LoadControlClientTLS(certsDir, component, gameID)
	if err != nil {
		status.Error = fmt.Sprintf("failed to load TLS config: %v", err)
		return status
	}

	// Create gRPC client with mTLS
	creds := credentials.NewTLS(tlsConfig)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	//nolint:staticcheck // grpc.NewClient requires different setup; DialContext works for 1.x
	conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		status.Error = fmt.Sprintf("failed to connect: %v", err)
		return status
	}
	defer func() { _ = conn.Close() }()

	client := controlv1.NewControlClient(conn)

	// Query status
	resp, err := client.Status(ctx, &controlv1.StatusRequest{})
	if err != nil {
		status.Error = fmt.Sprintf("failed to query status: %v", err)
		return status
	}

	// Process is running and responding
	status.Running = resp.Running
	status.Health = "healthy"
	status.PID = int(resp.Pid)
	status.UptimeSeconds = resp.UptimeSeconds

	return status
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
