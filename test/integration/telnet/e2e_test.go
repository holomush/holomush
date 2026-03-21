// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package telnet_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/holomush/holomush/internal/core"
	grpcpkg "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/telnet"
	tlscerts "github.com/holomush/holomush/internal/tls"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// Package-level vars so Event Persistence tests can access them.
var (
	startLocation ulid.ULID
	eventStore    *store.PostgresEventStore
)

// testTelnetClient wraps a raw TCP connection for telnet interaction.
type testTelnetClient struct {
	conn    net.Conn
	scanner *bufio.Scanner
	writer  *bufio.Writer
}

func newTestTelnetClient(addr string) (*testTelnetClient, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	return &testTelnetClient{
		conn:    conn,
		scanner: bufio.NewScanner(conn),
		writer:  bufio.NewWriter(conn),
	}, nil
}

func (c *testTelnetClient) SendLine(line string) {
	_, err := c.writer.WriteString(line + "\n")
	Expect(err).NotTo(HaveOccurred())
	err = c.writer.Flush()
	Expect(err).NotTo(HaveOccurred())
}

func (c *testTelnetClient) ReadLine() string {
	err := c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	Expect(err).NotTo(HaveOccurred())
	ok := c.scanner.Scan()
	Expect(ok).To(BeTrue(), "expected to read a line but scanner stopped: %v", c.scanner.Err())
	return c.scanner.Text()
}

func (c *testTelnetClient) ReadUntil(pattern string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	var lines []string
	for time.Now().Before(deadline) {
		err := c.conn.SetReadDeadline(deadline)
		Expect(err).NotTo(HaveOccurred())
		if !c.scanner.Scan() {
			break
		}
		line := c.scanner.Text()
		lines = append(lines, line)
		if strings.Contains(line, pattern) {
			return line
		}
	}
	Fail(fmt.Sprintf("timed out waiting for pattern %q; lines read: %v", pattern, lines))
	return ""
}

func (c *testTelnetClient) Close() {
	_ = c.conn.Close()
}

// connectAsGuest sends "connect guest", reads the welcome banner and auth response,
// and returns the character name from the welcome message.
func connectAsGuest(c *testTelnetClient) string {
	// Read banner lines
	line1 := c.ReadLine()
	Expect(line1).To(Equal("Welcome to HoloMUSH!"))
	line2 := c.ReadLine()
	Expect(line2).To(Equal("Use: connect guest"))

	c.SendLine("connect guest")
	welcomeLine := c.ReadLine()
	// Extract name from "Welcome, <Name>!"
	re := regexp.MustCompile(`Welcome, ([A-Z][a-z]+_[A-Z][a-z]+)!`)
	match := re.FindStringSubmatch(welcomeLine)
	Expect(match).NotTo(BeEmpty(), "expected welcome message to match themed name pattern, got: %s", welcomeLine)
	return match[1]
}

// waitForPipeline sends a probe command and waits for the echo, proving the
// full command→event subscription pipeline is ready. Both the sender's own
// broadcast (via the event stream) and peers' broadcasts are drained so probe
// messages don't leak into subsequent assertions.
func waitForPipeline(c *testTelnetClient, peers ...*testTelnetClient) {
	probe := "__ready__:" + ulid.Make().String()
	c.SendLine("say " + probe)
	c.ReadUntil(fmt.Sprintf(`You say, "%s"`, probe), 5*time.Second)
	// The sender also receives its own broadcast via the event stream as
	// "<Name> says, "<probe>"". Drain it to prevent stale messages from
	// interfering with subsequent ReadLine/ReadUntil assertions.
	c.ReadUntil(probe, 5*time.Second)
	for _, p := range peers {
		p.ReadUntil(probe, 5*time.Second)
	}
}

