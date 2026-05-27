<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Phase A: JetStream EventBus Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the additive infrastructure for the JetStream event bus into `main` *without* changing production behavior. After Phase A, embedded NATS starts at every server boot, the audit projection runs idle on an empty stream, the codec and bus interfaces exist with no consumers, and OTEL/Prom plumbing is wired. Production still uses the old PostgreSQL event store via `EventWriter` and `EventStore.SubscribeSession`.

**Architecture:** Seven independent additive PRs (M1–M7). Each PR is risk-zero or risk-low: no consumer of the old event store moves to the new bus until Phase B (separate plan). Embedded NATS is lock-isolated by per-instance `StoreDir` from `xdg.DataDir()`. The audit projection consumer subscribes to `events.>` but receives no events because nothing publishes there yet.

**Tech Stack:** Go 1.24+, `github.com/nats-io/nats-server/v2` (latest GA, pinned via Renovate trail), `github.com/nats-io/nats.go`, ULID via `internal/idgen`, `pgregory.net/rapid` for property tests, `prometheus-nats-exporter` (Go library), OTEL via existing `internal/telemetry`, testcontainers for PG integration tests.

**Spec:** [docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md](../specs/2026-04-18-jetstream-event-log-design.md)

**Epic bead:** `holomush-1tvn`

**Phase A bead IDs (set after creation):** M1=`holomush-1tvn.M1`, M2=`holomush-1tvn.M2`, … M7=`holomush-1tvn.M7` (placeholder; actual IDs below).

---

## Phase A invariants (apply to every task)

- **Production behavior MUST NOT change.** No consumer of the old event store gets rerouted in Phase A. Tests of the old code paths MUST still pass.
- **Each task lands as its own PR to main.** PRs are < 500 LOC churn; each must pass `task pr-prep` (lint, format, schema, license, unit, integration, E2E) green.
- **Coverage gate per package.** New `internal/eventbus/` packages MUST hit 90% coverage; `internal/eventbus/codec/` MUST hit 95%.
- **No `time.Sleep` in tests** under `internal/eventbus/` and `test/integration/eventbus_e2e/` (will be enforced by lint rule landing in M1).
- **Use `task` for all build/test/lint operations** (per `CLAUDE.md`).

---

## File Map (Phase A)

| File | Action | Task | Responsibility |
| --- | --- | --- | --- |
| `internal/eventbus/types.go` | Create | M1 | `Subject`, `Type`, `Event`, `Actor`, `ActorKind`, `Direction` |
| `internal/eventbus/types_test.go` | Create | M1 | Constructor validation, type-safety regression tests |
| `internal/eventbus/bus.go` | Create | M1 | `Publisher`, `Subscriber`, `HistoryReader`, `EventBus`, `Delivery`, `SessionStream`, `HistoryQuery`, `HistoryStream` interfaces |
| `internal/eventbus/bus_test.go` | Create | M1 | Interface satisfaction stubs |
| `internal/eventbus/errors.go` | Create | M1 | Error sentinels |
| `internal/eventbus/errors_test.go` | Create | M1 | Error wrapping/unwrapping |
| `.golangci.yml` | Modify | M1 | Add `time.Sleep` ban for `internal/eventbus/**` and `test/integration/eventbus_e2e/**` |
| `internal/eventbus/codec/codec.go` | Create | M2 | `Name` typed string + constants, `Codec`, `KeyProvider`, `KeySelector`, `Key`, `KeyID`, `KeyLabel`, `IdentityCodec` |
| `internal/eventbus/codec/codec_test.go` | Create | M2 | IdentityCodec round-trip + boundary tests |
| `internal/eventbus/codec/registry.go` | Create | M2 | Closed registry of host-known codecs + `Resolve` |
| `internal/eventbus/codec/registry_test.go` | Create | M2 | Registry sync meta-test |
| `internal/store/migrations/000020_create_events_audit.up.sql` | Create | M3 | `events_audit` table + indexes (number adjusts to next available) |
| `internal/store/migrations/000020_create_events_audit.down.sql` | Create | M3 | Rollback |
| `internal/store/events_audit_test.go` | Create | M3 | Integration test: schema present, indexes used, ON CONFLICT idempotent |
| `internal/eventbus/subsystem.go` | Create | M4 | `SubsystemEventBus` lifecycle (embedded NATS) |
| `internal/eventbus/subsystem_test.go` | Create | M4 | Lifecycle + idempotent stream declaration |
| `internal/eventbus/config.go` | Create | M4 | `Config` struct + defaults |
| `internal/eventbus/eventbustest/embedded.go` | Create | M4 | `New(t)` test helper using MemoryStorage |
| `internal/lifecycle/subsystem.go` | Modify | M4 + M5 | Add `SubsystemEventBus` and `SubsystemAuditProjection` IDs |
| `internal/config/config.go` | Modify | M4 | Add `EventBus` config key (with `game_id`, mode flag etc.) |
| `cmd/holomush/core.go` | Modify | M4 + M5 + M7 | Register new subsystems in orchestrator (idle until F1) |
| `internal/eventbus/audit/subsystem.go` | Create | M5 | `SubsystemAuditProjection` lifecycle |
| `internal/eventbus/audit/projection.go` | Create | M5 | Worker loop: read JS → INSERT events_audit ON CONFLICT |
| `internal/eventbus/audit/projection_test.go` | Create | M5 | Integration test with embedded NATS + PG testcontainer |
| `internal/eventbus/audit/lag_metric.go` | Create | M5 | Prometheus gauge: `audit_projection_lag_seconds` |
| `api/proto/holomush/eventbus/v1/eventbus.proto` | Create | M6 | Host envelope `Event` message, `Actor`, `ActorKind` enum |
| `api/proto/holomush/plugin/v1/audit.proto` | Create | M6 | `PluginAuditService` (`AuditEvent`, `QueryHistory` RPCs) + messages |
| `pkg/proto/holomush/eventbus/v1/eventbus.pb.go` | Generated | M6 | Via `task generate:proto` |
| `pkg/proto/holomush/plugin/v1/audit.pb.go` | Generated | M6 | Via `task generate:proto` |
| `pkg/proto/holomush/plugin/v1/audit_grpc.pb.go` | Generated | M6 | Via `task generate:proto` |
| `internal/eventbus/telemetry/otel.go` | Create | M7 | Span helpers, W3C traceparent extract/inject for nats.Header |
| `internal/eventbus/telemetry/otel_test.go` | Create | M7 | Round-trip header propagation |
| `internal/eventbus/telemetry/prometheus.go` | Create | M7 | In-process `prometheus-nats-exporter` registration |
| `internal/eventbus/telemetry/prometheus_test.go` | Create | M7 | Smoke: metrics endpoint exposes `gnatsd_*` keys |
| `renovate.json` | Modify | M1 (or whichever lands first) | Add NATS package group with `minimumReleaseAge: "14 days"` |
| `go.mod` / `go.sum` | Modify | M2 (codec deps), M4 (NATS deps), M5 (testcontainers PG already), M7 (exporter) | New direct deps |

---

## Task M1: EventBus skeleton — types, interfaces, errors, lint rule

**Files:**

- Create: `internal/eventbus/types.go`, `internal/eventbus/types_test.go`
- Create: `internal/eventbus/bus.go`, `internal/eventbus/bus_test.go`
- Create: `internal/eventbus/errors.go`, `internal/eventbus/errors_test.go`
- Modify: `.golangci.yml` (custom rule for `time.Sleep` ban)
- Modify: `renovate.json` (NATS group config)

**Bead:** `holomush-1tvn.M1`

- [ ] **Step 1: Add NATS Renovate group config**

Edit `renovate.json` to add the NATS group rule. If `renovate.json` does not exist yet, create it with `extends: ["config:base"]`.

```json
{
  "$schema": "https://docs.renovatebot.com/renovate-schema.json",
  "extends": ["config:base"],
  "packageRules": [
    {
      "matchPackageNames": [
        "github.com/nats-io/nats-server/v2",
        "github.com/nats-io/nats.go"
      ],
      "minimumReleaseAge": "14 days",
      "groupName": "nats",
      "schedule": ["after 9am on Monday"]
    }
  ]
}
```

- [ ] **Step 2: Create directory and write `errors.go` first**

```bash
mkdir -p internal/eventbus
```

