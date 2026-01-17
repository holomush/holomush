// Package grpc provides gRPC client and server implementations for HoloMUSH.
package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	corev1 "github.com/holomush/holomush/internal/proto/holomush/core/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// Client wraps a gRPC connection to the Core service.
type Client struct {
	conn   *grpc.ClientConn
	client corev1.CoreClient
}

// ClientConfig holds configuration for the gRPC client.
type ClientConfig struct {
	// Address is the target gRPC server address (e.g., "localhost:9000")
	Address string

	// TLSConfig for mTLS authentication. If nil, insecure connection is used.
	TLSConfig *tls.Config

	// KeepaliveTime is how often to ping the server (default: 10s)
	KeepaliveTime time.Duration

	// KeepaliveTimeout is how long to wait for ping response (default: 5s)
	KeepaliveTimeout time.Duration
}

// NewClient creates a new gRPC client connected to the Core service.
// The context parameter is reserved for future use (e.g., connection timeouts).
func NewClient(_ context.Context, cfg ClientConfig) (*Client, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("address is required")
	}

	// Set defaults
	if cfg.KeepaliveTime == 0 {
		cfg.KeepaliveTime = 10 * time.Second
	}
	if cfg.KeepaliveTimeout == 0 {
		cfg.KeepaliveTimeout = 5 * time.Second
	}

	// Build dial options
	opts := []grpc.DialOption{
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                cfg.KeepaliveTime,
			Timeout:             cfg.KeepaliveTimeout,
			PermitWithoutStream: true,
		}),
	}

	// Configure TLS
	if cfg.TLSConfig != nil {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(cfg.TLSConfig)))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Create client connection
	conn, err := grpc.NewClient(cfg.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to core: %w", err)
	}

	return &Client{
		conn:   conn,
		client: corev1.NewCoreClient(conn),
	}, nil
}

// Close closes the underlying gRPC connection.
func (c *Client) Close() error {
	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			return fmt.Errorf("failed to close connection: %w", err)
		}
	}
	return nil
}

// Authenticate validates credentials and creates a session.
func (c *Client) Authenticate(ctx context.Context, req *corev1.AuthRequest) (*corev1.AuthResponse, error) {
	resp, err := c.client.Authenticate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("authenticate RPC failed: %w", err)
	}
	return resp, nil
}

// HandleCommand processes a game command.
func (c *Client) HandleCommand(ctx context.Context, req *corev1.CommandRequest) (*corev1.CommandResponse, error) {
	resp, err := c.client.HandleCommand(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("handle command RPC failed: %w", err)
	}
	return resp, nil
}

// Subscribe opens a stream of events for the session.
func (c *Client) Subscribe(ctx context.Context, req *corev1.SubscribeRequest) (corev1.Core_SubscribeClient, error) {
	stream, err := c.client.Subscribe(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("subscribe RPC failed: %w", err)
	}
	return stream, nil
}

// Disconnect ends a session.
func (c *Client) Disconnect(ctx context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
	resp, err := c.client.Disconnect(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("disconnect RPC failed: %w", err)
	}
	return resp, nil
}

// CoreClient returns the underlying gRPC CoreClient interface for advanced usage.
func (c *Client) CoreClient() corev1.CoreClient {
	return c.client
}