var _ = Describe("Telnet Vertical Slice E2E", func() {
	var (
		testCtx      context.Context
		testCancel   context.CancelFunc
		container    *postgres.PostgresContainer
		grpcServer   *grpc.Server
		grpcListener net.Listener
		grpcCli      *grpcpkg.Client
		telnetAddr   string
		telnetLis    net.Listener
		acceptCancel context.CancelFunc
		guestAuth    *telnet.GuestAuthenticator
	)

	BeforeEach(func() {
		testCtx, testCancel = context.WithTimeout(context.Background(), 2*time.Minute)

		// 1. Start PostgreSQL container
		var err error
		container, err = postgres.Run(testCtx,
			"postgres:18-alpine",
			postgres.WithDatabase("holomush_test"),
			postgres.WithUsername("holomush"),
			postgres.WithPassword("holomush"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(30*time.Second),
			),
		)
		Expect(err).NotTo(HaveOccurred())

		connStr, err := container.ConnectionString(testCtx, "sslmode=disable")
		Expect(err).NotTo(HaveOccurred())

		// 2. Run migrations
		migrator, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migrator.Up()).To(Succeed())
		_ = migrator.Close()

		// 3. Create event store
		eventStore, err = store.NewPostgresEventStore(testCtx, connStr)
		Expect(err).NotTo(HaveOccurred())

		// 4. Init game ID
		gameID, err := eventStore.InitGameID(testCtx)
		Expect(err).NotTo(HaveOccurred())

		// 5. Generate TLS certs
		tmpDir, err := os.MkdirTemp("", "holomush-telnet-e2e-*")
		Expect(err).NotTo(HaveOccurred())
		certsDir := filepath.Join(tmpDir, "certs")
		Expect(os.MkdirAll(certsDir, 0o700)).To(Succeed())

		ca, err := tlscerts.GenerateCA(gameID)
		Expect(err).NotTo(HaveOccurred())
		serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
		Expect(err).NotTo(HaveOccurred())
		Expect(tlscerts.SaveCertificates(certsDir, ca, serverCert)).To(Succeed())
		clientCert, err := tlscerts.GenerateClientCert(ca, "gateway")
		Expect(err).NotTo(HaveOccurred())
		Expect(tlscerts.SaveClientCert(certsDir, clientCert)).To(Succeed())

		// 6. Load TLS configs
		serverTLS, err := tlscerts.LoadServerTLS(certsDir, "core")
		Expect(err).NotTo(HaveOccurred())
		clientTLS, err := tlscerts.LoadClientTLS(certsDir, "gateway", gameID)
		Expect(err).NotTo(HaveOccurred())

		// 7. Create core components
		sessions := core.NewSessionManager()
		broadcaster := core.NewBroadcaster()
		engine := core.NewEngine(eventStore, sessions)

		// 8. Create GuestAuthenticator
		startLocation = ulid.Make()
		guestAuth = telnet.NewGuestAuthenticator(telnet.NewGemstoneElementTheme(), startLocation)

		// 9. Create gRPC server
		coreServer := grpcpkg.NewCoreServer(engine, sessions, broadcaster, session.NewMemStore(),
			grpcpkg.WithAuthenticator(guestAuth),
			grpcpkg.WithDisconnectHook(func(info session.Info) {
				if info.IsGuest {
					guestAuth.ReleaseGuest(info.CharacterName)
				}
			}),
		)
		grpcServer = grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
		corev1.RegisterCoreServiceServer(grpcServer, coreServer)

		// 10. Start gRPC server
		grpcListener, err = net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())
		go func() { _ = grpcServer.Serve(grpcListener) }()

		grpcAddr := grpcListener.Addr().String()
		Eventually(func() bool {
			conn, err := net.DialTimeout("tcp", grpcAddr, 100*time.Millisecond)
			if err != nil {
				return false
			}
			conn.Close()
			return true
		}).Should(BeTrue())

		// 11. Create gRPC client
		grpcCli, err = grpcpkg.NewClient(testCtx, grpcpkg.ClientConfig{
			Address:   grpcAddr,
			TLSConfig: clientTLS,
		})
		Expect(err).NotTo(HaveOccurred())

		// 12. Start telnet listener
		telnetLis, err = net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())
		telnetAddr = telnetLis.Addr().String()

		// 13. Accept loop
		var acceptCtx context.Context
		acceptCtx, acceptCancel = context.WithCancel(testCtx)
		go func() {
			for {
				conn, err := telnetLis.Accept()
				if err != nil {
					return
				}
				handler := telnet.NewGatewayHandler(conn, grpcCli)
				go handler.Handle(acceptCtx)
			}
		}()
	})

	AfterEach(func() {
		if acceptCancel != nil {
			acceptCancel()
		}
		if telnetLis != nil {
			_ = telnetLis.Close()
		}
		if grpcCli != nil {
			_ = grpcCli.Close()
		}
		if grpcServer != nil {
			grpcServer.GracefulStop()
		}
		if grpcListener != nil {
			_ = grpcListener.Close()
		}
		if eventStore != nil {
			eventStore.Close()
		}
		if container != nil {
			_ = container.Terminate(context.Background())
		}
		if testCancel != nil {
			testCancel()
		}
	})

	Describe("Authentication", func() {
		It("Player A connects as guest and receives a themed name", func() {
			client, err := newTestTelnetClient(telnetAddr)
			Expect(err).NotTo(HaveOccurred())
			defer client.Close()

			name := connectAsGuest(client)
			Expect(name).To(MatchRegexp(`^[A-Z][a-z]+_[A-Z][a-z]+$`))
		})

		It("rejects registered login with helpful message", func() {
			client, err := newTestTelnetClient(telnetAddr)
			Expect(err).NotTo(HaveOccurred())
			defer client.Close()

			// Read banner
			_ = client.ReadLine() // Welcome to HoloMUSH!
			_ = client.ReadLine() // Use: connect guest

			client.SendLine("connect player1 password")
			line := client.ReadLine()
			Expect(line).To(ContainSubstring("connect guest"))
		})
	})

	Describe("Say Communication", func() {
		var clientA, clientB *testTelnetClient

		BeforeEach(func() {
			var err error
			clientA, err = newTestTelnetClient(telnetAddr)
			Expect(err).NotTo(HaveOccurred())
			clientB, err = newTestTelnetClient(telnetAddr)
			Expect(err).NotTo(HaveOccurred())

			connectAsGuest(clientA)
			connectAsGuest(clientB)

			waitForPipeline(clientA, clientB)
			waitForPipeline(clientB, clientA)
		})

		AfterEach(func() {
			clientA.Close()
			clientB.Close()
		})

		It("Player A says something and sees their own echo", func() {
			clientA.SendLine(`say Hello, world!`)
			line := clientA.ReadLine()
			Expect(line).To(ContainSubstring(`You say, "Hello, world!"`))
		})

		It("Player B receives Player A's say via event stream", func() {
			clientA.SendLine(`say Greetings!`)
			// A sees own echo
			_ = clientA.ReadLine()
			// B receives broadcast
			line := clientB.ReadUntil("says,", 5*time.Second)
			Expect(line).To(ContainSubstring(`says,`))
			Expect(line).To(ContainSubstring(`Greetings!`))
		})

		It("Player A receives Player B's say via event stream", func() {
			clientB.SendLine(`say From B!`)
			_ = clientB.ReadLine() // B's own echo
			line := clientA.ReadUntil("says,", 5*time.Second)
			Expect(line).To(ContainSubstring(`says,`))
			Expect(line).To(ContainSubstring(`From B!`))
		})
	})

	Describe("Pose Communication", func() {
		var clientA, clientB *testTelnetClient

		BeforeEach(func() {
			var err error
			clientA, err = newTestTelnetClient(telnetAddr)
			Expect(err).NotTo(HaveOccurred())
			clientB, err = newTestTelnetClient(telnetAddr)
			Expect(err).NotTo(HaveOccurred())

			connectAsGuest(clientA)
			connectAsGuest(clientB)

			waitForPipeline(clientA, clientB)
			waitForPipeline(clientB, clientA)
		})

		AfterEach(func() {
			clientA.Close()
			clientB.Close()
		})

		It("Player A poses and sees their own echo", func() {
			clientA.SendLine("pose waves enthusiastically")
			line := clientA.ReadLine()
			Expect(line).To(ContainSubstring("waves enthusiastically"))
		})

		It("Player B receives Player A's pose via event stream", func() {
			clientA.SendLine("pose dances a jig")
			_ = clientA.ReadLine() // A's own echo
			line := clientB.ReadUntil("dances a jig", 5*time.Second)
			Expect(line).To(ContainSubstring("dances a jig"))
		})
	})

	Describe("Disconnect", func() {
		It("Player A disconnects cleanly via quit", func() {
			client, err := newTestTelnetClient(telnetAddr)
			Expect(err).NotTo(HaveOccurred())
			defer client.Close()

			connectAsGuest(client)

			client.SendLine("quit")
			line := client.ReadLine()
			Expect(line).To(Equal("Goodbye!"))
		})

		It("Player B continues receiving events after A disconnects", func() {
			clientA, err := newTestTelnetClient(telnetAddr)
			Expect(err).NotTo(HaveOccurred())
			clientB, err := newTestTelnetClient(telnetAddr)
			Expect(err).NotTo(HaveOccurred())
			defer clientB.Close()

			connectAsGuest(clientA)
			connectAsGuest(clientB)
			waitForPipeline(clientA, clientB)
			waitForPipeline(clientB, clientA)

			// A says something, B receives
			clientA.SendLine(`say Before quit`)
			_ = clientA.ReadLine()
			line := clientB.ReadUntil("says,", 5*time.Second)
			Expect(line).To(ContainSubstring("Before quit"))

			// A quits
			clientA.SendLine("quit")
			_ = clientA.ReadLine() // Goodbye!
			clientA.Close()

			// C connects and says something — B should receive
			clientC, err := newTestTelnetClient(telnetAddr)
			Expect(err).NotTo(HaveOccurred())
			defer clientC.Close()

			connectAsGuest(clientC)
			waitForPipeline(clientC, clientB)

			clientC.SendLine(`say After A left`)
			_ = clientC.ReadLine()
			line = clientB.ReadUntil("says,", 5*time.Second)
			Expect(line).To(ContainSubstring("After A left"))
		})
	})

	Describe("Event Persistence", func() {
		It("events are persisted to PostgreSQL after say commands", func() {
			client, err := newTestTelnetClient(telnetAddr)
			Expect(err).NotTo(HaveOccurred())
			defer client.Close()

			connectAsGuest(client)
			waitForPipeline(client)

			client.SendLine(`say Persistence test`)
			line := client.ReadLine()
			Expect(line).To(ContainSubstring(`You say, "Persistence test"`))

			// Poll for the specific "Persistence test" event (not readiness probes)
			stream := "location:" + startLocation.String()
			Eventually(func() bool {
				events, err := eventStore.Replay(testCtx, stream, ulid.ULID{}, 100)
				if err != nil {
					return false
				}
				for _, ev := range events {
					if ev.Type == core.EventTypeSay && strings.Contains(string(ev.Payload), "Persistence test") {
						return true
					}
				}
				return false
			}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(), "expected 'Persistence test' say event persisted")
		})
	})

	Describe("Lifecycle Events", func() {
		It("emits arrive event on guest connect", func() {
			client, err := newTestTelnetClient(telnetAddr)
			Expect(err).NotTo(HaveOccurred())
			defer client.Close()

			connectAsGuest(client)

			// Poll for arrive event persistence
			Eventually(func() bool {
				events, err := eventStore.Replay(testCtx,
					"location:"+startLocation.String(), ulid.ULID{}, 100)
				if err != nil {
					return false
				}
				for _, e := range events {
					if string(e.Type) == "arrive" {
						return true
					}
				}
				return false
			}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(), "expected arrive event in store")
		})

		It("emits leave event on guest disconnect", func() {
			client, err := newTestTelnetClient(telnetAddr)
			Expect(err).NotTo(HaveOccurred())

			connectAsGuest(client)
			client.SendLine("quit")
			_ = client.ReadLine() // Goodbye
			client.Close()

			// Leave event should appear in the location stream
			Eventually(func() bool {
				events, err := eventStore.Replay(testCtx,
					"location:"+startLocation.String(), ulid.ULID{}, 100)
				if err != nil {
					return false
				}
				for _, e := range events {
					if string(e.Type) == string(core.EventTypeLeave) {
						return true
					}
				}
				return false
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(), "expected leave event in store")
		})

		It("releases guest name after disconnect", func() {
			// Connect a guest
			c, err := newTestTelnetClient(telnetAddr)
			Expect(err).NotTo(HaveOccurred())
			connectAsGuest(c)

			// Active count should be at least 1
			// (other tests in this BeforeEach may also have active guests)
			initialActive := guestAuth.ActiveCount()
			Expect(initialActive).To(BeNumerically(">=", 1))

			// Disconnect
			c.SendLine("quit")
			_ = c.ReadLine() // Goodbye
			c.Close()

			// Wait for disconnect RPC + hook to fire
			Eventually(func() int {
				return guestAuth.ActiveCount()
			}, 5*time.Second, 100*time.Millisecond).Should(
				BeNumerically("<", initialActive),
				"expected active guest count to decrease after disconnect",
			)
		})
	})
})