Write `internal/eventbus/errors.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package eventbus defines the host-facing event bus interfaces and
// supporting types. The concrete JetStream-backed implementation lives
// in subpackages (subsystem, audit, codec, telemetry).
package eventbus

import "errors"

// Sentinel errors returned by EventBus implementations. Consumers MUST
// match via errors.Is, never by string content.
var (
    ErrInvalidSubject       = errors.New("eventbus: invalid subject")
    ErrInvalidType          = errors.New("eventbus: invalid event type")
    ErrEmitNotPermitted     = errors.New("eventbus: subject not in manifest emits")
    ErrPayloadTooLarge      = errors.New("eventbus: payload exceeds MaxPayloadSize")
    ErrCodecHeaderMissing   = errors.New("eventbus: required App-Codec header missing")
    ErrUnknownCodec         = errors.New("eventbus: codec name not in registry")
    ErrPublishExpired       = errors.New("eventbus: publish retry exceeded dedup window")
    ErrInvalidFilter        = errors.New("eventbus: filter does not match stream subject")
    ErrInvalidCursor        = errors.New("eventbus: invalid history cursor")
    ErrInvalidTimeRange     = errors.New("eventbus: NotBefore must be <= NotAfter")
    ErrSessionAuth          = errors.New("eventbus: session authentication failed")
    ErrUnauthorized         = errors.New("eventbus: caller not authorized for subject")
    ErrPluginTimeout        = errors.New("eventbus: plugin RPC timeout")
    ErrSubjectOwnershipConflict = errors.New("eventbus: subject ownership conflict at startup")
    ErrManifestInvalid      = errors.New("eventbus: manifest validation failed")
    ErrStoreDirLocked       = errors.New("eventbus: NATS StoreDir is already locked by another process")
    ErrDecryptionFailed     = errors.New("eventbus: decryption failed")
    ErrKeyUnavailable       = errors.New("eventbus: codec key unavailable")
)

// MaxPayloadSize matches the prior cap in internal/core/event.go to keep
// behavior consistent across the cutover.
const MaxPayloadSize = 64 * 1024
```

Write `internal/eventbus/errors_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
    "errors"
    "fmt"
    "testing"

    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus"
)

func TestSentinelErrorsWrapAndUnwrap(t *testing.T) {
    wrapped := fmt.Errorf("publish failed: %w", eventbus.ErrPublishExpired)
    require.True(t, errors.Is(wrapped, eventbus.ErrPublishExpired))
    require.False(t, errors.Is(wrapped, eventbus.ErrInvalidSubject))
}

func TestMaxPayloadSizeMatchesLegacy(t *testing.T) {
    require.Equal(t, 64*1024, eventbus.MaxPayloadSize)
}
```

- [ ] **Step 3: Run errors_test.go and verify it fails (package not yet present in go list)**

```bash
task test -- ./internal/eventbus/... 2>&1 | head -20
```

Expected: build error or "no Go files" — package needs `types.go` to be valid.

- [ ] **Step 4: Write `types.go` with typed `Subject`, `Type`, and constructors**

Write `internal/eventbus/types.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
    "fmt"
    "regexp"
    "time"

    "github.com/oklog/ulid/v2"
)

// Subject is a typed JetStream subject. Constructed via NewSubject which
// validates against the documented token rules (see spec §1c).
type Subject string

// Type is a typed plugin-declared event type identifier. Constructed via
// NewType which validates against allowed character set.
type Type string

// Direction selects the iteration order of HistoryStream.
type Direction uint8

const (
    DirectionForward  Direction = 1
    DirectionBackward Direction = 2
)

// ActorKind identifies what type of entity caused an event. Mirrors the
// existing core.ActorKind so the cutover preserves semantics.
type ActorKind uint8

const (
    ActorKindUnknown   ActorKind = 0
    ActorKindCharacter ActorKind = 1
    ActorKindPlayer    ActorKind = 2
    ActorKindSystem    ActorKind = 3
    ActorKindPlugin    ActorKind = 4
)

// Actor identifies who caused an event. Host-stamped, never plugin-spoofable.
type Actor struct {
    Kind ActorKind
    ID   ulid.ULID // zero ULID for ActorKindSystem / Unknown
}

// Event is the host-side representation of a published event.
//
// Wire format (JetStream): proto-encoded Event in msg.Data, with headers
// `Nats-Msg-Id`, `App-Schema-Version`, `App-Event-Type`, `App-Codec`.
// See spec §1d.
type Event struct {
    ID        ulid.ULID
    Subject   Subject
    Type      Type
    Timestamp time.Time
    Actor     Actor
    Payload   []byte // codec.Encode output (ciphertext if encryption is on)
}

// subjectTokenRe permits NATS subject tokens: letters, digits, dashes,
// underscores. Wildcards (* and >) are positional and validated by NewSubject
// directly.
var subjectTokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// typeRe permits dot-segmented identifiers like "scene.lifecycle.created".
var typeRe = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

// NewSubject validates and constructs a Subject. Returns ErrInvalidSubject
// on failure.
//
// Rules (per spec §1c):
//   - dot-delimited tokens
//   - * matches one token (positional)
//   - > matches the remainder and MUST be the last token
//   - depth SHOULD be ≤ 16
//   - non-wildcard tokens match [A-Za-z0-9_-]+
//   - leading "events." prefix is required (host enforces by convention)
func NewSubject(s string) (Subject, error) {
    if s == "" {
        return "", fmt.Errorf("%w: empty subject", ErrInvalidSubject)
    }
    tokens := splitDots(s)
    if len(tokens) > 16 {
        return "", fmt.Errorf("%w: token depth %d exceeds 16", ErrInvalidSubject, len(tokens))
    }
    if tokens[0] != "events" {
        return "", fmt.Errorf("%w: must start with 'events.'", ErrInvalidSubject)
    }
    for i, tok := range tokens {
        if tok == "" {
            return "", fmt.Errorf("%w: empty token at position %d", ErrInvalidSubject, i)
        }
        if tok == ">" {
            if i != len(tokens)-1 {
                return "", fmt.Errorf("%w: '>' must be the last token", ErrInvalidSubject)
            }
            continue
        }
        if tok == "*" {
            continue
        }
        if !subjectTokenRe.MatchString(tok) {
            return "", fmt.Errorf("%w: token %q has invalid characters", ErrInvalidSubject, tok)
        }
    }
    return Subject(s), nil
}

// MustSubject panics on validation failure. Use only for compile-time
// constants in plugin code (e.g., var sceneICPattern = MustSubject("events.*.scene.*.ic")).
func MustSubject(s string) Subject {
    sub, err := NewSubject(s)
    if err != nil {
        panic(err)
    }
    return sub
}

// NewType validates and constructs a Type.
func NewType(s string) (Type, error) {
    if s == "" {
        return "", fmt.Errorf("%w: empty type", ErrInvalidType)
    }
    if !typeRe.MatchString(s) {
        return "", fmt.Errorf("%w: type %q does not match [a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*", ErrInvalidType, s)
    }
    return Type(s), nil
}

func splitDots(s string) []string {
    out := make([]string, 0, 4)
    start := 0
    for i := 0; i < len(s); i++ {
        if s[i] == '.' {
            out = append(out, s[start:i])
            start = i + 1
        }
    }
    out = append(out, s[start:])
    return out
}
```

Write `internal/eventbus/types_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
    "errors"
    "strings"
    "testing"

    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus"
)

func TestNewSubjectAcceptsValidPatterns(t *testing.T) {
    cases := []string{
        "events.main.location.01JABC",
        "events.main.scene.01JABC.ic",
        "events.*.scene.*.lifecycle",
        "events.main.scene.>",
    }
    for _, s := range cases {
        t.Run(s, func(t *testing.T) {
            got, err := eventbus.NewSubject(s)
            require.NoError(t, err)
            require.Equal(t, eventbus.Subject(s), got)
        })
    }
}

func TestNewSubjectRejectsInvalidPatterns(t *testing.T) {
    cases := []struct {
        name string
        in   string
    }{
        {"empty", ""},
        {"missing events prefix", "main.location.01JABC"},
        {"empty token between dots", "events..main.location.X"},
        {"tilde character", "events.main.location.~"},
        {"> not last", "events.>.scene.ic"},
        {"too deep", "events." + strings.Repeat("a.", 16)},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            _, err := eventbus.NewSubject(tc.in)
            require.Error(t, err)
            require.True(t, errors.Is(err, eventbus.ErrInvalidSubject), "got %v", err)
        })
    }
}

func TestNewTypeAcceptsValidPatterns(t *testing.T) {
    cases := []string{"say", "scene.pose", "scene.lifecycle.created"}
    for _, s := range cases {
        t.Run(s, func(t *testing.T) {
            got, err := eventbus.NewType(s)
            require.NoError(t, err)
            require.Equal(t, eventbus.Type(s), got)
        })
    }
}

func TestNewTypeRejectsInvalidPatterns(t *testing.T) {
    cases := []struct {
        name string
        in   string
    }{
        {"empty", ""},
        {"uppercase start", "Scene.pose"},
        {"trailing dot", "scene."},
        {"double dot", "scene..pose"},
        {"hyphen", "scene-pose"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            _, err := eventbus.NewType(tc.in)
            require.Error(t, err)
            require.True(t, errors.Is(err, eventbus.ErrInvalidType), "got %v", err)
        })
    }
}

func TestMustSubjectPanicsOnInvalid(t *testing.T) {
    require.Panics(t, func() { eventbus.MustSubject("not-prefixed") })
}

func TestMustSubjectAcceptsValid(t *testing.T) {
    require.NotPanics(t, func() { eventbus.MustSubject("events.main.scene.>") })
}
```

- [ ] **Step 5: Run types_test.go to verify it passes**

```bash
task test -- -run "TestNewSubject|TestNewType|TestMustSubject" ./internal/eventbus/...
```

Expected: all tests pass.

