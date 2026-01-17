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
	engine := core.NewEngine(store, sessions)

	srv := NewServer(":0", engine, sessions)
	go srv.Run(ctx)

	time.Sleep(50 * time.Millisecond)

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("Server has no address")
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

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
