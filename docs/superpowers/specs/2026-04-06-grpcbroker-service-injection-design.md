<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# GRPCBroker Service Injection for Binary Plugins

**Bead:** holomush-z82s (F-05) | **Status:** Draft | **Date:** 2026-04-06

## Overview

Binary plugins declare `requires: [holomush.world.v1.WorldService]` in their
manifest but never receive connections to those services. The
`ServiceConfig.required_services` map in `plugin.proto` is defined but never
populated. This design uses hashicorp/go-plugin's GRPCBroker to inject
required service connections from host to plugin, secured with mTLS using the
existing HoloMUSH CA infrastructure.

## RFC2119 Keywords

The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are used per RFC2119.

## Design Decisions

| #  | Decision | Rationale |
| -- | -------- | --------- |
| D1 | GRPCBroker for service injection | go-plugin's broker creates reverse gRPC servers over its multiplexed connection. No extra ports/sockets. Battle-tested (Terraform providers). |
| D2 | Generic transparent proxy (not per-service) | `UnknownServiceHandler` forwards all calls to the registry's `ClientConnInterface`. Zero per-service code. Adding a service = register + declare in manifest. |
| D3 | mTLS using existing HoloMUSH CA | Per-plugin certs generated at load time. Provides transport encryption + cryptographic plugin identity for ABAC. Consistent with core↔gateway TLS. |
| D4 | Ephemeral per-plugin certs | Generated on load, cleaned up on unload. Same lifecycle philosophy as ephemeral database passwords. No cert storage or rotation. |
| D5 | Observability at broker proxy | gRPC interceptors on broker servers capture per-plugin, per-service latency and error rates via OTel spans and slog. |

## 1. GRPCBroker Flow

When a binary plugin declares `requires`, the host makes each required service
callable from the plugin subprocess via the GRPCBroker.

### 1.1 During Host.Load()

1. Plugin process starts via go-plugin (existing code)
2. Host captures the `GRPCBroker` from `GRPCClient()` (currently ignored)
3. For each service in `manifest.Requires`:
   a. Resolve the service from `ServiceRegistry` → get `RegisteredService.Conn`
   b. Assign a broker ID (sequential `uint32`)
   c. Call `broker.AcceptAndServe(id, NewBrokerProxy(conn))` — starts a reverse
   gRPC server that transparently proxies all calls to the real service
   d. Add to `required_services` map:
   `"holomush.world.v1.WorldService" → "broker:42"`
4. Pass `required_services` in `InitRequest.Config`
5. Plugin calls `broker.Dial(42)` → gets a `grpc.ClientConn` for WorldService
6. Plugin creates typed client: `worldv1.NewWorldServiceClient(conn)`

### 1.2 Dependency Ordering Guarantee