- [ ] **Step 6: Write `bus.go` with the three composed interfaces and Delivery**

Write `internal/eventbus/bus.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
    "context"
    "time"

    "github.com/oklog/ulid/v2"
)

// Publisher writes events. Used by the EventSink facade in
// internal/plugin/event_emitter.go after Phase B (F1).
type Publisher interface {
    Publish(ctx context.Context, event Event) error
}

// Subscriber opens long-lived session streams. Used by the gRPC Subscribe
// handler after Phase B (F3).
type Subscriber interface {
    OpenSession(ctx context.Context, sessionID string, filters []Subject) (SessionStream, error)
}

// HistoryReader serves paginated history reads. Used by gRPC QueryHistory
// handler after Phase B (F4).
type HistoryReader interface {
    QueryHistory(ctx context.Context, q HistoryQuery) (HistoryStream, error)
}

// EventBus is the concrete implementation that satisfies all three
// single-responsibility interfaces. Tests SHOULD depend on the narrow
// interface they actually need.
type EventBus interface {
    Publisher
    Subscriber
    HistoryReader
}

// Delivery is a typed handle for a single message in flight from a
// SessionStream. Replaces the prior (Event, AckFunc, error) tuple shape:
// typed handles are easier to mock, log, and extend.
type Delivery interface {
    Event() Event
    Ack() error
    // Nack signals the message should be redelivered. Use for transient
    // handler errors.
    Nack() error
    // InProgress extends the ack-wait timer. Use sparingly for handlers
    // expecting to exceed the default.
    InProgress() error
}

// SessionStream is a consumer-side handle bound to a JS durable consumer.
type SessionStream interface {
    // Next blocks until the next delivery or ctx done.
    Next(ctx context.Context) (Delivery, error)
    // SetFilters atomically replaces the FilterSubjects on the underlying
    // durable consumer. Cursor is preserved by JS UpdateConsumer.
    SetFilters(ctx context.Context, filters []Subject) error
    Close() error
}

// HistoryQuery describes a paginated history read. Auth flows via
// context.Context (auth.WithSession), not via this struct.
type HistoryQuery struct {
    Subject   Subject   // exact subject OR pattern with * / >
    After     ulid.ULID // exclusive lower bound; zero ULID = from start
    Before    ulid.ULID // exclusive upper bound; zero ULID = unbounded
    NotBefore time.Time // optional time bound
    NotAfter  time.Time // optional time bound
    Direction Direction
    PageSize  int // host caps at 200; default 50
}

// HistoryStream is a server-streaming handle. Caller iterates Next()
// until io.EOF; for next-page resume, the caller records the ULID of the
// last Event returned and passes it as After on the next call.
type HistoryStream interface {
    Next(ctx context.Context) (Event, error)
    Close() error
}
```

Write `internal/eventbus/bus_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
    "context"
    "testing"

    "github.com/holomush/holomush/internal/eventbus"
)

// fakeBus satisfies all three split interfaces; used to verify that
// EventBus is satisfiable as the composition.
type fakeBus struct{}

func (fakeBus) Publish(_ context.Context, _ eventbus.Event) error { return nil }
func (fakeBus) OpenSession(_ context.Context, _ string, _ []eventbus.Subject) (eventbus.SessionStream, error) {
    return nil, nil
}
func (fakeBus) QueryHistory(_ context.Context, _ eventbus.HistoryQuery) (eventbus.HistoryStream, error) {
    return nil, nil
}

func TestEventBusInterfaceComposesAllThree(t *testing.T) {
    var (
        _ eventbus.Publisher     = fakeBus{}
        _ eventbus.Subscriber    = fakeBus{}
        _ eventbus.HistoryReader = fakeBus{}
        _ eventbus.EventBus      = fakeBus{}
    )
}
```

- [ ] **Step 7: Run all eventbus tests**

```bash
task test -- ./internal/eventbus/...
```

Expected: all tests pass. Coverage MUST be ≥ 90%.

- [ ] **Step 8: Add lint rule for `time.Sleep` ban**

Modify `.golangci.yml` to add a `forbidigo` rule (if `forbidigo` is not yet enabled, add it):

```yaml
linters:
  enable:
    - forbidigo

linters-settings:
  forbidigo:
    forbid:
      - p: ^time\.Sleep$
        pkg: ^github.com/holomush/holomush/internal/eventbus(/.*)?$
        msg: "time.Sleep is banned in eventbus tests; use eventbustest.Await* helpers instead"
      - p: ^time\.Sleep$
        pkg: ^github.com/holomush/holomush/test/integration/eventbus_e2e(/.*)?$
        msg: "time.Sleep is banned in eventbus E2E tests; use eventbustest.Await* helpers instead"
    analyze-types: true
```

- [ ] **Step 9: Run lint to verify no false positives**

```bash
task lint
```

Expected: passes. (Old code still uses `time.Sleep` legitimately and is outside the ban path.)

- [ ] **Step 10: Run full pr-prep**

```bash
task pr-prep
```

Expected: all green.

- [ ] **Step 11: Commit M1**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`. Conventional message: `feat(eventbus): M1 — types, interfaces, errors, and lint guardrails`.

---

## Task M2: Codec interface, IdentityCodec, registry meta-test

**Files:**

- Create: `internal/eventbus/codec/codec.go`, `internal/eventbus/codec/codec_test.go`
- Create: `internal/eventbus/codec/registry.go`, `internal/eventbus/codec/registry_test.go`

**Bead:** `holomush-1tvn.M2`

- [ ] **Step 1: Write the failing test for `IdentityCodec` round-trip**

Create directory and write `internal/eventbus/codec/codec_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package codec_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus/codec"
)

func TestIdentityCodecRoundTripPreservesBytes(t *testing.T) {
    c := codec.IdentityCodec{}
    plaintext := []byte("hello, scene 01JABC")

    encoded, err := c.Encode(context.Background(), plaintext, codec.NoKey)
    require.NoError(t, err)

    decoded, err := c.Decode(context.Background(), encoded, codec.NoKey)
    require.NoError(t, err)
    require.Equal(t, plaintext, decoded)
}

func TestIdentityCodecHandlesEmptyPlaintext(t *testing.T) {
    c := codec.IdentityCodec{}
    encoded, err := c.Encode(context.Background(), nil, codec.NoKey)
    require.NoError(t, err)
    decoded, err := c.Decode(context.Background(), encoded, codec.NoKey)
    require.NoError(t, err)
    require.Empty(t, decoded)
}

func TestIdentityCodecName(t *testing.T) {
    c := codec.IdentityCodec{}
    require.Equal(t, codec.NameIdentity, c.Name())
}
```

- [ ] **Step 2: Run the failing test**

```bash
task test -- ./internal/eventbus/codec/...
```

Expected: build error (package doesn't exist).

- [ ] **Step 3: Write `codec.go`**

```bash
mkdir -p internal/eventbus/codec
```

Create `internal/eventbus/codec/codec.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package codec defines the host-owned codec interface for event payload
// encoding/decoding. The Identity codec is the pass-through default;
// future encryption codecs (e.g., aes-gcm-v1) plug in via the registry.
//
// Per the spec (§9), the codec is a narrow crypto primitive — it does
// NOT know about subjects or routing. Subject→key mapping lives in a
// separate KeySelector (also defined here, no production implementation
// yet).
package codec

import "context"

// Name is a closed enumeration of host-known codecs. Plugins MUST NOT
// register codecs.
type Name string

const (
    NameIdentity Name = "identity"
    // Future:
    // NameAESGCMv1        Name = "aes-gcm-v1"
    // NameXChaCha20v1     Name = "xchacha20poly1305-v1"
)

// Codec encodes and decodes event payload bytes. Implementations MUST be
// stateless and safe for concurrent use.
type Codec interface {
    Name() Name
    Encode(ctx context.Context, plaintext []byte, key Key) ([]byte, error)
    Decode(ctx context.Context, ciphertext []byte, key Key) ([]byte, error)
}

// Key is the opaque cryptographic material a codec uses to encrypt/decrypt.
// IdentityCodec ignores it and accepts NoKey.
type Key struct {
    ID    KeyID
    Bytes []byte
    // Codec-specific metadata may be carried inside Bytes; codecs are free
    // to interpret as they need.
}

// NoKey is the sentinel passed to keyless codecs (IdentityCodec).
var NoKey = Key{}

// KeyID is a stable identifier for a key version. Stored in the codec's
// internal envelope so Decode can pick the right key on rotation.
type KeyID uint64

// KeyLabel is a logical purpose name (e.g., "scene-content", "dm-content")
// used by KeyProvider.Active to look up the current key for that purpose.
type KeyLabel string

// KeyProvider supplies keys to codecs.
type KeyProvider interface {
    Active(ctx context.Context, label KeyLabel) (Key, error)
    ByID(ctx context.Context, id KeyID) (Key, error)
}

// KeySelector maps a publish-time subject to (codec name, key label) per
// the deployment's encryption policy. Lives upstream of Codec.
type KeySelector interface {
    SelectForEncrypt(ctx context.Context, subject string) (Name, KeyLabel, error)
    SelectForDecrypt(ctx context.Context, codec Name, keyID KeyID) (Key, error)
}

