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
// Use the constructor functions NewProcessStatus and NewProcessStatusError
// to create instances - they enforce valid state invariants.
type ProcessStatus struct {
	Component     string `json:"component"`
	Running       bool   `json:"running"`
	Health        string `json:"health,omitempty"`
	PID           int    `json:"pid,omitempty"`
	UptimeSeconds int64  `json:"uptime_seconds,omitempty"`
	Error         string `json:"error,omitempty"`
}

// NewProcessStatus creates a ProcessStatus for a running process.
// This constructor ensures valid state: Running is true, Error is empty,
// and Health is set to "healthy".
func NewProcessStatus(component string, running bool, pid int, uptime int64) ProcessStatus {
	return ProcessStatus{
		Component:     component,
		Running:       running,
		Health:        "healthy",
		PID:           pid,
		UptimeSeconds: uptime,
	}
}

// NewProcessStatusError creates a ProcessStatus for a process that failed to respond.
// This constructor ensures valid state: Running is false, Error contains the message,
// and Health/PID/Uptime are zero values.
func NewProcessStatusError(component string, err error) ProcessStatus {
	return ProcessStatus{
		Component: component,
		Running:   false,
		Error:     err.Error(),
	}
}

// statusConfig holds configuration for the status command.
type statusConfig struct {
	jsonOutput  bool
	coreAddr    string
	gatewayAddr string
}

// Validate checks that the configuration is valid.
func (cfg *statusConfig) Validate() error {
	if cfg.coreAddr == "" {
		return fmt.Errorf("core-addr is required")
	}
	if cfg.gatewayAddr == "" {
		return fmt.Errorf("gateway-addr is required")
	}
	return nil
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

	cmd.Flags().BoolVar(&cfg.jsonOutput, "json", false, "output status as JSON")
	cmd.Flags().StringVar(&cfg.coreAddr, "core-addr", defaultCoreControlAddr, "core control gRPC address")
	cmd.Flags().StringVar(&cfg.gatewayAddr, "gateway-addr", defaultGatewayControlAddr, "gateway control gRPC address")

	return cmd
}

// runStatus executes the status command.
func runStatus(cmd *cobra.Command, cfg *statusConfig) error {
	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

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
	// Get certs directory
	certsDir, err := xdg.CertsDir()
	if err != nil {
		return NewProcessStatusError(component, fmt.Errorf("failed to get certs directory: %w", err))
	}

	// Extract game_id from CA certificate for ServerName verification
	gameID, err := control.ExtractGameIDFromCA(certsDir)
	if err != nil {
		return NewProcessStatusError(component, fmt.Errorf("failed to extract game_id from CA: %w", err))
	}

	// Load TLS config for client with game_id for proper ServerName verification
	tlsConfig, err := control.LoadControlClientTLS(certsDir, component, gameID)
	if err != nil {
		return NewProcessStatusError(component, fmt.Errorf("failed to load TLS config: %w", err))
	}

	// Create gRPC client with mTLS
	creds := credentials.NewTLS(tlsConfig)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// TODO: Migrate to grpc.NewClient when ready.
	//
	// grpc.DialContext is deprecated in favor of grpc.NewClient. Key differences:
	//
	// 1. Connection behavior: grpc.NewClient creates a "virtual" connection without
	//    immediately establishing a physical connection (lazy connect). grpc.DialContext
	//    can block with WithBlock option. For our use case, we want eager connection
	//    to detect unavailable services quickly.
	//
	// 2. Name resolver: grpc.NewClient uses "dns" as default resolver, while DialContext
	//    uses "passthrough". For direct IP:port addresses like ours, this shouldn't matter,
	//    but we should verify behavior with mTLS ServerName verification.
	//
	// 3. Migration steps:
	//    a. Replace grpc.DialContext with grpc.NewClient
	//    b. Remove context parameter (NewClient doesn't take context)
	//    c. If blocking behavior is needed, call conn.Connect() and use
	//       conn.WaitForStateChange() to wait for Ready state
	//    d. Test mTLS with ServerName verification still works correctly
	//    e. Update timeout handling (move to RPC context instead of dial context)
	//
	// See: https://github.com/grpc/grpc-go/blob/master/Documentation/anti-patterns.md
	//
	//nolint:staticcheck // grpc.NewClient requires different setup; DialContext works for 1.x
	conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return NewProcessStatusError(component, fmt.Errorf("failed to connect: %w", err))
	}
	defer func() { _ = conn.Close() }()

	client := controlv1.NewControlClient(conn)

	// Query status
	resp, err := client.Status(ctx, &controlv1.StatusRequest{})
	if err != nil {
		return NewProcessStatusError(component, fmt.Errorf("failed to query status: %w", err))
	}

	// Process is running and responding
	return NewProcessStatus(component, resp.Running, int(resp.Pid), resp.UptimeSeconds)
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
