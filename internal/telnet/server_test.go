package telnet

import (
	"bufio"
	"net"
	"testing"
	"time"
)

func TestServer_AcceptsConnections(t *testing.T) {
	ctx := t.Context()

	// Start server on random port
	srv := NewServer(":0")
	go srv.Run(ctx)

	// Wait for server to start
	time.Sleep(50 * time.Millisecond)

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("Server has no address")
	}

	// Connect
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Should receive welcome message
	conn.SetReadDeadline(time.Now().Add(time.Second))
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read welcome: %v", err)
	}
	if line == "" {
		t.Error("Expected welcome message")
	}
}