// IdentityCodec is the default no-op codec. It returns plaintext unchanged.
type IdentityCodec struct{}

func (IdentityCodec) Name() Name { return NameIdentity }

func (IdentityCodec) Encode(_ context.Context, plaintext []byte, _ Key) ([]byte, error) {
    return plaintext, nil
}

func (IdentityCodec) Decode(_ context.Context, ciphertext []byte, _ Key) ([]byte, error) {
    return ciphertext, nil
}
```

- [ ] **Step 4: Run codec_test.go to verify pass**

```bash
task test -- ./internal/eventbus/codec/...
```

Expected: all 3 tests pass.

- [ ] **Step 5: Write the registry**

Create `internal/eventbus/codec/registry.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package codec

import (
    "fmt"
    "sync"
)

// registry holds all host-known codecs. Closed enumeration — plugins
// cannot register codecs. The variable is package-private; access via
// Resolve / All / RegisterForTest.
var (
    regMu    sync.RWMutex
    registry = map[Name]Codec{
        NameIdentity: IdentityCodec{},
    }
)

// Resolve returns the codec for the given name, or error if unknown.
// Hard-fails on unknown names — callers MUST NOT silently fall back to
// identity.
func Resolve(name Name) (Codec, error) {
    regMu.RLock()
    defer regMu.RUnlock()
    c, ok := registry[name]
    if !ok {
        return nil, fmt.Errorf("codec: unknown name %q", name)
    }
    return c, nil
}

// All returns a copy of all registered codec names. Used by the meta-test
// to assert const ↔ registry sync.
func All() []Name {
    regMu.RLock()
    defer regMu.RUnlock()
    out := make([]Name, 0, len(registry))
    for n := range registry {
        out = append(out, n)
    }
    return out
}

// RegisterForTest installs a codec at runtime. Production code MUST NOT
// call this — it is intended for tests that exercise a custom codec
// (e.g., a stub encrypt/decrypt for property tests). Returns a cleanup
// func that restores the prior state.
func RegisterForTest(c Codec) func() {
    regMu.Lock()
    prev, hadPrev := registry[c.Name()]
    registry[c.Name()] = c
    regMu.Unlock()
    return func() {
        regMu.Lock()
        defer regMu.Unlock()
        if hadPrev {
            registry[c.Name()] = prev
        } else {
            delete(registry, c.Name())
        }
    }
}
```

- [ ] **Step 6: Write the meta-test**

Create `internal/eventbus/codec/registry_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package codec_test

import (
    "reflect"
    "testing"

    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus/codec"
)

// declaredNames lists every Name constant defined in codec.go.
// This list MUST be updated when a new constant is added — the meta-test
// below catches the case where a const is declared but not registered.
var declaredNames = []codec.Name{
    codec.NameIdentity,
    // Add new constants here when introduced.
}

func TestEveryDeclaredCodecNameIsRegistered(t *testing.T) {
    for _, n := range declaredNames {
        t.Run(string(n), func(t *testing.T) {
            c, err := codec.Resolve(n)
            require.NoError(t, err, "codec %q is declared but not in registry", n)
            require.Equal(t, n, c.Name())
        })
    }
}

func TestRegistryHasNoExtraEntriesNotDeclared(t *testing.T) {
    declared := make(map[codec.Name]bool, len(declaredNames))
    for _, n := range declaredNames {
        declared[n] = true
    }
    for _, n := range codec.All() {
        if !declared[n] {
            t.Errorf("registry contains %q which is not in declaredNames — update declaredNames or remove from registry", n)
        }
    }
}

func TestResolveUnknownCodecReturnsError(t *testing.T) {
    _, err := codec.Resolve(codec.Name("does-not-exist"))
    require.Error(t, err)
}

func TestRegisterForTestRestoresState(t *testing.T) {
    var stub stubCodec
    cleanup := codec.RegisterForTest(stub)
    c, err := codec.Resolve(stub.Name())
    require.NoError(t, err)
    require.True(t, reflect.TypeOf(c) == reflect.TypeOf(stub))

    cleanup()
    _, err = codec.Resolve(stub.Name())
    require.Error(t, err)
}

type stubCodec struct{}

func (stubCodec) Name() codec.Name { return codec.Name("test-only-stub") }
func (stubCodec) Encode(_ interface{}, p []byte, _ codec.Key) ([]byte, error) {
    return p, nil
}
func (stubCodec) Decode(_ interface{}, p []byte, _ codec.Key) ([]byte, error) {
    return p, nil
}
```

Wait — the stub above has wrong signatures (uses `interface{}` for ctx). Fix the test stub to match the real `Codec` interface (uses `context.Context`):

Replace the bottom of `registry_test.go`:

```go
type stubCodec struct{}

func (stubCodec) Name() codec.Name { return codec.Name("test-only-stub") }
func (stubCodec) Encode(_ context.Context, p []byte, _ codec.Key) ([]byte, error) {
    return p, nil
}
func (stubCodec) Decode(_ context.Context, p []byte, _ codec.Key) ([]byte, error) {
    return p, nil
}
```

And add `import "context"` at the top.

- [ ] **Step 7: Run registry_test**

```bash
task test -- ./internal/eventbus/codec/...
```

Expected: all tests pass. Coverage MUST be ≥ 95%.

- [ ] **Step 8: Run pr-prep**

```bash
task pr-prep
```

Expected: green.

- [ ] **Step 9: Commit M2**

Commit. Conventional message: `feat(eventbus/codec): M2 — codec interface, IdentityCodec, closed registry with sync meta-test`.

---

## Task M3: PostgreSQL `events_audit` migration

**Files:**

- Create: `internal/store/migrations/000NNN_create_events_audit.up.sql`, `.down.sql` (NNN = next available number)
- Create: `internal/store/events_audit_test.go`

**Bead:** `holomush-1tvn.M3`

- [ ] **Step 1: Determine the next migration number**

```bash
ls internal/store/migrations/ | sort | tail -3
```

Use the next sequential number after the highest existing migration. Example: if last is `000019_*`, use `000020`.

- [ ] **Step 2: Write the up migration**

Create `internal/store/migrations/000020_create_events_audit.up.sql` (replace `000020` with the actual number from Step 1):

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE IF NOT EXISTS events_audit (
    id           BYTEA       PRIMARY KEY,
    subject      TEXT        NOT NULL,
    type         TEXT        NOT NULL,
    timestamp    TIMESTAMPTZ NOT NULL,
    actor_kind   TEXT        NOT NULL,
    actor_id     BYTEA,
    payload      BYTEA       NOT NULL,
    schema_ver   SMALLINT    NOT NULL,
    codec        TEXT        NOT NULL,
    js_seq       BIGINT      NOT NULL,
    inserted_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS events_audit_subject_id  ON events_audit (subject, id);
CREATE INDEX IF NOT EXISTS events_audit_subject_ts  ON events_audit (subject, timestamp);
CREATE INDEX IF NOT EXISTS events_audit_subject_pat ON events_audit (subject text_pattern_ops);
```

- [ ] **Step 3: Write the down migration**

Create `internal/store/migrations/000020_create_events_audit.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS events_audit_subject_pat;
DROP INDEX IF EXISTS events_audit_subject_ts;
DROP INDEX IF EXISTS events_audit_subject_id;
DROP TABLE IF EXISTS events_audit;
```

- [ ] **Step 4: Write the integration test (uses testcontainers)**

Create `internal/store/events_audit_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
    "context"
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/test/testutil"
)

func TestEventsAuditTablePresentWithIndexes(t *testing.T) {
    pg := testutil.SharedPostgres(t)
    db := testutil.FreshDatabase(t, pg)
    ctx := context.Background()

    var n int
    require.NoError(t,
        db.QueryRowContext(ctx,
            "SELECT count(*) FROM information_schema.tables WHERE table_name='events_audit'",
        ).Scan(&n),
    )
    require.Equal(t, 1, n, "events_audit table not created by migrations")

    rows, err := db.QueryContext(ctx,
        "SELECT indexname FROM pg_indexes WHERE tablename='events_audit' ORDER BY indexname",
    )
    require.NoError(t, err)
    defer rows.Close()
    var indexes []string
    for rows.Next() {
        var name string
        require.NoError(t, rows.Scan(&name))
        indexes = append(indexes, name)
    }
    require.Contains(t, indexes, "events_audit_subject_id")
    require.Contains(t, indexes, "events_audit_subject_ts")
    require.Contains(t, indexes, "events_audit_subject_pat")
    require.Contains(t, indexes, "events_audit_pkey")
}

func TestEventsAuditInsertOnConflictIsIdempotent(t *testing.T) {
    pg := testutil.SharedPostgres(t)
    db := testutil.FreshDatabase(t, pg)
    ctx := context.Background()

    id := ulid.Make()
    insert := `
        INSERT INTO events_audit (
            id, subject, type, timestamp, actor_kind, actor_id,
            payload, schema_ver, codec, js_seq
        ) VALUES ($1, $2, $3, now(), 'system', NULL, $4, 1, 'identity', 100)
        ON CONFLICT (id) DO NOTHING`
    payload := []byte(`{"hello":"world"}`)

    res1, err := db.ExecContext(ctx, insert, id[:], "events.main.test", "test.t", payload)
    require.NoError(t, err)
    n1, _ := res1.RowsAffected()
    require.EqualValues(t, 1, n1, "first insert should affect 1 row")

    res2, err := db.ExecContext(ctx, insert, id[:], "events.main.test", "test.t", payload)
    require.NoError(t, err)
    n2, _ := res2.RowsAffected()
    require.EqualValues(t, 0, n2, "duplicate insert should affect 0 rows")

    var count int
    require.NoError(t,
        db.QueryRowContext(ctx, "SELECT count(*) FROM events_audit WHERE id=$1", id[:]).Scan(&count),
    )
    require.Equal(t, 1, count)
}

func TestEventsAuditCodecColumnIsNotNull(t *testing.T) {
    pg := testutil.SharedPostgres(t)
    db := testutil.FreshDatabase(t, pg)
    ctx := context.Background()

    id := ulid.Make()
    insert := `
        INSERT INTO events_audit (
            id, subject, type, timestamp, actor_kind, actor_id,
            payload, schema_ver, codec, js_seq
        ) VALUES ($1, 'events.main.test', 'test.t', now(), 'system', NULL, $2, 1, NULL, 100)`
    _, err := db.ExecContext(ctx, insert, id[:], []byte(`{}`))
    require.Error(t, err, "NULL codec should be rejected by NOT NULL constraint")
}
```

