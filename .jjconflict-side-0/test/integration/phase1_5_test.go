// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package integration provides end-to-end integration tests for HoloMUSH.
package integration

import (
	"context"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/holomush/holomush/internal/control"
	"github.com/holomush/holomush/internal/core"
	grpcpkg "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/store"
	tlscerts "github.com/holomush/holomush/internal/tls"
	controlv1 "github.com/holomush/holomush/pkg/proto/holomush/control/v1"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// testEnv holds all the resources needed for integration tests.
type testEnv struct {
	ctx           context.Context
	cancel        context.CancelFunc
	container     testcontainers.Container
	store         *store.PostgresEventStore
	certsDir      string
	runtimeDir    string
	gameID        string
	grpcAddr      string
	grpcServer    *grpc.Server
	grpcListener  net.Listener
	controlServer *control.GRPCServer
	controlAddr   string
}

// setupTestEnv creates a complete test environment with PostgreSQL, TLS certs, and directories.
func setupTestEnv() (*testEnv, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)

	env := &testEnv{
		ctx:    ctx,
		cancel: cancel,
	}

	// Create temp directories
	tmpDir, err := os.MkdirTemp("", "holomush-test-*")
	if err != nil {
		cancel()
		return nil, err
	}
	env.certsDir = filepath.Join(tmpDir, "certs")
	env.runtimeDir = filepath.Join(tmpDir, "run")

	if err := os.MkdirAll(env.certsDir, 0o700); err != nil {
		cancel()
		return nil, err
	}
	if err := os.MkdirAll(env.runtimeDir, 0o700); err != nil {
		cancel()
		return nil, err
	}

	// Set XDG env vars for control socket
	os.Setenv("XDG_RUNTIME_DIR", tmpDir)

	// Start PostgreSQL container
	container, err := postgres.Run(ctx,
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
	if err != nil {
		cancel()
		return nil, err
	}
	env.container = container

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, err
	}

	// Run migrations using the new Migrator
	migrator, err := store.NewMigrator(connStr)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, err
	}
	if err := migrator.Up(); err != nil {
		_ = migrator.Close()
		_ = container.Terminate(ctx)
		return nil, err
	}
	_ = migrator.Close()

	// Create event store
	env.store, err = store.NewPostgresEventStore(ctx, connStr)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, err
	}

	return env, nil
}

// cleanup releases all test resources.
func (env *testEnv) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if env.controlServer != nil {
		_ = env.controlServer.Stop(ctx)
	}

	if env.grpcServer != nil {
		env.grpcServer.GracefulStop()
	}

	if env.grpcListener != nil {
		_ = env.grpcListener.Close()
	}

	if env.store != nil {
		env.store.Close()
	}

	if env.container != nil {
		_ = env.container.Terminate(ctx)
	}

	env.cancel()
}

