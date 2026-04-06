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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"
	"github.com/testcontainers/testcontainers-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/auth"
	authpostgres "github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers"
	"github.com/holomush/holomush/internal/core"
	grpcpkg "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/naming"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/telnet"
	tlscerts "github.com/holomush/holomush/internal/tls"
	"github.com/holomush/holomush/internal/world"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	"github.com/holomush/holomush/test/testutil"
)

// authCharRepoAdapter wraps a pgxpool.Pool to implement auth.CharacterRepository
// (and auth.GuestCharacterRepository). Mirrors cmd/holomush/auth_adapters.go.
type authCharRepoAdapter struct {
	pool     *pgxpool.Pool
	charRepo *worldpostgres.CharacterRepository
}

func (a *authCharRepoAdapter) Create(ctx context.Context, char *world.Character) error {
	return a.charRepo.Create(ctx, char)
}

func (a *authCharRepoAdapter) ExistsByName(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := a.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM characters WHERE LOWER(name) = LOWER($1))", name,
	).Scan(&exists)
	if err != nil {
		return false, oops.Code("CHARACTER_EXISTS_CHECK_FAILED").With("name", name).Wrap(err)
	}
	return exists, nil
}

func (a *authCharRepoAdapter) CountByPlayer(ctx context.Context, playerID ulid.ULID) (int, error) {
	var count int
	err := a.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM characters WHERE player_id = $1", playerID.String(),
	).Scan(&count)
	if err != nil {
		return 0, oops.Code("CHARACTER_COUNT_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	return count, nil
}

func (a *authCharRepoAdapter) ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error) {
	rows, err := a.pool.Query(ctx,
		`SELECT id, player_id, name, description, location_id, created_at
		 FROM characters WHERE player_id = $1 ORDER BY name`, playerID.String(),
	)
	if err != nil {
		return nil, oops.Code("CHARACTER_LIST_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	defer rows.Close()

	var chars []*world.Character
	for rows.Next() {
		var c world.Character
		var idStr, pidStr string
		var locStr *string
		if scanErr := rows.Scan(&idStr, &pidStr, &c.Name, &c.Description, &locStr, &c.CreatedAt); scanErr != nil {
			return nil, oops.Code("CHARACTER_SCAN_FAILED").Wrap(scanErr)
		}
		var parseErr error
		c.ID, parseErr = ulid.Parse(idStr)
		if parseErr != nil {
			return nil, oops.Code("CHARACTER_PARSE_FAILED").With("field", "id").With("value", idStr).Wrap(parseErr)
		}
		c.PlayerID, parseErr = ulid.Parse(pidStr)
		if parseErr != nil {
			return nil, oops.Code("CHARACTER_PARSE_FAILED").With("field", "player_id").With("value", pidStr).Wrap(parseErr)
		}
		if locStr != nil {
			lid, lidErr := ulid.Parse(*locStr)
			if lidErr != nil {
				return nil, oops.Code("CHARACTER_PARSE_FAILED").With("field", "location_id").With("value", *locStr).Wrap(lidErr)
			}
			c.LocationID = &lid
		}
		chars = append(chars, &c)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, oops.Code("CHARACTER_ITERATE_FAILED").Wrap(rowsErr)
	}
	return chars, nil
}

// Package-level vars so Event Persistence tests can access them.
var (
	startLocation ulid.ULID
	eventStore    *store.PostgresEventStore
)

// registerTestCommands adds say and pose handlers for E2E tests.
// These replicate the behavior of the core plugins via the old handler interface
// so the telnet pipeline test doesn't need the full plugin system.
func registerTestCommands(reg *command.Registry) {
	mustRegister := func(cfg command.CommandEntryConfig) {
		entry, err := command.NewCommandEntry(cfg)
		if err != nil {
			panic("failed to create test command " + cfg.Name + ": " + err.Error())
		}
		if err := reg.Register(*entry); err != nil {
			panic("failed to register test command " + cfg.Name + ": " + err.Error())
		}
	}

	mustRegister(command.CommandEntryConfig{
		Name: "say",
		Handler: func(ctx context.Context, exec *command.CommandExecution) error {
			return exec.Services().Events().Append(ctx, core.Event{
				ID:      ulid.Make(),
				Stream:  "location:" + exec.LocationID().String(),
				Type:    core.EventType(pluginsdk.EventTypeSay),
				Actor:   core.Actor{Kind: core.ActorCharacter, ID: exec.CharacterID().String()},
				Payload: []byte(fmt.Sprintf(`{"character_name":"%s","message":"%s"}`, exec.CharacterName(), exec.Args)),
			})
		},
		Help:   "Say something",
		Usage:  "say <message>",
		Source: "test",
	})

	mustRegister(command.CommandEntryConfig{
		Name: "pose",
		Handler: func(ctx context.Context, exec *command.CommandExecution) error {
			return exec.Services().Events().Append(ctx, core.Event{
				ID:      ulid.Make(),
				Stream:  "location:" + exec.LocationID().String(),
				Type:    core.EventType(pluginsdk.EventTypePose),
				Actor:   core.Actor{Kind: core.ActorCharacter, ID: exec.CharacterID().String()},
				Payload: []byte(fmt.Sprintf(`{"character_name":"%s","message":"%s"}`, exec.CharacterName(), exec.Args)),
			})
		},
		Help:   "Pose an action",
		Usage:  "pose <action>",
		Source: "test",
	})
}

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
	Fail(fmt.Sprintf("timed out waiting for pattern %q; lines read: %v; scanner err: %v", pattern, lines, c.scanner.Err()))
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
	// Extract name from "Welcome, <Name>!" — names use spaces (e.g. "Sapphire Diamond").
	re := regexp.MustCompile(`Welcome, ([A-Z][a-z]+ [A-Z][a-z]+)!`)
	match := re.FindStringSubmatch(welcomeLine)
	Expect(match).NotTo(BeEmpty(), "expected welcome message to match themed name pattern, got: %s", welcomeLine)
	return match[1]
}

// waitForPipeline sends a probe command and waits for the broadcast, proving
// the full command→event subscription pipeline is ready. The sender sees their
// own say broadcast via the event stream ("<Name> says, ..."). Peers' copies
// are also drained so probe messages don't leak into subsequent assertions.
func waitForPipeline(c *testTelnetClient, peers ...*testTelnetClient) {
	probe := "__ready__:" + ulid.Make().String()
	c.SendLine("say " + probe)
	// Sender sees own broadcast via event stream (no inline echo).
	c.ReadUntil(probe, 5*time.Second)
	for _, p := range peers {
		p.ReadUntil(probe, 5*time.Second)
	}
}

var _ = Describe("Telnet Vertical Slice E2E", func() {
	var (
		testCtx      context.Context
		testCancel   context.CancelFunc
		container    testcontainers.Container
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
		pgEnv, err := testutil.StartPostgres(testCtx)
		Expect(err).NotTo(HaveOccurred())
		container = pgEnv.Container
		connStr := pgEnv.ConnStr

		// 2. Run migrations
		migrator, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migrator.Up()).To(Succeed())
		_ = migrator.Close()

		// 3. Create event store
		eventStore, err = store.NewPostgresEventStore(testCtx, connStr)
		Expect(err).NotTo(HaveOccurred())

		// 3a. Obtain the underlying pgxpool for auth repos.
		pool := eventStore.Pool()
		Expect(pool).NotTo(BeNil())

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
		engine := core.NewEngine(eventStore)

		// 8. Create start location in DB and GuestAuthenticator
		startLocation = ulid.Make()
		_, locErr := pool.Exec(testCtx,
			`INSERT INTO locations (id, name, description) VALUES ($1, $2, $3)`,
			startLocation.String(), "Start Location", "The starting location for guests.",
		)
		Expect(locErr).NotTo(HaveOccurred())
		guestAuth = telnet.NewGuestAuthenticator(naming.NewGemstoneElementTheme(), startLocation)

		// 8a. Create auth repos and GuestService.
		playerRepo := authpostgres.NewPlayerRepository(pool)
		playerSessionRepo := store.NewPostgresPlayerSessionStore(pool)
		worldCharRepo := worldpostgres.NewCharacterRepository(pool)
		charRepoAdapter := &authCharRepoAdapter{pool: pool, charRepo: worldCharRepo}
		guestService, gsErr := auth.NewGuestService(guestAuth, playerRepo, charRepoAdapter, playerSessionRepo)
		Expect(gsErr).NotTo(HaveOccurred())

		// 9. Create gRPC server with command dispatcher
		sessStore := session.NewMemStore()
		reg := command.NewRegistry()
		handlers.RegisterAll(reg)
		registerTestCommands(reg)
		pe := policytest.AllowAllEngine()
		cmdSvc := command.NewTestServices(command.ServicesConfig{
			Session: sessStore,
			Engine:  pe,
			Events:  eventStore,
		})
		dispatcher, dispErr := command.NewDispatcher(reg, pe)
		Expect(dispErr).NotTo(HaveOccurred())

		coreServer := grpcpkg.NewCoreServer(engine, sessStore, dispatcher, cmdSvc,
			grpcpkg.WithAuthenticator(guestAuth),
			grpcpkg.WithEventStore(eventStore),
			grpcpkg.WithGuestService(guestService),
			grpcpkg.WithPlayerRepo(playerRepo),
			grpcpkg.WithPlayerSessionRepo(playerSessionRepo),
			grpcpkg.WithCharacterRepo(charRepoAdapter),
			grpcpkg.WithDisconnectHook(func(info session.Info) {
				// Release the guest name (stored as underscore form in the namer's
				// active set, but the session stores it as space-separated display form).
				rawName := strings.ReplaceAll(info.CharacterName, " ", "_")
				guestAuth.ReleaseGuest(rawName)
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
		sharedRegistry := core.NewVerbRegistry()
		err = core.RegisterBuiltinTypes(sharedRegistry)
		Expect(err).NotTo(HaveOccurred())

		var acceptCtx context.Context
		acceptCtx, acceptCancel = context.WithCancel(testCtx)
		go func() {
			for {
				conn, err := telnetLis.Accept()
				if err != nil {
					return
				}
				handler := telnet.NewGatewayHandler(conn, grpcCli, sharedRegistry)
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
			Expect(name).To(MatchRegexp(`^[A-Z][a-z]+ [A-Z][a-z]+$`))
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

		It("Player A says something and sees their own broadcast", func() {
			clientA.SendLine(`say Hello, world!`)
			line := clientA.ReadUntil("Hello, world!", 5*time.Second)
			Expect(line).To(ContainSubstring(`says, "Hello, world!"`))
		})

		It("Player B receives Player A's say via event stream", func() {
			clientA.SendLine(`say Greetings!`)
			// A sees own broadcast via event stream
			_ = clientA.ReadUntil("Greetings!", 5*time.Second)
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
			line := client.ReadUntil("Persistence test", 5*time.Second)
			Expect(line).To(ContainSubstring(`says, "Persistence test"`))

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