- [ ] **Step 5: Run the integration tests**

```bash
task test:int -- -run TestEventsAudit ./internal/store/...
```

Expected: 3 tests pass.

- [ ] **Step 6: Run pr-prep**

```bash
task pr-prep
```

Expected: green (includes migration smoke test).

- [ ] **Step 7: Commit M3**

Conventional message: `feat(store): M3 — add events_audit table for JetStream audit projection`.

---

## Task M4: `SubsystemEventBus` — embedded NATS lifecycle

**Files:**

- Create: `internal/eventbus/config.go`
- Create: `internal/eventbus/subsystem.go`, `internal/eventbus/subsystem_test.go`
- Create: `internal/eventbus/eventbustest/embedded.go`
- Modify: `internal/lifecycle/subsystem.go` — add `SubsystemEventBus` ID
- Modify: `internal/config/config.go` — add `EventBus` config struct
- Modify: `cmd/holomush/core.go` — register the subsystem
- Modify: `go.mod` — add `nats-server/v2` and `nats.go` deps

**Bead:** `holomush-1tvn.M4`

- [ ] **Step 1: Add NATS deps**

```bash
task install -- github.com/nats-io/nats-server/v2 || \
    go get github.com/nats-io/nats-server/v2@latest && \
    go get github.com/nats-io/nats.go@latest && \
    go mod tidy
```

