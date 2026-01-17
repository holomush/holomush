package telnet

import (
	"bufio"
	"net"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/core"
)

func TestServer_AcceptsConnections(t *testing.T) {
	ctx := t.Context()

	store := core.NewMemoryEventStore()
	sessions := core.NewSessionManager()
	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)
	srv := NewServer(":0", engine, sessions, broadcaster)
	go func() {
		//nolint:errcheck,gosec // Server shutdown error is expected when context cancels
		srv.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("Server has no address")
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() {
		_ = conn.Close() // Best effort cleanup in tests
	}()

	err = conn.SetReadDeadline(time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("Failed to set read deadline: %v", err)
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read welcome: %v", err)
	}
	if line == "" {
		t.Error("Expected welcome message")
	}
}