var _ = Describe("Phase 1.5 Integration", func() {
	var env *testEnv

	BeforeEach(func() {
		var err error
		env, err = setupTestEnv()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		env.cleanup()
	})

	Describe("Database Migrations", func() {
		It("verifies migrations run successfully and system_info table exists", func() {
			err := env.store.SetSystemInfo(env.ctx, "test_key", "test_value")
			Expect(err).NotTo(HaveOccurred())

			value, err := env.store.GetSystemInfo(env.ctx, "test_key")
			Expect(err).NotTo(HaveOccurred())
			Expect(value).To(Equal("test_value"))
		})
	})

	Describe("Game ID Generation", func() {
		It("generates and persists game_id", func() {
			gameID, err := env.store.InitGameID(env.ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(gameID).To(HaveLen(26), "game_id should be a valid ULID (26 characters)")

			// Verify it persists (calling again returns same ID)
			gameID2, err := env.store.InitGameID(env.ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(gameID2).To(Equal(gameID), "game_id should persist between calls")

			// Verify it's stored in database
			storedID, err := env.store.GetSystemInfo(env.ctx, "game_id")
			Expect(err).NotTo(HaveOccurred())
			Expect(storedID).To(Equal(gameID))
		})
	})

	Describe("TLS Certificate Generation", func() {
		It("generates TLS certs with correct game_id in SAN", func() {
			gameID, err := env.store.InitGameID(env.ctx)
			Expect(err).NotTo(HaveOccurred())
			env.gameID = gameID

			// Generate CA
			ca, err := tlscerts.GenerateCA(gameID)
			Expect(err).NotTo(HaveOccurred())

			// Generate server cert
			serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
			Expect(err).NotTo(HaveOccurred())

			// Save certificates
			err = tlscerts.SaveCertificates(env.certsDir, ca, serverCert)
			Expect(err).NotTo(HaveOccurred())

			// Generate and save client cert
			clientCert, err := tlscerts.GenerateClientCert(ca, "gateway")
			Expect(err).NotTo(HaveOccurred())
			err = tlscerts.SaveClientCert(env.certsDir, clientCert)
			Expect(err).NotTo(HaveOccurred())

			// Verify server cert has correct SAN
			expectedSAN := "holomush-" + gameID
			Expect(serverCert.Certificate.DNSNames).To(ContainElement(expectedSAN))

			// Verify CA has game_id in CN
			expectedCN := "HoloMUSH CA " + gameID
			Expect(ca.Certificate.Subject.CommonName).To(Equal(expectedCN))

			// Verify CA has game_id in URI SAN
			expectedURI := "holomush://game/" + gameID
			var foundURI bool
			for _, uri := range ca.Certificate.URIs {
				if uri.String() == expectedURI {
					foundURI = true
					break
				}
			}
			Expect(foundURI).To(BeTrue(), "CA should have game_id in URI SAN")

			// Verify client cert CN
			Expect(clientCert.Certificate.Subject.CommonName).To(Equal("holomush-gateway"))

			// Verify files were created
			files := []string{"root-ca.crt", "root-ca.key", "core.crt", "core.key", "gateway.crt", "gateway.key"}
			for _, f := range files {
				path := filepath.Join(env.certsDir, f)
				_, err := os.Stat(path)
				Expect(err).NotTo(HaveOccurred(), "expected file %s to exist", path)
			}
		})
	})

	Describe("gRPC Server mTLS", func() {
		It("starts and accepts mTLS connections", func() {
			gameID, err := env.store.InitGameID(env.ctx)
			Expect(err).NotTo(HaveOccurred())
			env.gameID = gameID

			// Generate and save certificates
			ca, err := tlscerts.GenerateCA(gameID)
			Expect(err).NotTo(HaveOccurred())
			serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
			Expect(err).NotTo(HaveOccurred())
			err = tlscerts.SaveCertificates(env.certsDir, ca, serverCert)
			Expect(err).NotTo(HaveOccurred())
			clientCert, err := tlscerts.GenerateClientCert(ca, "gateway")
			Expect(err).NotTo(HaveOccurred())
			err = tlscerts.SaveClientCert(env.certsDir, clientCert)
			Expect(err).NotTo(HaveOccurred())

			// Load server TLS config
			serverTLS, err := tlscerts.LoadServerTLS(env.certsDir, "core")
			Expect(err).NotTo(HaveOccurred())

			// Create core components
			eventStore := core.NewMemoryEventStore()
			broadcaster := core.NewBroadcaster()
			sessions := core.NewSessionManager()
			engine := core.NewEngine(eventStore, sessions, broadcaster)

			// Create gRPC server with mTLS
			coreServer := grpcpkg.NewCoreServer(engine, sessions, broadcaster)
			env.grpcServer = grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
			corev1.RegisterCoreServer(env.grpcServer, coreServer)

			// Start listening
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			Expect(err).NotTo(HaveOccurred())
			env.grpcListener = listener
			env.grpcAddr = listener.Addr().String()

			go func() {
				_ = env.grpcServer.Serve(listener)
			}()

			// Wait for server to be ready
			Eventually(func() bool {
				conn, err := net.DialTimeout("tcp", env.grpcAddr, 100*time.Millisecond)
				if err != nil {
					return false
				}
				conn.Close()
				return true
			}).Should(BeTrue())

			// Load client TLS config
			clientTLS, err := tlscerts.LoadClientTLS(env.certsDir, "gateway", gameID)
			Expect(err).NotTo(HaveOccurred())

			// Create gRPC client with mTLS
			client, err := grpcpkg.NewClient(env.ctx, grpcpkg.ClientConfig{
				Address:   env.grpcAddr,
				TLSConfig: clientTLS,
			})
			Expect(err).NotTo(HaveOccurred())
			defer client.Close()

			// Test connection by attempting to authenticate (will fail auth but confirms connection works)
			resp, err := client.Authenticate(env.ctx, &corev1.AuthRequest{
				Username: "test",
				Password: "test",
			})
			Expect(err).NotTo(HaveOccurred())

			// We expect auth to fail (no authenticator configured) but the RPC should succeed
			Expect(resp.Success).To(BeFalse())
		})

		It("rejects connections with invalid certs", func() {
			gameID, err := env.store.InitGameID(env.ctx)
			Expect(err).NotTo(HaveOccurred())
			env.gameID = gameID

			// Generate and save certificates
			ca, err := tlscerts.GenerateCA(gameID)
			Expect(err).NotTo(HaveOccurred())
			serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
			Expect(err).NotTo(HaveOccurred())
			err = tlscerts.SaveCertificates(env.certsDir, ca, serverCert)
			Expect(err).NotTo(HaveOccurred())

			// Load server TLS config
			serverTLS, err := tlscerts.LoadServerTLS(env.certsDir, "core")
			Expect(err).NotTo(HaveOccurred())

			// Create core components
			eventStore := core.NewMemoryEventStore()
			broadcaster := core.NewBroadcaster()
			sessions := core.NewSessionManager()
			engine := core.NewEngine(eventStore, sessions, broadcaster)

			// Create gRPC server with mTLS
			coreServer := grpcpkg.NewCoreServer(engine, sessions, broadcaster)
			env.grpcServer = grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
			corev1.RegisterCoreServer(env.grpcServer, coreServer)

			// Start listening
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			Expect(err).NotTo(HaveOccurred())
			env.grpcListener = listener
			env.grpcAddr = listener.Addr().String()

			go func() {
				_ = env.grpcServer.Serve(listener)
			}()

			// Wait for server to be ready
			time.Sleep(100 * time.Millisecond)

			// Generate a different CA (attacker's CA)
			badCA, err := tlscerts.GenerateCA("bad-game-id")
			Expect(err).NotTo(HaveOccurred())
			badCertsDir := filepath.Join(env.certsDir, "bad")
			err = os.MkdirAll(badCertsDir, 0o700)
			Expect(err).NotTo(HaveOccurred())
			badClientCert, err := tlscerts.GenerateClientCert(badCA, "gateway")
			Expect(err).NotTo(HaveOccurred())
			err = tlscerts.SaveCertificates(badCertsDir, badCA, nil)
			Expect(err).NotTo(HaveOccurred())
			err = tlscerts.SaveClientCert(badCertsDir, badClientCert)
			Expect(err).NotTo(HaveOccurred())

			// Try to connect with bad cert
			badClientTLS, err := tlscerts.LoadClientTLS(badCertsDir, "gateway", "bad-game-id")
			Expect(err).NotTo(HaveOccurred())

			client, err := grpcpkg.NewClient(env.ctx, grpcpkg.ClientConfig{
				Address:   env.grpcAddr,
				TLSConfig: badClientTLS,
			})
			Expect(err).NotTo(HaveOccurred())
			defer client.Close()

			// Attempt RPC - should fail due to cert mismatch
			_, err = client.Authenticate(env.ctx, &corev1.AuthRequest{
				Username: "test",
				Password: "test",
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Control Server gRPC", func() {
		It("responds to status requests", func() {
			gameID, err := env.store.InitGameID(env.ctx)
			Expect(err).NotTo(HaveOccurred())
			env.gameID = gameID

			// Generate and save certificates
			ca, err := tlscerts.GenerateCA(gameID)
			Expect(err).NotTo(HaveOccurred())
			serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
			Expect(err).NotTo(HaveOccurred())
			err = tlscerts.SaveCertificates(env.certsDir, ca, serverCert)
			Expect(err).NotTo(HaveOccurred())
			clientCert, err := tlscerts.GenerateClientCert(ca, "gateway")
			Expect(err).NotTo(HaveOccurred())
			err = tlscerts.SaveClientCert(env.certsDir, clientCert)
			Expect(err).NotTo(HaveOccurred())

			var shutdownCalled atomic.Bool
			shutdownFunc := func() {
				shutdownCalled.Store(true)
			}

			// Create and start control server
			env.controlServer, err = control.NewGRPCServer("core", shutdownFunc)
			Expect(err).NotTo(HaveOccurred())

			// Load server TLS config
			serverTLS, err := control.LoadControlServerTLS(env.certsDir, "core")
			Expect(err).NotTo(HaveOccurred())

			// Start on random port
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			Expect(err).NotTo(HaveOccurred())
			env.controlAddr = listener.Addr().String()
			listener.Close() // Close so the server can bind

			_, err = env.controlServer.Start(env.controlAddr, serverTLS)
			Expect(err).NotTo(HaveOccurred())

			// Wait for server to be ready
			Eventually(func() bool {
				conn, err := net.DialTimeout("tcp", env.controlAddr, 100*time.Millisecond)
				if err != nil {
					return false
				}
				conn.Close()
				return true
			}).Should(BeTrue())

			// Load client TLS config
			clientTLS, err := control.LoadControlClientTLS(env.certsDir, "gateway", gameID)
			Expect(err).NotTo(HaveOccurred())

			// Create gRPC client
			conn, err := grpc.NewClient(env.controlAddr, grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			client := controlv1.NewControlClient(conn)

			// Test Status RPC
			statusResp, err := client.Status(env.ctx, &controlv1.StatusRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(statusResp.Running).To(BeTrue())
			Expect(statusResp.Component).To(Equal("core"))

			// Test Shutdown RPC
			shutdownResp, err := client.Shutdown(env.ctx, &controlv1.ShutdownRequest{Graceful: true})
			Expect(err).NotTo(HaveOccurred())
			Expect(shutdownResp.Message).To(Equal("shutdown initiated"))

			// Wait for shutdown to be triggered
			Eventually(func() bool {
				return shutdownCalled.Load()
			}).Should(BeTrue())
		})
	})

	Describe("End-to-End Workflow", func() {
		It("completes the full Phase 1.5 workflow", func() {
			By("Step 1: Database migrations (done in setup)")

			By("Step 2: Initialize game_id")
			gameID, err := env.store.InitGameID(env.ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(gameID).To(HaveLen(26))
			env.gameID = gameID

			By("Step 3: Generate TLS certs with game_id")
			ca, err := tlscerts.GenerateCA(gameID)
			Expect(err).NotTo(HaveOccurred())
			serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
			Expect(err).NotTo(HaveOccurred())
			err = tlscerts.SaveCertificates(env.certsDir, ca, serverCert)
			Expect(err).NotTo(HaveOccurred())
			clientCert, err := tlscerts.GenerateClientCert(ca, "gateway")
			Expect(err).NotTo(HaveOccurred())
			err = tlscerts.SaveClientCert(env.certsDir, clientCert)
			Expect(err).NotTo(HaveOccurred())

			// Verify SAN contains game_id
			Expect(serverCert.Certificate.DNSNames).To(ContainElement("holomush-" + gameID))

			By("Step 4: Start gRPC server with mTLS")
			serverTLS, err := tlscerts.LoadServerTLS(env.certsDir, "core")
			Expect(err).NotTo(HaveOccurred())

			eventStore := core.NewMemoryEventStore()
			broadcaster := core.NewBroadcaster()
			sessions := core.NewSessionManager()
			engine := core.NewEngine(eventStore, sessions, broadcaster)

			coreServer := grpcpkg.NewCoreServer(engine, sessions, broadcaster)
			env.grpcServer = grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
			corev1.RegisterCoreServer(env.grpcServer, coreServer)

			listener, err := net.Listen("tcp", "127.0.0.1:0")
			Expect(err).NotTo(HaveOccurred())
			env.grpcListener = listener
			env.grpcAddr = listener.Addr().String()

			go func() {
				_ = env.grpcServer.Serve(listener)
			}()

			Eventually(func() bool {
				conn, err := net.DialTimeout("tcp", env.grpcAddr, 100*time.Millisecond)
				if err != nil {
					return false
				}
				conn.Close()
				return true
			}).Should(BeTrue())

			By("Step 5: Connect gRPC client")
			clientTLS, err := tlscerts.LoadClientTLS(env.certsDir, "gateway", gameID)
			Expect(err).NotTo(HaveOccurred())

			client, err := grpcpkg.NewClient(env.ctx, grpcpkg.ClientConfig{
				Address:   env.grpcAddr,
				TLSConfig: clientTLS,
			})
			Expect(err).NotTo(HaveOccurred())
			defer client.Close()

			By("Step 6: Verify authentication works (RPC succeeds, auth fails as expected)")
			resp, err := client.Authenticate(env.ctx, &corev1.AuthRequest{
				Username: "test",
				Password: "test",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Success).To(BeFalse())

			By("Step 7: Start control gRPC server")
			shutdownCh := make(chan struct{})
			env.controlServer, err = control.NewGRPCServer("core", func() {
				close(shutdownCh)
			})
			Expect(err).NotTo(HaveOccurred())

			controlServerTLS, err := control.LoadControlServerTLS(env.certsDir, "core")
			Expect(err).NotTo(HaveOccurred())

			// Start on random port
			controlListener, err := net.Listen("tcp", "127.0.0.1:0")
			Expect(err).NotTo(HaveOccurred())
			env.controlAddr = controlListener.Addr().String()
			controlListener.Close()

			_, err = env.controlServer.Start(env.controlAddr, controlServerTLS)
			Expect(err).NotTo(HaveOccurred())

			By("Step 8: Verify control server responds to status")
			controlClientTLS, err := control.LoadControlClientTLS(env.certsDir, "gateway", gameID)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				conn, err := net.DialTimeout("tcp", env.controlAddr, 100*time.Millisecond)
				if err != nil {
					return false
				}
				conn.Close()
				return true
			}).Should(BeTrue())

			controlConn, err := grpc.NewClient(env.controlAddr, grpc.WithTransportCredentials(credentials.NewTLS(controlClientTLS)))
			Expect(err).NotTo(HaveOccurred())
			defer controlConn.Close()

			controlClient := controlv1.NewControlClient(controlConn)

			statusResp, err := controlClient.Status(env.ctx, &controlv1.StatusRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(statusResp.Running).To(BeTrue())
			Expect(statusResp.Component).To(Equal("core"))

			By("Step 9: Clean shutdown")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err = env.controlServer.Stop(ctx)
			Expect(err).NotTo(HaveOccurred())
			env.controlServer = nil // Prevent double-stop in cleanup

			env.grpcServer.GracefulStop()
			env.grpcServer = nil // Prevent double-stop in cleanup
		})
	})

	Describe("Certificate Chain Validation", func() {
		It("verifies the certificate chain is properly validated", func() {
			gameID, err := env.store.InitGameID(env.ctx)
			Expect(err).NotTo(HaveOccurred())

			// Generate CA and certs
			ca, err := tlscerts.GenerateCA(gameID)
			Expect(err).NotTo(HaveOccurred())
			serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
			Expect(err).NotTo(HaveOccurred())
			clientCert, err := tlscerts.GenerateClientCert(ca, "gateway")
			Expect(err).NotTo(HaveOccurred())

			// Verify server cert is signed by CA
			caPool := x509.NewCertPool()
			caPool.AddCert(ca.Certificate)

			chains, err := serverCert.Certificate.Verify(x509.VerifyOptions{
				Roots:     caPool,
				KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(chains).NotTo(BeEmpty(), "server cert should have valid chains to CA")

			// Verify client cert is signed by CA
			chains, err = clientCert.Certificate.Verify(x509.VerifyOptions{
				Roots:     caPool,
				KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(chains).NotTo(BeEmpty(), "client cert should have valid chains to CA")
		})
	})
})