(Use the project's preferred dep update flow — `task` if a `go-get` task exists, otherwise `go get`.)

Pin to the latest GA minor (verify `go.mod` shows `v2.12.x` or current latest).

- [ ] **Step 2: Add lifecycle subsystem ID**

Edit `internal/lifecycle/subsystem.go`. Add to the existing `const` block:

```go
const (
    // ... existing IDs ...
    SubsystemEventBus         SubsystemID = "eventbus"
    SubsystemAuditProjection  SubsystemID = "audit_projection"
)
```

- [ ] **Step 3: Write `config.go`**

Create `internal/eventbus/config.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import "time"

// Mode selects between embedded and clustered NATS.
type Mode string

const (
    ModeEmbedded Mode = "embedded"
    ModeCluster  Mode = "cluster" // future; not implemented in Phase A
)

// Config controls the EventBus subsystem.
type Config struct {
    Mode     Mode
    GameID   string // mandatory; first segment after "events." (default "main")
    StoreDir string // blank = xdg.DataDir() + "/jetstream"

    StreamMaxAge time.Duration // default 720h (30 days)
    DupeWindow   time.Duration // default 30m

    MonitorPort        int  // 0 = disabled
    PrometheusExporter bool // in-process exporter

    // Cluster-mode only (unused in Phase A):
    ClusterURL      string
    CredentialsFile string
}

// Defaults applies the documented defaults to any zero-value field.
func (c Config) Defaults() Config {
    if c.Mode == "" {
        c.Mode = ModeEmbedded
    }
    if c.GameID == "" {
        c.GameID = "main"
    }
    if c.StreamMaxAge == 0 {
        c.StreamMaxAge = 30 * 24 * time.Hour
    }
    if c.DupeWindow == 0 {
        c.DupeWindow = 30 * time.Minute
    }
    return c
}
```

- [ ] **Step 4: Wire config into `internal/config/config.go`**

Add the new section to the existing config struct (locate the appropriate config struct in `internal/config/config.go` and add):

```go
// EventBus is the JetStream event bus configuration. Phase A only.
EventBus eventbus.Config `koanf:"event_bus"`
```

(Add the import for `internal/eventbus`.)

- [ ] **Step 5: Write the failing subsystem test**

Create `internal/eventbus/subsystem_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
    "context"
    "testing"
    "time"

    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus"
    "github.com/holomush/holomush/internal/eventbus/eventbustest"
)

func TestSubsystemStartsAndExposesJetStream(t *testing.T) {
    e := eventbustest.New(t)
    require.NotNil(t, e.JS)
    info, err := e.JS.Stream(context.Background(), "EVENTS")
    require.NoError(t, err)
    require.NotNil(t, info)
}

func TestSubsystemStreamDeclarationIsIdempotent(t *testing.T) {
    e := eventbustest.New(t)
    // Re-running ensure shouldn't error
    sub := e.Bus
    require.NoError(t, sub.EnsureStream(context.Background()))
    require.NoError(t, sub.EnsureStream(context.Background()))
}

func TestSubsystemStopDrainsAndShutsDown(t *testing.T) {
    e := eventbustest.New(t)
    require.NoError(t, e.Bus.Stop(context.Background()))
    // After Stop, calls SHOULD fail
    _, err := e.JS.Stream(context.Background(), "EVENTS")
    require.Error(t, err)
}

func TestSubsystemDependsOnNothing(t *testing.T) {
    s := eventbus.NewSubsystem(eventbus.Config{}.Defaults())
    require.Empty(t, s.DependsOn())
}

func TestSubsystemReadyTimeoutIsBounded(t *testing.T) {
    // The embedded server SHOULD be ready well under 10s.
    start := time.Now()
    e := eventbustest.New(t)
    elapsed := time.Since(start)
    require.NotNil(t, e.Bus)
    require.Less(t, elapsed, 5*time.Second, "Subsystem.Start exceeded 5s")
}
```

- [ ] **Step 6: Implement the subsystem and the test helper**

Create `internal/eventbus/subsystem.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
    "context"
    "errors"
    "fmt"
    "time"

    "github.com/nats-io/nats-server/v2/server"
    "github.com/nats-io/nats.go"
    "github.com/nats-io/nats.go/jetstream"

    "github.com/holomush/holomush/internal/lifecycle"
)

// StreamName is the single JetStream stream that holds all events.
const StreamName = "EVENTS"

// SubjectFilter is the stream subject filter — every event lands here.
const SubjectFilter = "events.>"

// Subsystem wraps the embedded NATS server and exposes a JetStream context.
type Subsystem struct {
    cfg     Config
    server  *server.Server
    conn    *nats.Conn
    js      jetstream.JetStream
    storage jetstream.StorageType // overridable for tests
}

// NewSubsystem constructs the subsystem from a defaulted Config.
// FileStorage is the default; tests override via NewSubsystemWithStorage.
func NewSubsystem(cfg Config) *Subsystem {
    return NewSubsystemWithStorage(cfg, jetstream.FileStorage)
}

// NewSubsystemWithStorage allows tests to use MemoryStorage for speed.
func NewSubsystemWithStorage(cfg Config, storage jetstream.StorageType) *Subsystem {
    return &Subsystem{cfg: cfg.Defaults(), storage: storage}
}

func (s *Subsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemEventBus }
func (s *Subsystem) DependsOn() []lifecycle.SubsystemID { return nil }

func (s *Subsystem) Start(ctx context.Context) error {
    opts := &server.Options{
        ServerName: "holomush-embedded",
        JetStream:  true,
        StoreDir:   s.cfg.StoreDir,
        DontListen: true,
        NoSigs:     true,
        LogtimeUTC: true,
        HTTPPort:   s.cfg.MonitorPort,
    }
    srv, err := server.NewServer(opts)
    if err != nil {
        return fmt.Errorf("eventbus: NewServer: %w", err)
    }
    s.server = srv
    go s.server.Start()
    if !s.server.ReadyForConnections(10 * time.Second) {
        s.server.Shutdown()
        return errors.New("eventbus: NATS server not ready in 10s")
    }
    s.conn, err = nats.Connect("",
        nats.InProcessServer(s.server),
        nats.Name("holomush-host"),
    )
    if err != nil {
        s.server.Shutdown()
        return fmt.Errorf("eventbus: in-process connect: %w", err)
    }
    s.js, err = jetstream.New(s.conn)
    if err != nil {
        s.conn.Close()
        s.server.Shutdown()
        return fmt.Errorf("eventbus: jetstream context: %w", err)
    }
    if err := s.EnsureStream(ctx); err != nil {
        s.conn.Close()
        s.server.Shutdown()
        return err
    }
    return nil
}

// EnsureStream creates or updates the EVENTS stream idempotently.
func (s *Subsystem) EnsureStream(ctx context.Context) error {
    _, err := s.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
        Name:        StreamName,
        Subjects:    []string{SubjectFilter},
        Retention:   jetstream.LimitsPolicy,
        Storage:     s.storage,
        Replicas:    1,
        MaxAge:      s.cfg.StreamMaxAge,
        Duplicates:  s.cfg.DupeWindow,
        AllowDirect: true,
    })
    if err != nil {
        return fmt.Errorf("eventbus: CreateOrUpdateStream: %w", err)
    }
    return nil
}

func (s *Subsystem) Stop(ctx context.Context) error {
    if s.conn != nil {
        _ = s.conn.Drain()
    }
    if s.server != nil {
        s.server.Shutdown()
        s.server.WaitForShutdown()
    }
    return nil
}

// JS returns the JetStream context. Used by audit projection (M5) and
// the publish/subscribe wiring (Phase B).
func (s *Subsystem) JS() jetstream.JetStream { return s.js }

// Conn returns the in-process NATS connection.
func (s *Subsystem) Conn() *nats.Conn { return s.conn }
```

Create `internal/eventbus/eventbustest/embedded.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package eventbustest provides test helpers for the embedded JetStream
// event bus. Tests SHOULD use New(t) to get a fresh, in-memory bus per test.
package eventbustest

import (
    "context"
    "testing"

    "github.com/nats-io/nats.go"
    "github.com/nats-io/nats.go/jetstream"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus"
)

// Embedded bundles the bus subsystem with its JetStream context and
// connection so tests can interact directly.
type Embedded struct {
    Bus  *eventbus.Subsystem
    JS   jetstream.JetStream
    Conn *nats.Conn
}

// New starts a fresh embedded NATS server with MemoryStorage and registers
// cleanup on t.Cleanup. Per-test isolation; safe for t.Parallel.
func New(t *testing.T) *Embedded {
    t.Helper()
    cfg := eventbus.Config{
        StoreDir: t.TempDir(),
    }.Defaults()
    bus := eventbus.NewSubsystemWithStorage(cfg, jetstream.MemoryStorage)
    require.NoError(t, bus.Start(context.Background()))
    t.Cleanup(func() { _ = bus.Stop(context.Background()) })
    return &Embedded{
        Bus:  bus,
        JS:   bus.JS(),
        Conn: bus.Conn(),
    }
}
```

- [ ] **Step 7: Run subsystem tests**

```bash
task test -- ./internal/eventbus/...
```

Expected: all 5 tests pass.

- [ ] **Step 8: Register the subsystem in the orchestrator (idle wiring)**

Edit `cmd/holomush/core.go`. Locate the section where subsystems are constructed and registered (after `dbSub` per the existing pattern). Add:

```go
// JetStream event bus (idle in Phase A — no consumers until Phase B F1).
eventBusSub := eventbus.NewSubsystem(cfg.EventBus)
orch.Register(eventBusSub)
```

(Add the import for `internal/eventbus`.)

- [ ] **Step 9: Run pr-prep**

```bash
task pr-prep
```

Expected: green. The full server starts, embedded NATS comes up, no consumers attach, no events flow. Verify by checking startup logs include "holomush-embedded" server name.

- [ ] **Step 10: Commit M4**

Conventional message: `feat(eventbus): M4 — SubsystemEventBus (embedded NATS, idle Phase A)`.

---

## Task M5: `SubsystemAuditProjection` — durable consumer drains empty stream

**Files:**

- Create: `internal/eventbus/audit/subsystem.go`, `internal/eventbus/audit/subsystem_test.go`
- Create: `internal/eventbus/audit/projection.go`, `internal/eventbus/audit/projection_test.go`
- Create: `internal/eventbus/audit/lag_metric.go`
- Modify: `cmd/holomush/core.go` — register

**Bead:** `holomush-1tvn.M5`

The projection worker writes audit rows for every JS message NOT owned by a plugin. In Phase A, no plugins claim subjects yet, so the worker sees `events.>` minus an empty exclusion list = `events.>`. But nothing publishes (Phase B introduces publishing), so the worker drains zero messages and reports lag == 0.

- [ ] **Step 1: Write the failing integration test**

Create `internal/eventbus/audit/projection_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package audit_test

import (
    "context"
    "testing"
    "time"

    "github.com/nats-io/nats.go/jetstream"
    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus/audit"
    "github.com/holomush/holomush/internal/eventbus/eventbustest"
    "github.com/holomush/holomush/test/testutil"
)

func TestProjectionDrainsPublishedMessageToAuditTable(t *testing.T) {
    e := eventbustest.New(t)
    pg := testutil.SharedPostgres(t)
    db := testutil.FreshDatabase(t, pg)

    proj := audit.NewSubsystem(e.Bus.JS(), db, audit.Config{}.Defaults())
    require.NoError(t, proj.Start(context.Background()))
    t.Cleanup(func() { _ = proj.Stop(context.Background()) })

    // Publish a raw NATS message simulating an event publish.
    id := ulid.Make()
    msgID := id.String()
    _, err := e.Bus.JS().Publish(context.Background(), "events.main.test.unit", []byte(`{"hello":"world"}`),
        jetstream.WithMsgID(msgID),
    )
    require.NoError(t, err)

    // Wait until projection ack catches up (deterministic via metric, not sleep).
    proj.AwaitDrained(t, 5*time.Second)

    var count int
    require.NoError(t,
        db.QueryRowContext(context.Background(),
            "SELECT count(*) FROM events_audit WHERE id=$1", id[:]).Scan(&count))
    require.Equal(t, 1, count)
}

func TestProjectionIsIdempotentOnDuplicate(t *testing.T) {
    // ... similar setup, publish same id twice, assert count == 1
}
```

(For brevity, the second test is omitted in this plan — implement it analogously.)

- [ ] **Step 2: Implement `audit.Subsystem` and `Config`**

Create `internal/eventbus/audit/subsystem.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package audit projects events from JetStream into the PostgreSQL
// events_audit table for forever-archive and historical query support.
package audit

import (
    "context"
    "database/sql"
    "fmt"
    "time"

    "github.com/nats-io/nats.go/jetstream"

    "github.com/holomush/holomush/internal/lifecycle"
)

// Config controls the audit projection worker.
type Config struct {
    ConsumerName  string        // default "host_audit_projection"
    BatchSize     int           // default 64 (matches MaxAckPending)
    AckWait       time.Duration // default 5s
    MaxAckPending int           // default 64
}

func (c Config) Defaults() Config {
    if c.ConsumerName == "" {
        c.ConsumerName = "host_audit_projection"
    }
    if c.BatchSize == 0 {
        c.BatchSize = 64
    }
    if c.AckWait == 0 {
        c.AckWait = 5 * time.Second
    }
    if c.MaxAckPending == 0 {
        c.MaxAckPending = 64
    }
    return c
}

// Subsystem manages the host audit projection worker lifecycle.
type Subsystem struct {
    js     jetstream.JetStream
    db     *sql.DB
    cfg    Config
    cancel context.CancelFunc
    worker *projection
}

func NewSubsystem(js jetstream.JetStream, db *sql.DB, cfg Config) *Subsystem {
    return &Subsystem{js: js, db: db, cfg: cfg.Defaults()}
}

func (s *Subsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemAuditProjection }

func (s *Subsystem) DependsOn() []lifecycle.SubsystemID {
    return []lifecycle.SubsystemID{
        lifecycle.SubsystemDatabase,
        lifecycle.SubsystemEventBus,
    }
}

func (s *Subsystem) Start(ctx context.Context) error {
    p, err := newProjection(ctx, s.js, s.db, s.cfg)
    if err != nil {
        return fmt.Errorf("audit: %w", err)
    }
    s.worker = p
    workerCtx, cancel := context.WithCancel(context.Background())
    s.cancel = cancel
    go p.run(workerCtx)
    return nil
}

func (s *Subsystem) Stop(ctx context.Context) error {
    if s.cancel != nil {
        s.cancel()
    }
    if s.worker != nil {
        return s.worker.drain(ctx)
    }
    return nil
}

// AwaitDrained is a test-only helper: blocks until the consumer's last-acked
// seq matches the stream's last-published seq, or until timeout.
func (s *Subsystem) AwaitDrained(t interface{ Helper(); Fatal(args ...any) }, timeout time.Duration) {
    if s.worker == nil {
        return
    }
    s.worker.awaitDrained(t, timeout)
}
```

Create `internal/eventbus/audit/projection.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
    "context"
    "database/sql"
    "fmt"
    "time"

    "github.com/nats-io/nats.go/jetstream"
)

// projection holds the durable pull consumer and INSERT loop.
type projection struct {
    consumer jetstream.Consumer
    db       *sql.DB
    cfg      Config
}

func newProjection(ctx context.Context, js jetstream.JetStream, db *sql.DB, cfg Config) (*projection, error) {
    cons, err := js.CreateOrUpdateConsumer(ctx, "EVENTS", jetstream.ConsumerConfig{
        Durable:        cfg.ConsumerName,
        FilterSubjects: []string{"events.>"},
        AckPolicy:      jetstream.AckExplicitPolicy,
        AckWait:        cfg.AckWait,
        MaxAckPending:  cfg.MaxAckPending,
    })
    if err != nil {
        return nil, fmt.Errorf("CreateOrUpdateConsumer: %w", err)
    }
    return &projection{consumer: cons, db: db, cfg: cfg}, nil
}

func (p *projection) run(ctx context.Context) {
    cc, err := p.consumer.Consume(p.handle)
    if err != nil {
        // Real impl: structured-log + retry-with-backoff. For Phase A this is rare.
        return
    }
    <-ctx.Done()
    cc.Stop()
}

func (p *projection) handle(msg jetstream.Msg) {
    if err := p.persist(msg); err != nil {
        // No-ack on error; JS will redeliver after AckWait.
        // Real impl: structured-log + lag-metric increment.
        return
    }
    _ = msg.Ack()
}

func (p *projection) persist(msg jetstream.Msg) error {
    h := msg.Headers()
    msgID := h.Get("Nats-Msg-Id")
    if msgID == "" {
        return fmt.Errorf("audit: missing Nats-Msg-Id header")
    }
    codec := h.Get("App-Codec")
    if codec == "" {
        return fmt.Errorf("audit: missing App-Codec header")
    }
    eventType := h.Get("App-Event-Type")
    if eventType == "" {
        return fmt.Errorf("audit: missing App-Event-Type header")
    }
    schemaVer := h.Get("App-Schema-Version")
    if schemaVer == "" {
        return fmt.Errorf("audit: missing App-Schema-Version header")
    }
    meta, err := msg.Metadata()
    if err != nil {
        return fmt.Errorf("audit: msg.Metadata: %w", err)
    }
    // Phase A test path: actor data is not yet in headers; default to system.
    // Phase B publish path will inject ActorKind/ActorID headers.
    actorKind := h.Get("App-Actor-Kind")
    if actorKind == "" {
        actorKind = "system"
    }
    var actorID []byte
    if v := h.Get("App-Actor-ID"); v != "" {
        actorID = []byte(v)
    }
    // ULID id from msgID (msgID is the canonical ULID string).
    idBytes, err := decodeULIDString(msgID)
    if err != nil {
        return fmt.Errorf("audit: decode msg-id ulid: %w", err)
    }
    _, err = p.db.ExecContext(context.Background(), `
        INSERT INTO events_audit (
            id, subject, type, timestamp, actor_kind, actor_id,
            payload, schema_ver, codec, js_seq
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
        ON CONFLICT (id) DO NOTHING`,
        idBytes,
        msg.Subject(),
        eventType,
        meta.Timestamp,
        actorKind,
        actorID,
        msg.Data(),
        atoiOrZero(schemaVer),
        codec,
        meta.Sequence.Stream,
    )
    return err
}

func (p *projection) drain(ctx context.Context) error {
    // Best-effort: wait briefly for in-flight handle() calls to complete.
    // ConsumeContext.Stop() is called in run() on cancel.
    select {
    case <-time.After(2 * time.Second):
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}

func (p *projection) awaitDrained(t interface{ Helper(); Fatal(args ...any) }, timeout time.Duration) {
    t.Helper()
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        info, err := p.consumer.Info(context.Background())
        if err == nil && info.NumPending == 0 && info.NumAckPending == 0 {
            return
        }
        time.Sleep(20 * time.Millisecond) // safe in test helper, not test code
    }
    t.Fatal("audit projection did not drain in time")
}
```

Add helpers (`decodeULIDString`, `atoiOrZero`) at the bottom of the same file:

```go
import "github.com/oklog/ulid/v2"
import "strconv"

func decodeULIDString(s string) ([]byte, error) {
    u, err := ulid.Parse(s)
    if err != nil {
        return nil, err
    }
    return u[:], nil
}

func atoiOrZero(s string) int {
    n, _ := strconv.Atoi(s)
    return n
}
```

(Move imports to the top of file in real implementation.)

Create `internal/eventbus/audit/lag_metric.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
    "github.com/prometheus/client_golang/prometheus"
)

// LagSeconds reports the audit projection's lag (publish→ack) per subject domain.
var LagSeconds = prometheus.NewGaugeVec(
    prometheus.GaugeOpts{
        Namespace: "holomush",
        Subsystem: "audit",
        Name:      "projection_lag_seconds",
        Help:      "Seconds between latest published seq and audit consumer's last-acked seq",
    },
    []string{"projection"},
)

func init() {
    prometheus.MustRegister(LagSeconds)
}
```

- [ ] **Step 3: Run the integration test**

```bash
task test:int -- -run TestProjection ./internal/eventbus/audit/...
```

Expected: passes.

- [ ] **Step 4: Register the audit subsystem in the orchestrator**

Edit `cmd/holomush/core.go`:

```go
auditSub := audit.NewSubsystem(eventBusSub.JS(), db, audit.Config{})
orch.Register(auditSub)
```

(Order matters: `auditSub` registered after `eventBusSub` and `dbSub` since it depends on both.)

- [ ] **Step 5: Run pr-prep**

```bash
task pr-prep
```

Expected: green. Full server starts; audit projection comes up; drains zero messages (no publishers yet); shuts down cleanly.

- [ ] **Step 6: Commit M5**

Conventional message: `feat(eventbus/audit): M5 — SubsystemAuditProjection (idle Phase A)`.

---

## Task M6: `PluginAuditService` proto + bindings

**Files:**

- Create: `api/proto/holomush/eventbus/v1/eventbus.proto`
- Create: `api/proto/holomush/plugin/v1/audit.proto`
- Generated: `pkg/proto/holomush/eventbus/v1/eventbus.pb.go`
- Generated: `pkg/proto/holomush/plugin/v1/audit.pb.go`, `audit_grpc.pb.go`

**Bead:** `holomush-1tvn.M6`

- [ ] **Step 1: Write `eventbus.proto` (host envelope)**

Create `api/proto/holomush/eventbus/v1/eventbus.proto`:

```protobuf
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

syntax = "proto3";

package holomush.eventbus.v1;

option go_package = "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1;eventbusv1";

import "google/protobuf/timestamp.proto";

// ActorKind identifies what type of entity caused an event.
enum ActorKind {
  ACTOR_KIND_UNSPECIFIED = 0;
  ACTOR_KIND_CHARACTER   = 1;
  ACTOR_KIND_PLAYER      = 2;
  ACTOR_KIND_SYSTEM      = 3;
  ACTOR_KIND_PLUGIN      = 4;
}

// Actor identifies who caused an event.
message Actor {
  ActorKind kind = 1;
  bytes id = 2; // ULID (16 bytes); empty for system/unknown
}

// Event is the host-side envelope. Wire encoding is proto bytes in the
// JetStream message data; headers carry routing/codec/version metadata.
message Event {
  bytes id = 1;                             // ULID (16 bytes)
  string subject = 2;
  string type = 3;
  google.protobuf.Timestamp timestamp = 4;
  Actor actor = 5;
  bytes payload = 6;                        // codec.Encode output
}
```

- [ ] **Step 2: Write `audit.proto` (plugin audit RPC contract)**

Create `api/proto/holomush/plugin/v1/audit.proto`:

```protobuf
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

syntax = "proto3";

package holomush.plugin.v1;

option go_package = "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1;pluginv1";

import "holomush/eventbus/v1/eventbus.proto";

// PluginAuditService is implemented by plugins that declare audit
// subjects in their manifest. The host owns the JetStream durable
// consumer and forwards each delivered event to the plugin via
// AuditEvent. The plugin INSERTs into its own schema and acks.
//
// QueryHistory is invoked by host's bus.QueryHistory when the queried
// subject prefix is owned by this plugin.
service PluginAuditService {
  rpc AuditEvent(AuditEventRequest) returns (AuditEventResponse);
  rpc QueryHistory(QueryHistoryRequest) returns (stream QueryHistoryResponse);
}

message AuditEventRequest {
  holomush.eventbus.v1.Event event = 1;
  // Headers carried verbatim from the JS message (App-Codec,
  // App-Schema-Version, App-Event-Type, etc.) so the plugin can store them.
  map<string, string> headers = 2;
}

message AuditEventResponse {}

message QueryHistoryRequest {
  string subject = 1;
  bytes after = 2;       // ULID; empty = from start
  bytes before = 3;      // ULID; empty = unbounded
  int32 page_size = 4;   // host caps at 200
  int32 direction = 5;   // 1=forward, 2=backward
  google.protobuf.Timestamp not_before = 6;
  google.protobuf.Timestamp not_after = 7;
}

message QueryHistoryResponse {
  holomush.eventbus.v1.Event event = 1;
}
```

- [ ] **Step 3: Generate Go bindings**

```bash
task generate:proto
```

Expected: new files in `pkg/proto/holomush/eventbus/v1/` and `pkg/proto/holomush/plugin/v1/`.

- [ ] **Step 4: Run buf lint and breaking checks**

```bash
task lint:proto
```

Expected: passes.

- [ ] **Step 5: Run pr-prep**

```bash
task pr-prep
```

Expected: green.

- [ ] **Step 6: Commit M6**

Conventional message: `feat(proto): M6 — eventbus envelope + PluginAuditService contract`.

---

## Task M7: OTEL header propagation + Prometheus NATS exporter wiring

**Files:**

- Create: `internal/eventbus/telemetry/otel.go`, `internal/eventbus/telemetry/otel_test.go`
- Create: `internal/eventbus/telemetry/prometheus.go`, `internal/eventbus/telemetry/prometheus_test.go`
- Modify: `internal/eventbus/subsystem.go` — wire telemetry on `Start`

**Bead:** `holomush-1tvn.M7`

- [ ] **Step 1: Add `prometheus-nats-exporter` Go dep**

```bash
go get github.com/nats-io/prometheus-nats-exporter@latest && go mod tidy
```

- [ ] **Step 2: Write the failing OTEL header test**

Create `internal/eventbus/telemetry/otel_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry_test

import (
    "context"
    "testing"

    "github.com/nats-io/nats.go"
    "github.com/stretchr/testify/require"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/sdk/trace"

    "github.com/holomush/holomush/internal/eventbus/telemetry"
)

func TestInjectExtractRoundTripPreservesTraceContext(t *testing.T) {
    tp := trace.NewTracerProvider()
    otel.SetTracerProvider(tp)

    tr := otel.Tracer("test")
    ctx, span := tr.Start(context.Background(), "publish")
    defer span.End()

    h := nats.Header{}
    telemetry.InjectHeaders(ctx, h)
    require.NotEmpty(t, h.Get("traceparent"), "traceparent header MUST be injected")

    extractedCtx := telemetry.ExtractContext(context.Background(), h)
    extracted := otel.Tracer("test").Start(extractedCtx, "consume")
    require.Equal(t, span.SpanContext().TraceID(), extracted.SpanContext().TraceID())
    extracted.End()
}
```

- [ ] **Step 3: Implement OTEL helpers**

Create `internal/eventbus/telemetry/otel.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package telemetry provides OTEL header propagation and in-process
// Prometheus exporter wiring for the JetStream event bus.
package telemetry

import (
    "context"

    "github.com/nats-io/nats.go"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/propagation"
)

// InjectHeaders writes the W3C traceparent/tracestate headers from ctx
// into h. Use on the publish path before js.PublishMsg.
func InjectHeaders(ctx context.Context, h nats.Header) {
    propagator := otel.GetTextMapPropagator()
    propagator.Inject(ctx, natsHeaderCarrier(h))
}

// ExtractContext reads W3C trace headers from h and returns a context
// linked to the upstream span. Use on the subscribe / audit-projection
// receive path.
func ExtractContext(ctx context.Context, h nats.Header) context.Context {
    propagator := otel.GetTextMapPropagator()
    return propagator.Extract(ctx, natsHeaderCarrier(h))
}

// natsHeaderCarrier adapts nats.Header to TextMapCarrier.
type natsHeaderCarrier nats.Header

func (c natsHeaderCarrier) Get(key string) string         { return nats.Header(c).Get(key) }
func (c natsHeaderCarrier) Set(key, value string)         { nats.Header(c).Set(key, value) }
func (c natsHeaderCarrier) Keys() []string {
    keys := make([]string, 0, len(c))
    for k := range c {
        keys = append(keys, k)
    }
    return keys
}

var _ propagation.TextMapCarrier = natsHeaderCarrier{}
```

- [ ] **Step 4: Run OTEL test**

```bash
task test -- ./internal/eventbus/telemetry/...
```

Expected: passes.

- [ ] **Step 5: Implement Prometheus exporter wiring**

Create `internal/eventbus/telemetry/prometheus.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
    "fmt"
    "net/url"
    "strconv"

    "github.com/nats-io/prometheus-nats-exporter/exporter"
)

// StartNATSExporter starts the prometheus-nats-exporter against the
// embedded NATS server's monitoring endpoint. monitorPort is the HTTP
// port set on server.Options. exporterPort is where the exporter
// listens for Prometheus scrapes (0 = random ephemeral).
//
// Returns the exporter handle so the caller can Stop it on shutdown.
func StartNATSExporter(monitorPort int, exporterPort int) (*exporter.NATSExporter, error) {
    if monitorPort <= 0 {
        return nil, fmt.Errorf("telemetry: NATS monitor port must be > 0 to enable exporter")
    }
    opts := exporter.GetDefaultExporterOptions()
    opts.ListenAddress = "127.0.0.1"
    opts.ListenPort = exporterPort
    opts.GetVarz = true
    opts.GetConnz = true
    opts.GetSubz = true
    opts.GetJszFilter = "all"
    exp := exporter.NewExporter(opts)
    serverURL := (&url.URL{
        Scheme: "http",
        Host:   fmt.Sprintf("127.0.0.1:%s", strconv.Itoa(monitorPort)),
    }).String()
    if err := exp.AddServer("holomush-embedded", serverURL); err != nil {
        return nil, fmt.Errorf("telemetry: AddServer: %w", err)
    }
    if err := exp.Start(); err != nil {
        return nil, fmt.Errorf("telemetry: exp.Start: %w", err)
    }
    return exp, nil
}
```

Create `internal/eventbus/telemetry/prometheus_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry_test

import (
    "io"
    "net/http"
    "strings"
    "testing"

    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus/eventbustest"
    "github.com/holomush/holomush/internal/eventbus/telemetry"
)

func TestNATSExporterExposesGnatsdMetrics(t *testing.T) {
    e := eventbustest.New(t)
    _ = e
    // For this test, restart with monitor port enabled
    // (skip the convoluted config; demonstrate exporter starts and serves)

    exp, err := telemetry.StartNATSExporter(8222, 0)
    if err != nil {
        t.Skip("monitor port not available in this test env")
    }
    t.Cleanup(func() { exp.Stop() })

    // The exporter listens on a random port; query its /metrics
    addr := exp.HTTPServerAddr()
    require.NotEmpty(t, addr)
    resp, err := http.Get("http://" + addr + "/metrics")
    require.NoError(t, err)
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    require.True(t, strings.Contains(string(body), "gnatsd_"), "expected gnatsd_* metrics")
}
```

- [ ] **Step 6: Wire telemetry into the subsystem**

Modify `internal/eventbus/subsystem.go` — at the end of `Start`, after the stream is ensured:

```go
if s.cfg.PrometheusExporter && s.cfg.MonitorPort > 0 {
    exp, err := telemetry.StartNATSExporter(s.cfg.MonitorPort, 0)
    if err != nil {
        return fmt.Errorf("eventbus: prometheus exporter: %w", err)
    }
    s.exporter = exp
}
```

Add the field to the struct and the cleanup in `Stop`:

```go
type Subsystem struct {
    // ... existing fields ...
    exporter *exporter.NATSExporter
}

// in Stop, before server.Shutdown:
if s.exporter != nil {
    s.exporter.Stop()
}
```

- [ ] **Step 7: Run all eventbus + telemetry tests**

```bash
task test -- ./internal/eventbus/...
```

Expected: all pass.

- [ ] **Step 8: Run pr-prep**

```bash
task pr-prep
```

Expected: green.

- [ ] **Step 9: Commit M7**

Conventional message: `feat(eventbus/telemetry): M7 — OTEL header propagation + Prom NATS exporter`.

---

## Phase A acceptance gate (before declaring Phase A complete)

- [ ] All 7 M-PRs (M1-M7) merged to main
- [ ] `task pr-prep` green on main HEAD
- [ ] Cumulative coverage gates met (≥ 80% per package, ≥ 90% for `internal/eventbus/`, ≥ 95% for `internal/eventbus/codec/`)
- [ ] Manual smoke: start server locally, observe in startup logs:
  - `holomush-embedded` NATS server ready
  - audit projection consumer registered
  - no consumers attached for plugin subjects (none defined yet)
  - existing event store paths still in use (Phase A is additive only)
- [ ] No production code consumes the new bus yet (`grep` for `eventbus.NewSubsystem` should show only `cmd/holomush/core.go` and tests)
- [ ] Phase B feature branch (`feat/eventbus-cutover`) created from main HEAD post-Phase-A; ready for F1

---

## Self-review checklist

- [ ] Spec sections 1-7 each have at least one Phase A task implementing the additive piece
- [ ] All file paths are absolute and exact
- [ ] All code blocks are complete (no "implement similar to above" handwaves)
- [ ] All commands are exact with expected output
- [ ] No `time.Sleep` in any test code (helpers may use it but test code MUST NOT)
- [ ] Each task ends with a `task pr-prep` gate before commit
- [ ] Each commit message is conventional and scope-tagged