The dependency resolver (`dependency.go`, Kahn's algorithm) guarantees that
by the time plugin A loads, all services in A's `requires` are already
registered in the `ServiceRegistry`. No race condition is possible because
plugins load sequentially in topological order.

### 1.3 Error Handling

If a required service is not found in the `ServiceRegistry` during
`Host.Load()`, the plugin MUST fail to load with error code
`PLUGIN_SERVICE_NOT_FOUND`. This is a fatal startup error — the dependency
resolver should have caught it, but the registry lookup is the final guard.

## 2. mTLS with Existing CA Infrastructure

The existing `internal/tls` package provides `GenerateCA`,
`GenerateServerCert`, and `GenerateClientCert` using ECDSA P-256. These are
currently used for core↔gateway gRPC connections.

### 2.1 Per-Plugin Cert Lifecycle

In go-plugin, the plugin subprocess is the gRPC **server** and the host is
the gRPC **client**. For mTLS:

1. During `Host.Load()`, generate certs for this plugin:
   - `GenerateServerCert(ca, gameID, "plugin-<name>")` — plugin's server cert
   - Host reuses its own client cert (generated once at Host construction)
2. Write plugin's server cert + key to a temp directory
3. Pass cert paths to plugin subprocess via environment variables:
   - `HOLOMUSH_PLUGIN_CERT` — plugin's server certificate PEM path
   - `HOLOMUSH_PLUGIN_KEY` — plugin's server private key PEM path
   - `HOLOMUSH_CA_CERT` — CA certificate PEM path (for verifying host's client cert)
4. Configure go-plugin's `ClientConfig.TLSConfig` on host side with:
   - CA cert as root (to verify plugin's server cert)
   - Host's client cert (for mTLS)
5. Plugin SDK's `Serve()` reads certs from env, configures `TLSProvider`
6. GRPCBroker **inherits** mTLS from the parent go-plugin connection — all
   brokered service connections are automatically mTLS-protected
7. On unload, temp cert directory is removed

### 2.2 Plugin Identity for ABAC

The plugin's certificate CN is `plugin-<name>` (e.g., `plugin-core-scenes`).
This provides a cryptographic identity that:

- ABAC policies can reference for authorization decisions
- Audit logs can use for attribution
- Cannot be spoofed by a plugin claiming a different name

This identity is available from the cert in `peer.AuthInfo` on the server
side of brokered connections.

### 2.3 Security Properties

| Threat | Mitigation |
| ------ | ---------- |
| Unauthorized process connects to broker port | Rejected — no valid client cert signed by game CA |
| Plugin impersonates the host | Rejected — no valid server cert |
| Eavesdropping on broker traffic | Encrypted via TLS |
| Plugin claims wrong identity | CN is embedded in cert by host, verified by CA chain |

## 3. Broker Proxy (Generic Transparent Proxy)

A new file `internal/plugin/goplugin/broker_proxy.go` provides the broker
server factory.

### 3.1 NewBrokerProxy

```text
NewBrokerProxy(conn grpc.ClientConnInterface, pluginName string) func([]grpc.ServerOption) *grpc.Server
```

Returns a factory function compatible with `broker.AcceptAndServe`. The
factory creates a `grpc.Server` with:

1. `UnknownServiceHandler` that transparently proxies all RPCs to `conn`
   (same bidirectional stream copy pattern as `GRPCServiceProxy` in
   `grpc_proxy.go`)
2. Unary and stream interceptors for observability (see Section 4)

### 3.2 Stream Proxy

The transparent proxy copies frames bidirectionally between the plugin's
broker stream and the real service's `ClientConnInterface`. This is the same
logic as `GRPCServiceProxy.streamHandler()` — extract method name from the
stream context, open a client stream on the target conn, and copy in both
directions.

The implementation SHOULD share code with `grpc_proxy.go` (either by
extracting the proxy logic into a shared function or by having
`broker_proxy.go` import and reuse the proxy core).

## 4. Observability

### 4.1 Interceptors on Broker Proxy

The broker proxy gRPC server includes unary and stream interceptors that:

- Log each call via slog at debug level: method, plugin name, duration, gRPC status code
- Emit an OpenTelemetry span per call with attributes:

| Attribute | Example value |
| --------- | ------------- |
| `rpc.system` | `grpc` |
| `rpc.service` | `holomush.world.v1.WorldService` |
| `rpc.method` | `GetLocation` |
| `plugin.name` | `core-scenes` |
| `plugin.type` | `binary` |
| `direction` | `plugin_to_host` |

### 4.2 Metrics

The OTel spans feed into standard gRPC metrics (request count, latency
histogram, error rate) with the plugin dimensions. This enables per-plugin,
per-service dashboards and alerting.

## 5. Code Changes

### 5.1 Modified Files

| File | Change |
| ---- | ------ |
| `internal/plugin/goplugin/host.go` | Capture `GRPCBroker`, wire mTLS, start broker servers for required services, populate `required_services` in InitRequest |
| `internal/plugin/goplugin/plugin.go` | Store `GRPCBroker` from `GRPCClient()` on plugin struct |
| `pkg/plugin/sdk.go` | Add `TLSProvider` to `Serve()`, load certs from env vars |
| `internal/plugin/setup/subsystem.go` | Pass CA to `goplugin.Host` constructor |

### 5.2 New Files

| File | Purpose |
| ---- | ------- |
| `internal/plugin/goplugin/broker_proxy.go` | Generic transparent proxy factory for broker.AcceptAndServe |
| `pkg/plugin/broker.go` | Plugin-side helpers: `ParseBrokerServices(map)`, `DialBrokerService(broker, id)` |

### 5.3 Plugin SDK (pkg/plugin)

**sdk.go changes:**

```text
Serve(impl PluginServiceServer)
  - Reads HOLOMUSH_PLUGIN_CERT, HOLOMUSH_PLUGIN_KEY, HOLOMUSH_CA_CERT from env
  - Configures TLSProvider for go-plugin's ServeConfig
  - Plugin authors call Serve() from main() — TLS is transparent
```

**broker.go (new):**

```text
ParseBrokerServices(services map[string]string) map[string]uint32
  - Parses "broker:42" → 42 for each entry
  - Returns map of service name → broker ID

DialBrokerService(broker *goplugin.GRPCBroker, id uint32) (*grpc.ClientConn, error)
  - Calls broker.Dial(id)
  - Returns a grpc.ClientConn the plugin can use to create typed clients
```

### 5.4 Host Construction

`goplugin.NewHost()` gains a `CA` parameter (the game's root CA). The host
generates its own client cert once at construction
(`GenerateClientCert(ca, "holomush-plugin-host")`) and per-plugin server
certs during Load().

## 6. Testing Strategy

| Test | Type | Description |
| ---- | ---- | ----------- |
| Broker proxy forwards unary RPCs | Unit | Mock `grpc.ClientConnInterface`, verify calls reach it |
| Broker proxy forwards streaming RPCs | Unit | Bidirectional stream proxy |
| Plugin receives required_services in Init | Integration | Load core-scenes, verify Init gets WorldService broker ID |
| Plugin calls WorldService via broker | Integration | core-scenes calls WorldService.GetLocation through broker, gets real response |
| mTLS rejects unsigned client | Integration | Direct TCP connect to broker port without valid cert → rejected |
| Plugin without requires gets empty map | Unit | Load plugin with no requires, verify empty `required_services` |
| Missing required service fails load | Unit | Plugin requires unregistered service, verify `PLUGIN_SERVICE_NOT_FOUND` error |
| OTel span emitted for broker call | Integration | Call through broker, verify span with correct attributes |

Integration tests use `testutil.StartPostgres` and the binary plugin test
fixture in `test/integration/plugin/`.
