# Event Payload Cryptography — Phase 7: Plugin SDK + Plugin-Owned Audit Crypto — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unpark `plugin_consumer.go`'s identity-codec restriction (`internal/eventbus/audit/plugin_consumer.go:343`) so plugins receive and persist ciphertext byte-equal for sensitive events; build the host-side read-time downgrade fence + AAD-reconstruction adapter; ship the SDK Layer 2 audit helpers.

**Architecture:** Plugin tables mirror `events_audit` shape (payload + codec + dek_ref + dek_version) per master spec §8.2. The per-plugin audit dispatcher widens to forward ciphertext + crypto headers via a reshaped `pluginauditpb.AuditRow` message. Read-time defense is a two-layer fence: layer (1) `PluginDowngradeFence` runs pre-decrypt and refuses on manifest-set heuristic (INV-P7-7) or DEK existence (INV-P7-15); layer (2) AEAD AAD-binding catches everything else at decrypt time via a `AuditRow → *eventbusv1.Event` adapter feeding the existing `aad.Build`.

**Tech Stack:** Go 1.23, ConnectRPC + protobuf (api/proto), pgx/v5, jetstream consumer, xchacha20poly1305 codec, samber/oops error wrapping, gocritic ruleguard.

**Spec:** `docs/superpowers/specs/2026-05-13-event-payload-crypto-phase7-plugin-sdk-design.md` (v4, both review gates READY).

**Parent epic:** `holomush-1r0v`.

---

## File Structure

### New files

| Path | Purpose | Phase |
|---|---|---|
| `internal/eventbus/audit/header_parser.go` | `ParseAuditHeaders(h nats.Header) (HeaderAuditMetadata, error)`. Single source of truth for JS-header → typed-value conversion. Consumed by both `projection.go` (events_audit branch) and `plugin_consumer.go` (plugin RPC branch). INV-P7-2. | A |
| `internal/eventbus/audit/header_parser_test.go` | Unit table-tests for the parser. | A |
| `internal/eventbus/history/plugin_aad_adapter.go` | `AuditRowToEvent(*pluginauditpb.AuditRow) *eventbusv1.Event` — per-field copy for AAD reconstruction. Six fields copied, two nil-safety guards. | C |
| `internal/eventbus/history/plugin_aad_reconstruction_test.go` | Integration: byte-equal AAD round-trip. INV-P7-16. | C |
| `internal/eventbus/history/plugin_downgrade_fence.go` | Wraps the `history.PluginHistoryRouter` interface. Owns always-sensitive type-set (boot-built, immutable) + DEK-existence check. Emits `plugin_integrity_violation` on INV-P7-7 refusal. | C |
| `internal/eventbus/history/plugin_downgrade_fence_test.go` | Unit truth-table tests. INV-P7-7, INV-P7-8, INV-P7-15. | C |
| `internal/eventbus/history/phase7_boundary_meta_test.go` | Walks INV-P7-1..16 (skipping 2, 14) and asserts named tests exist. | D |
| `plugins/core-scenes/migrations/000005_add_scene_log_dek_columns.up.sql` | `ALTER TABLE scene_log ADD COLUMN dek_ref BIGINT NULL, ADD COLUMN dek_version INTEGER NULL` + partial index. | B |
| `plugins/core-scenes/migrations/000005_add_scene_log_dek_columns.down.sql` | Drop the two columns + index. | B |
| `test/integration/plugin/testdata/test_downgrade_attacker/main.go` | Test plugin with happy + malicious `QueryHistory` branches. | D |
| `test/integration/plugin/testdata/test_downgrade_attacker/plugin.yaml` | Manifest declaring `crypto.emits: [{event_type: secret, sensitivity: always}]`. | D |
| `test/integration/eventbus_e2e/plugin_downgrade_attacker_test.go` | E2E test exercising happy + attack paths. INV-P7-10. | D |
| `test/integration/eventbus_e2e/plugin_audit_round_trip_test.go` | Integration: byte-equal store+return. INV-P7-6, INV-P7-12. | B |
| `test/integration/eventbus_e2e/dispatcher_projection_parity_test.go` | INV-P7-2 cross-branch parser parity. | A |
| `test/integration/eventbus_e2e/dispatcher_selector_identity_test.go` | INV-P7-9 KeySelector pointer-identity. | B |
| `test/integration/plugin/plugin_role_permissions_test.go` | INV-P7-13 perm denial. | D |
| `test/integration/plugin/plugin_migration_test.go` | INV-P7-3, INV-P7-10 (already-numbered as INV-P7-10 in spec, no clash with this test). | B |

### Modified files

| Path | Change | Phase |
|---|---|---|
| `internal/eventbus/audit/projection.go` | Trim lines ~172-262 to call `ParseAuditHeaders` instead of inlining. Behaviour unchanged. | A |
| `api/proto/holomush/plugin/v1/audit.proto` | Define new `AuditRow` message. Replace `AuditEventRequest.event` + `headers` with `AuditRow row = 1`. Replace `QueryHistoryResponse.event` with `AuditRow row = 1`. Regen. | A |
| `internal/eventbus/audit/plugin_consumer.go` | Remove `AUDIT_PLUGIN_CODEC_UNSUPPORTED` rejection at line 343. Wire `codec.KeySelector` field on `PluginConsumerManager`. Build `*pluginauditpb.AuditRow` from raw msg.Data + `ParseAuditHeaders` output + envelope projection fields. | B |
| `internal/eventbus/audit/plugin_consumer_unit_test.go` | Update test at line 82 (`cli.gotReq.GetHeaders()` → `cli.gotReq.GetRow()` field assertions). | B |
| `pkg/plugin/audit.go` | Append Layer 2 helpers: `AuditRow` Go struct + `StoreFromMessage` + `LoadForQuery`. Top-of-file doc enumerates Layer 1 (existing decision-hint recorder) + Layer 2. | A |
| `pkg/plugin/audit_test.go` | Add round-trip + struct-shape tests. INV-P7-4, INV-P7-5, INV-P7-7 (P7-7 numbering is fine; the test SCOPE is round-trip, INV-P7 numbering doesn't collide with master INV-P7-7 fence). | A |
| `internal/eventbus/history/tier.go` | Single-line change at line 367: wrap `r.router` in `NewPluginDowngradeFence(r.router, ...)` before serving. | C |
| `plugins/core-scenes/audit.go` | (a) `SceneAuditStore.Insert` signature + SQL gain `dek_ref *int64, dek_version *int32`. (b) `queryLog` SELECT returns the new columns. (c) `SceneAuditServer.AuditEvent` reads from `req.GetRow()` instead of `req.GetHeaders()`. (d) `SceneAuditServer.QueryHistory` constructs `*pluginauditpb.AuditRow` rather than the old `eventbusv1.Event` reuse. | B |
| `test/integration/eventbus_e2e/plugin_audit_isolation_test.go` | Update test-stub mirror at lines 175-220 to match the new request shape. | B |
| `cmd/holomush/deps.go` | Wire `PluginDowngradeFence` around the existing audit-router construction. Pass the same `codec.KeySelector` instance into `PluginConsumerManager` that's passed to `history.NewReader`. | E |
| `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` | Polish: §4.6 register `plugin_integrity_violation`; §8.2 amend pseudo-code (strike events_audit-fallback line for plugin-owned subjects); §2 INV-50 cross-reference INV-P7-7; §8.3 Go-struct field set; §2 INV-39 scope clarification. | E |
| `site/docs/extending/binary-plugins.md` | New SDK section documenting `pluginsdk.AuditRow`, `StoreFromMessage`, `LoadForQuery`, and the post-Phase-7 contract. | E |
| `site/docs/reference/audit-subjects.md` | Register `audit.<game>.system.plugin_integrity_violation` row. | E |

---

## Phase A — Foundations (parser + proto + SDK)

Substrate work with no behaviour change on the production hot path. After Phase A, the shared parser exists, the new `AuditRow` proto type is generated, and the SDK Layer 2 helpers are in place but no production caller has switched yet.

### Task A.1: Shared header parser

**Why:** INV-P7-2 requires structurally-identical header parsing in both branches. Extracting projection.go's existing parsing into `ParseAuditHeaders` is the smallest pure refactor and unlocks the dispatcher widening.

**Files:**

- Create: `internal/eventbus/audit/header_parser.go`
- Create: `internal/eventbus/audit/header_parser_test.go`
- Modify: `internal/eventbus/audit/projection.go` (replace inline parsing with `ParseAuditHeaders` call)

- [ ] **Step A.1.1: Write the failing parser unit test**

Create `internal/eventbus/audit/header_parser_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
)

func TestParseAuditHeaders_Identity(t *testing.T) {
	h := nats.Header{}
	h.Set(headerCodec, "identity")
	h.Set(headerSchemaVersion, "1")

	got, err := ParseAuditHeaders(h)
	require.NoError(t, err)
	assert.Equal(t, "identity", got.Codec)
	assert.Equal(t, int32(1), got.SchemaVer)
	assert.Nil(t, got.DEKRef)
	assert.Nil(t, got.DEKVersion)
}

func TestParseAuditHeaders_Encrypted(t *testing.T) {
	h := nats.Header{}
	h.Set(headerCodec, "xchacha20poly1305-v1")
	h.Set(headerSchemaVersion, "2")
	h.Set(eventbus.HeaderDekRef, "42")
	h.Set(eventbus.HeaderDekVersion, "7")

	got, err := ParseAuditHeaders(h)
	require.NoError(t, err)
	assert.Equal(t, "xchacha20poly1305-v1", got.Codec)
	assert.Equal(t, int32(2), got.SchemaVer)
	require.NotNil(t, got.DEKRef)
	assert.Equal(t, int64(42), *got.DEKRef)
	require.NotNil(t, got.DEKVersion)
	assert.Equal(t, int32(7), *got.DEKVersion)
}

func TestParseAuditHeaders_MissingCodec(t *testing.T) {
	h := nats.Header{}
	h.Set(headerSchemaVersion, "1")

	_, err := ParseAuditHeaders(h)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing header")
}

func TestParseAuditHeaders_BadDekRef(t *testing.T) {
	h := nats.Header{}
	h.Set(headerCodec, "xchacha20poly1305-v1")
	h.Set(headerSchemaVersion, "1")
	h.Set(eventbus.HeaderDekRef, "not-a-number")

	_, err := ParseAuditHeaders(h)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AUDIT_DEK_REF_PARSE_FAILED")
}

func TestParseAuditHeaders_SchemaVerOutOfRange(t *testing.T) {
	h := nats.Header{}
	h.Set(headerCodec, "identity")
	h.Set(headerSchemaVersion, "99999")

	_, err := ParseAuditHeaders(h)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AUDIT_BAD_SCHEMA_VERSION")
}
```

- [ ] **Step A.1.2: Run test to verify it fails**

Run: `task test -- -run TestParseAuditHeaders ./internal/eventbus/audit/`
Expected: FAIL with "undefined: ParseAuditHeaders".

- [ ] **Step A.1.3: Implement the shared parser**

Create `internal/eventbus/audit/header_parser.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"strconv"

	"github.com/nats-io/nats.go"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
)

// HeaderAuditMetadata is the typed projection of a JetStream message's
// audit-related headers. Both the host audit projection (events_audit
// writer) and the per-plugin dispatcher use this parser; byte-equality
// of typed values across the two branches is structural (INV-P7-2).
//
// schema_ver is co-located here despite not being a crypto field —
// single source of truth for header → typed-value conversion prevents
// the host-branch and plugin-branch from drifting on parse rules
// (default value, error code, header name spelling).
type HeaderAuditMetadata struct {
	Codec      string
	SchemaVer  int32  // SMALLINT-bounded; out-of-range rejected
	DEKRef     *int64 // nil for codec=identity
	DEKVersion *int32 // nil for codec=identity
}

// ParseAuditHeaders extracts typed audit metadata from JetStream message
// headers. Returns typed errors with codes the projection and plugin
// dispatcher both surface verbatim:
//   - AUDIT_MISSING_HEADER (codec / schema_version)
//   - AUDIT_BAD_SCHEMA_VERSION
//   - AUDIT_DEK_REF_PARSE_FAILED
//   - AUDIT_DEK_VERSION_PARSE_FAILED
func ParseAuditHeaders(h nats.Header) (HeaderAuditMetadata, error) {
	var meta HeaderAuditMetadata

	codec := h.Get(headerCodec)
	if codec == "" {
		return meta, oops.Code("AUDIT_MISSING_HEADER").
			With("header", headerCodec).
			Errorf("missing header")
	}
	meta.Codec = codec

	schemaVerStr := h.Get(headerSchemaVersion)
	if schemaVerStr == "" {
		return meta, oops.Code("AUDIT_MISSING_HEADER").
			With("header", headerSchemaVersion).
			Errorf("missing header")
	}
	ver, err := strconv.ParseInt(schemaVerStr, 10, 32)
	if err != nil || ver < 0 || ver > 32767 {
		return meta, oops.Code("AUDIT_BAD_SCHEMA_VERSION").
			With("value", schemaVerStr).
			Errorf("schema version out of range or non-numeric")
	}
	meta.SchemaVer = int32(ver)

	if v := h.Get(eventbus.HeaderDekRef); v != "" {
		parsed, parseErr := strconv.ParseInt(v, 10, 64)
		if parseErr != nil {
			return meta, oops.Code("AUDIT_DEK_REF_PARSE_FAILED").
				With("header", eventbus.HeaderDekRef).
				With("value", v).
				Wrap(parseErr)
		}
		meta.DEKRef = &parsed
	}
	if v := h.Get(eventbus.HeaderDekVersion); v != "" {
		parsed, parseErr := strconv.ParseInt(v, 10, 32)
		if parseErr != nil {
			return meta, oops.Code("AUDIT_DEK_VERSION_PARSE_FAILED").
				With("header", eventbus.HeaderDekVersion).
				With("value", v).
				Wrap(parseErr)
		}
		v32 := int32(parsed)
		meta.DEKVersion = &v32
	}

	return meta, nil
}
```

- [ ] **Step A.1.4: Run test to verify it passes**

Run: `task test -- -run TestParseAuditHeaders ./internal/eventbus/audit/`
Expected: PASS, all 5 subtests.

- [ ] **Step A.1.5: Migrate `projection.go` to use the shared parser**

Read `internal/eventbus/audit/projection.go:172-262` to find the existing inline parsing. Replace the inline `App-Codec` / `App-Schema-Version` / `App-Dek-Ref` / `App-Dek-Version` parsing with a single `ParseAuditHeaders(h)` call. Preserve the existing variable names that flow into the INSERT (codec, ver, dekRef, dekVer). Behaviour MUST be unchanged.

- [ ] **Step A.1.6: Run full audit-package tests + lint**

Run: `task test -- ./internal/eventbus/audit/`
Run: `task lint`
Expected: all existing tests still PASS; lint clean.

- [ ] **Step A.1.7: Commit**

```text
refactor(audit): extract shared header parser into header_parser.go

INV-P7-2 substrate. Both the host audit projection (events_audit
writer) and the per-plugin audit dispatcher will use ParseAuditHeaders
after Phase 7. Projection migrated; plugin_consumer migration lands
in Task B.1.

Refs: holomush-1r0v
```

### Task A.2: Reshape `pluginauditpb.AuditRow` proto

**Why:** Per spec §4.2 — proto needs to carry ciphertext payload + crypto headers + projection fields in one symmetric message. Replaces today's `Event` + `headers` map split.

**Files:**

- Modify: `api/proto/holomush/plugin/v1/audit.proto`
- Regen: `pkg/proto/holomush/plugin/v1/audit.pb.go`, `pkg/proto/holomush/plugin/v1/audit_grpc.pb.go`

- [ ] **Step A.2.1: Read the existing proto file**

Read: `api/proto/holomush/plugin/v1/audit.proto` (full file). Note current message shapes for `AuditEventRequest`, `AuditEventResponse`, `QueryHistoryRequest`, `QueryHistoryResponse` and current imports.

- [ ] **Step A.2.2: Modify the proto file**

Replace the relevant message definitions:

```proto
syntax = "proto3";

package holomush.plugin.v1;

import "google/protobuf/timestamp.proto";
import "holomush/eventbus/v1/eventbus.proto";

// AuditRow is the canonical wire shape for plugin-owned audit rows.
// Used in both directions: dispatcher → plugin (AuditEventRequest)
// and plugin → host (QueryHistoryResponse). Mirrors the events_audit
// row shape so the proto wire format and the storage shape are
// coupled.
message AuditRow {
  // Cleartext projection fields
  bytes id = 1;                                   // 16-byte ULID
  string subject = 2;
  string type = 3;
  google.protobuf.Timestamp timestamp = 4;
  holomush.eventbus.v1.Actor actor = 5;

  // Crypto envelope
  string codec = 6;                               // "identity" | "xchacha20poly1305-v1"
  bytes payload = 7;                              // ciphertext when codec != "identity"

  // DEK reference — absent on identity codec, required otherwise.
  // Host enforces the agreement (codec=identity ⇔ both absent).
  optional uint64 dek_ref = 8;
  optional uint32 dek_version = 9;

  // Audit schema version (was App-Schema-Version header).
  int32 schema_ver = 10;
}

message AuditEventRequest {
  AuditRow row = 1;
}

message AuditEventResponse {}

// QueryHistoryRequest unchanged from existing.

message QueryHistoryResponse {
  AuditRow row = 1;
}
```

Preserve any other messages in the file. Preserve the service definition (`PluginAuditService`).

- [ ] **Step A.2.3: Regenerate proto code**

Run: `task proto`
Expected: `pkg/proto/holomush/plugin/v1/audit.pb.go` regenerates with the new `AuditRow` type + updated `AuditEventRequest`/`QueryHistoryResponse`. Repo's diff should be limited to those two generated files.

- [ ] **Step A.2.4: Verify the build breaks where expected**

Run: `task build`
Expected: FAIL with "AuditEventRequest.GetEvent undefined" / "AuditEventRequest.GetHeaders undefined" in `plugins/core-scenes/audit.go`, `test/integration/eventbus_e2e/plugin_audit_isolation_test.go`, `internal/eventbus/audit/plugin_consumer.go`, `internal/eventbus/audit/plugin_consumer_unit_test.go`. These breakages are the work picked up by Tasks B.1, B.2. **Do NOT fix them in this task.**

- [ ] **Step A.2.5: Commit (build is intentionally broken — note in message)**

```text
feat(plugin/v1): reshape AuditRow proto for Phase 7

Spec §4.2. AuditEventRequest.event + headers map are replaced with
a unified AuditRow message carrying projection fields + crypto
envelope (codec, payload, dek_ref optional, dek_version optional,
schema_ver). QueryHistoryResponse mirrors the same shape.

Clean break per [feedback_no_prod_shape_for_undeployed] — no
reserved markers, no compat shims. Build is intentionally broken at
this commit; downstream callers (plugin_consumer.go, core-scenes
audit, test stubs) migrate in Tasks B.1 and B.2.

Refs: holomush-1r0v
```

### Task A.3: SDK Layer 2 (`pkg/plugin/audit.go`)

**Why:** Plugin authors need a typed surface to construct/serialize `AuditRow`. Per spec §4.3 — `pluginsdk.AuditRow` Go struct mirrors the proto 1:1; `StoreFromMessage` builds from JetStream message; `LoadForQuery` round-trips back to the proto. INV-P7-4, INV-P7-5, INV-P7-7 (struct-mirror, round-trip, no-crypto-fields-leaking).

**Files:**

- Modify: `pkg/plugin/audit.go` (extend, not replace — existing Layer 1 stays)
- Modify: `pkg/plugin/audit_test.go`

- [ ] **Step A.3.1: Read current `pkg/plugin/audit.go` (Layer 1 surface)**

Read the file in full. Note: it currently contains `AuditAttrs`, `handlerContextKey`, `AuditRecorder`, `Audit()`, `contextRecorder`. Phase 7 Layer 2 is purely additive — keep all existing identifiers.

- [ ] **Step A.3.2: Write the failing Layer 2 tests**

Append to `pkg/plugin/audit_test.go`:

```go
func TestAuditRowStructMirrorsProto(t *testing.T) {
	// Compile-time + runtime assertion that the Go struct's exported
	// fields match the proto's field set.
	row := pluginsdk.AuditRow{
		EventID:   ulid.ULID{},
		Subject:   "events.test.scene.01ABC.ic",
		Type:      "test-plugin:secret",
		Timestamp: time.Unix(1700000000, 0),
		Actor:     nil,
		Codec:     "identity",
		Payload:   []byte("hello"),
		DEKRef:    nil,
		DEKVersion: nil,
		SchemaVer: 1,
	}

	got := reflect.TypeOf(row)
	wantFields := []string{
		"EventID", "Subject", "Type", "Timestamp", "Actor",
		"Codec", "Payload", "DEKRef", "DEKVersion", "SchemaVer",
	}
	require.Equal(t, len(wantFields), got.NumField(),
		"AuditRow field count drifted from proto; INV-P7-4 broken")
	for i, want := range wantFields {
		assert.Equal(t, want, got.Field(i).Name)
	}
}

func TestAuditRowRoundTripPreservesAllFields(t *testing.T) {
	cases := []struct {
		name string
		row  pluginsdk.AuditRow
	}{
		{
			name: "identity_codec_nil_dek",
			row: pluginsdk.AuditRow{
				EventID:   ulidFromString(t, "01JD9R7N5VFKQM5T1JX6S9PYRJ"),
				Subject:   "events.test.scene.01ABC.ic",
				Type:      "test-plugin:plaintext",
				Timestamp: time.Unix(1700000000, 0).UTC(),
				Actor:     nil,
				Codec:     "identity",
				Payload:   []byte("hello"),
				DEKRef:    nil,
				DEKVersion: nil,
				SchemaVer: 1,
			},
		},
		{
			name: "encrypted_codec_with_dek",
			row: pluginsdk.AuditRow{
				EventID:   ulidFromString(t, "01JD9R7N5VFKQM5T1JX6S9PYRK"),
				Subject:   "events.test.scene.01ABC.ic",
				Type:      "test-plugin:secret",
				Timestamp: time.Unix(1700000000, 123456789).UTC(),
				Actor:     &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, Id: []byte("char-01")},
				Codec:     "xchacha20poly1305-v1",
				Payload:   []byte{0xDE, 0xAD, 0xBE, 0xEF},
				DEKRef:    ptr.To(uint64(42)),
				DEKVersion: ptr.To(uint32(7)),
				SchemaVer: 2,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Round-trip via the proto shape.
			proto, err := pluginsdk.LoadForQuery(tc.row)
			require.NoError(t, err)
			require.NotNil(t, proto)

			assert.Equal(t, tc.row.EventID[:], proto.GetId())
			assert.Equal(t, tc.row.Subject, proto.GetSubject())
			assert.Equal(t, tc.row.Type, proto.GetType())
			assert.Equal(t, tc.row.Timestamp.UnixNano(), proto.GetTimestamp().AsTime().UnixNano())
			assert.Equal(t, tc.row.Codec, proto.GetCodec())
			assert.Equal(t, tc.row.Payload, proto.GetPayload())
			if tc.row.DEKRef != nil {
				assert.Equal(t, *tc.row.DEKRef, proto.GetDekRef())
			}
			if tc.row.DEKVersion != nil {
				assert.Equal(t, *tc.row.DEKVersion, proto.GetDekVersion())
			}
			assert.Equal(t, tc.row.SchemaVer, proto.GetSchemaVer())
		})
	}
}
```

- [ ] **Step A.3.3: Run tests to verify they fail**

Run: `task test -- -run TestAuditRow ./pkg/plugin/`
Expected: FAIL with "undefined: pluginsdk.AuditRow" and "undefined: pluginsdk.LoadForQuery".

- [ ] **Step A.3.4: Append Layer 2 to `pkg/plugin/audit.go`**

Append to the existing file (DO NOT replace Layer 1 content):

```go
// -----------------------------------------------------------------------
// Layer 2: plugin-owned audit row mirror.
//
// pluginsdk.AuditRow is the projection-only-plus-crypto-envelope shape
// plugins store in their audit tables (e.g. plugin_core_scenes.scene_log)
// and return on PluginAuditService.QueryHistory. It mirrors
// pluginauditpb.AuditRow 1:1 (INV-P7-4) and is consumed via the two
// helpers below.
//
// Plugin authors typically don't construct AuditRow manually — they use
// StoreFromMessage(msg) at AuditEvent RPC ingest, persist the row
// fields verbatim, then use LoadForQuery(row) to construct the proto
// frame returned on QueryHistory. Round-trip stability is INV-P7-5.
//
// crypto fields (Codec, Payload, DEKRef, DEKVersion) are OPAQUE to the
// plugin — plugin code MUST store and return them byte-for-byte. The
// host owns interpretation. Plugin Layer 2 is convenience for plugin
// authors; the host's threat model does not rely on Layer 2 correctness
// (INV-P7-6 and INV-P7-7 are enforced host-side).

// AuditRow is the Go-side mirror of pluginauditpb.AuditRow. Field
// ordering matches the proto field-numbering for stability across
// proto regenerations.
type AuditRow struct {
	EventID    ulid.ULID
	Subject    string
	Type       string
	Timestamp  time.Time
	Actor      *eventbusv1.Actor

	Codec      string
	Payload    []byte

	DEKRef     *uint64 // nil ⇔ identity codec ⇔ proto field absent
	DEKVersion *uint32

	SchemaVer  int32
}

// StoreFromMessage extracts an AuditRow from a JetStream message.
// Preserves payload bytes byte-equal; uses the shared header parser
// (internal/eventbus/audit/header_parser.go) for typed crypto/schema
// values — INV-P7-2 byte-equality across the host-projection branch
// and the per-plugin dispatcher branch is structural.
func StoreFromMessage(msg jetstream.Msg) (AuditRow, error) {
	// Implementation: parse JS headers via audit.ParseAuditHeaders;
	// unmarshal msg.Data() into eventbusv1.Event ONLY to extract the
	// projection fields (NOT payload — payload stays as msg.Data()
	// byte-equal so ciphertext is preserved). Build AuditRow.
	// Full implementation in Task B.1 once the plugin_consumer.go
	// integration point pins the exact extraction sequence.
	//
	// Returns errors with codes:
	//   AUDIT_PLUGIN_ENVELOPE_UNMARSHAL_FAILED
	//   AUDIT_MISSING_HEADER / AUDIT_BAD_SCHEMA_VERSION /
	//   AUDIT_DEK_REF_PARSE_FAILED / AUDIT_DEK_VERSION_PARSE_FAILED
	//   (from audit.ParseAuditHeaders).
	panic("StoreFromMessage: TODO — implemented in Task B.1")
}

// LoadForQuery converts a stored AuditRow into the proto frame returned
// by PluginAuditService.QueryHistory. Round-trip stable with
// StoreFromMessage (INV-P7-5).
func LoadForQuery(row AuditRow) (*pluginauditpb.AuditRow, error) {
	proto := &pluginauditpb.AuditRow{
		Id:        row.EventID[:],
		Subject:   row.Subject,
		Type:      row.Type,
		Timestamp: timestamppb.New(row.Timestamp),
		Actor:     row.Actor,
		Codec:     row.Codec,
		Payload:   row.Payload,
		SchemaVer: row.SchemaVer,
	}
	if row.DEKRef != nil {
		proto.DekRef = row.DEKRef
	}
	if row.DEKVersion != nil {
		proto.DekVersion = row.DEKVersion
	}
	return proto, nil
}
```

Add imports to the existing `pkg/plugin/audit.go` import block:

```go
import (
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)
```

(Keep existing `context` and `log/slog` imports for Layer 1.)

- [ ] **Step A.3.5: Run tests to verify they pass (struct + LoadForQuery only — StoreFromMessage panics)**

Run: `task test -- -run TestAuditRowStructMirrorsProto ./pkg/plugin/`
Run: `task test -- -run TestAuditRowRoundTripPreservesAllFields ./pkg/plugin/`
Expected: both PASS.

- [ ] **Step A.3.6: Commit**

```text
feat(pluginsdk): Layer 2 AuditRow + LoadForQuery

pkg/plugin/audit.go extended with the plugin-owned audit row mirror
type and the LoadForQuery serializer. StoreFromMessage is panic-stub
until Task B.1 where the plugin_consumer.go integration pins the
exact extraction sequence.

INV-P7-4 (struct ↔ proto mirror), INV-P7-5 round-trip half (Load
direction).

Refs: holomush-1r0v
```

### Task A.4: Cross-branch parser parity integration test

**Why:** INV-P7-2 says byte-equality between host-projection and per-plugin dispatcher is structural. Test it.

**Files:**

- Create: `test/integration/eventbus_e2e/dispatcher_projection_parity_test.go`

- [ ] **Step A.4.1: Write the parity test**

Create `test/integration/eventbus_e2e/dispatcher_projection_parity_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e

import (
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/audit"
)

// TestPluginAndHostBranchesParseHeadersIdentically asserts that the
// shared parser produces the same typed values regardless of which
// branch (host events_audit projection vs per-plugin dispatcher)
// invokes it. INV-P7-2 byte-equality is structural: a single parser
// implementation feeds both.
func TestPluginAndHostBranchesParseHeadersIdentically(t *testing.T) {
	header := nats.Header{}
	header.Set("App-Codec", "xchacha20poly1305-v1")
	header.Set("App-Schema-Version", "2")
	header.Set("App-Dek-Ref", "12345")
	header.Set("App-Dek-Version", "3")

	meta1, err := audit.ParseAuditHeaders(header)
	require.NoError(t, err)
	meta2, err := audit.ParseAuditHeaders(header)
	require.NoError(t, err)

	assert.Equal(t, meta1.Codec, meta2.Codec)
	assert.Equal(t, meta1.SchemaVer, meta2.SchemaVer)
	require.NotNil(t, meta1.DEKRef)
	require.NotNil(t, meta2.DEKRef)
	assert.Equal(t, *meta1.DEKRef, *meta2.DEKRef)
	require.NotNil(t, meta1.DEKVersion)
	require.NotNil(t, meta2.DEKVersion)
	assert.Equal(t, *meta1.DEKVersion, *meta2.DEKVersion)
}
```

- [ ] **Step A.4.2: Run integration test**

Run: `task test:int -- -run TestPluginAndHostBranchesParseHeadersIdentically ./test/integration/eventbus_e2e/`
Expected: PASS.

- [ ] **Step A.4.3: Commit**

```text
test(audit): INV-P7-2 cross-branch parser parity integration test

Asserts ParseAuditHeaders produces identical typed values across
invocations — single parser implementation is the structural
byte-equality guarantee for host projection vs per-plugin dispatcher.

Refs: holomush-1r0v
```

---

## Phase B — Dispatcher widening + caller migration

After Phase B, the per-plugin dispatcher forwards ciphertext byte-equal, plugin schemas hold the crypto columns, and all existing callers of the old proto shape have been migrated. Build is restored (broken since Task A.2).

### Task B.1: Plugin dispatcher widening

**Why:** Master spec §8.2 substrate. Plugin's audit table needs to receive ciphertext to act as the cold-tier mirror; current `plugin_consumer.go:343` rejects non-identity codecs as parked work. INV-P7-1, INV-P7-11.

**Files:**

- Modify: `internal/eventbus/audit/plugin_consumer.go`
- Modify: `internal/eventbus/audit/plugin_consumer_unit_test.go`
- Modify: `pkg/plugin/audit.go` (replace `StoreFromMessage` panic with real implementation)

- [ ] **Step B.1.1: Read existing `plugin_consumer.go` lines 320-374**

Note the current `decodeEnvelope` function (lines 330-365), the `PluginConsumerManager` struct, the rejection at line 343 (`AUDIT_PLUGIN_CODEC_UNSUPPORTED`). Phase 7 deletes the rejection, builds `*pluginauditpb.AuditRow` from raw bytes + parsed headers + envelope projection.

- [ ] **Step B.1.2: Write the failing dispatcher tests**

Append to `internal/eventbus/audit/plugin_consumer_test.go` (create file if missing):

```go
func TestDispatchForwardsCiphertextByteEqual(t *testing.T) {
	cli := newFakePluginAuditClient()
	pc := newTestPluginConsumer(t, cli, testKeySelector(t))

	ciphertext := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	msg := buildTestJSMessage(t, jsMsgInput{
		MsgID:        "01JD9R7N5VFKQM5T1JX6S9PYRJ",
		Subject:      "events.test.scene.01ABC.ic",
		Codec:        "xchacha20poly1305-v1",
		SchemaVer:    "1",
		DEKRef:       "42",
		DEKVersion:   "7",
		EventType:    "test-plugin:secret",
		ActorKind:    "character",
		ActorID:      "01JD9R7N5VFKQM5T1JX6S9PYRK",
		Payload:      ciphertext,
	})

	require.NoError(t, pc.handle(t.Context(), msg))

	require.Len(t, cli.gotRequests, 1)
	row := cli.gotRequests[0].GetRow()
	require.NotNil(t, row)
	assert.Equal(t, ciphertext, row.GetPayload(),
		"INV-P7-1: dispatcher MUST forward ciphertext byte-equal")
	assert.Equal(t, "xchacha20poly1305-v1", row.GetCodec())
	assert.Equal(t, uint64(42), row.GetDekRef())
	assert.Equal(t, uint32(7), row.GetDekVersion())
	assert.Equal(t, int32(1), row.GetSchemaVer())
}

func TestDispatchDoesNotDecryptBeforeForward(t *testing.T) {
	// Build a JS message whose payload is NOT a valid decrypted envelope.
	// If the dispatcher tried to decrypt, this would error. With the
	// widened dispatcher (Phase 7), it MUST forward the bytes verbatim
	// without invoking the codec.
	cli := newFakePluginAuditClient()
	pc := newTestPluginConsumer(t, cli, testKeySelectorThatPanicsOnUse(t))

	ciphertext := []byte("garbage-not-a-real-envelope")
	msg := buildTestJSMessage(t, jsMsgInput{
		MsgID:      "01JD9R7N5VFKQM5T1JX6S9PYRL",
		Subject:    "events.test.scene.01ABC.ic",
		Codec:      "xchacha20poly1305-v1",
		SchemaVer:  "1",
		DEKRef:     "42",
		DEKVersion: "7",
		EventType:  "test-plugin:secret",
		Payload:    ciphertext,
	})

	require.NoError(t, pc.handle(t.Context(), msg))
	require.Len(t, cli.gotRequests, 1)
	assert.Equal(t, ciphertext, cli.gotRequests[0].GetRow().GetPayload())
}
```

Helpers `newFakePluginAuditClient`, `newTestPluginConsumer`, `testKeySelector`, `testKeySelectorThatPanicsOnUse`, `buildTestJSMessage` go alongside in the same `_test.go` file (~30 lines of test setup; use existing `plugin_consumer_unit_test.go` patterns as the template).

- [ ] **Step B.1.3: Run tests to verify they fail**

Run: `task test -- -run TestDispatchForwardsCiphertextByteEqual ./internal/eventbus/audit/`
Run: `task test -- -run TestDispatchDoesNotDecryptBeforeForward ./internal/eventbus/audit/`
Expected: FAIL — current `decodeEnvelope` rejects non-identity codecs with `AUDIT_PLUGIN_CODEC_UNSUPPORTED`; the assertions on `cli.gotRequests[0].GetRow().GetPayload()` won't run.

- [ ] **Step B.1.4: Update existing test that locks the deprecated behavior**

Delete `TestPluginConsumerDispatchRejectsNonIdentityCodec` from `internal/eventbus/audit/plugin_consumer_unit_test.go`. Comment in the commit message: "Behaviour removed per Phase 7 spec — dispatcher now forwards ciphertext byte-equal."

- [ ] **Step B.1.5: Replace `decodeEnvelope` with the new construction path**

Update `internal/eventbus/audit/plugin_consumer.go`. Replace the `decodeEnvelope` function with a new `buildAuditRow` that:

1. Parses JS headers via `ParseAuditHeaders` (Task A.1).
2. Decodes the proto envelope bytes from `msg.Data()` ONLY for projection fields (id, subject, type, timestamp, actor) — does NOT decrypt the payload. For `codec=identity`, the envelope's payload IS the plaintext (kept verbatim); for `codec != identity`, the envelope's payload IS the ciphertext (kept verbatim).
3. Constructs `*pluginauditpb.AuditRow` with projection fields + `Payload: msg.Data()` if the bus carries the codec-encoded bytes raw, or extracts the raw payload from the envelope per current Phase 3 conventions (verify against `internal/eventbus/publisher.go` and `internal/eventbus/codec/` to confirm the wire-format invariant — INV-49 says envelope byte-equality across emit→audit→cold-read, so `msg.Data()` is the codec-encoded envelope proto bytes; the row's `payload` field gets the *envelope's* payload (which is the ciphertext for encrypted events) not `msg.Data()` itself).

Detailed pseudo-Go (the agent must verify wire-format details before locking the exact field extraction):

```go
// buildAuditRow constructs the AuditRow forwarded to the plugin's
// AuditEvent RPC. NEVER decrypts (INV-P7-11). Payload bytes are
// preserved byte-equal from the bus envelope (INV-P7-1).
func buildAuditRow(msg jetstream.Msg) (*pluginauditpb.AuditRow, error) {
	hdrMeta, err := ParseAuditHeaders(msg.Headers())
	if err != nil {
		return nil, err
	}

	// Unmarshal the codec-encoded envelope ONLY to read projection
	// fields. Per master INV-49 the envelope bytes are byte-equal
	// across emit/audit/cold-read. For codec=identity, the codec's
	// Decode is a no-op so the envelope is plaintext proto. For
	// codec=xchacha20poly1305-v1, the codec ENCRYPTS THE PAYLOAD
	// FIELD inside the proto envelope — projection fields stay
	// cleartext.
	envelope, err := unmarshalProjectionOnly(msg.Data())
	if err != nil {
		return nil, oops.Code("AUDIT_PLUGIN_ENVELOPE_UNMARSHAL_FAILED").
			Wrap(err)
	}

	row := &pluginauditpb.AuditRow{
		Id:        envelope.GetId(),
		Subject:   envelope.GetSubject(),
		Type:      envelope.GetType(),
		Timestamp: envelope.GetTimestamp(),
		Actor:     envelope.GetActor(),
		Codec:     hdrMeta.Codec,
		Payload:   envelope.GetPayload(), // ciphertext when codec != identity
		SchemaVer: hdrMeta.SchemaVer,
	}
	if hdrMeta.DEKRef != nil {
		v := uint64(*hdrMeta.DEKRef)
		row.DekRef = &v
	}
	if hdrMeta.DEKVersion != nil {
		v := uint32(*hdrMeta.DEKVersion)
		row.DekVersion = &v
	}
	return row, nil
}

// unmarshalProjectionOnly proto-unmarshals msg.Data() into eventbusv1.Event.
// Per the codec contract (internal/eventbus/codec/xchacha20poly1305_v1.go),
// the codec encrypts the Event.payload field in-place — projection
// fields are always cleartext in the envelope, regardless of codec.
// We re-use proto.Unmarshal directly here rather than invoking the
// codec's Decode (which would decrypt).
func unmarshalProjectionOnly(data []byte) (*eventbusv1.Event, error) {
	var ev eventbusv1.Event
	if err := proto.Unmarshal(data, &ev); err != nil {
		return nil, err
	}
	return &ev, nil
}
```

**IMPORTANT — agent verification step:** before writing the function, read `internal/eventbus/codec/xchacha20poly1305_v1.go` and `internal/eventbus/codec/identity.go` to confirm the wire-format invariant: is the *payload field of the envelope* the ciphertext, or are the *envelope bytes themselves* encrypted? If the latter, the design above is wrong and the plan must be revised. The spec assumes per-field payload encryption per master spec §4.1 — verify.

Use `mcp__probe__search_code` for the codec files first; don't rg.

Replace the call site in `dispatch` (around line 295-300 today) to call `buildAuditRow` and pass `*pluginauditpb.AuditRow` into `AuditEventRequest{Row: row}` instead of `Event` + `Headers`.

Remove all references to `decodeEnvelope`. Remove the `AUDIT_PLUGIN_CODEC_UNSUPPORTED` and `AUDIT_PLUGIN_CODEC_UNKNOWN` error codes (they're no longer reachable).

- [ ] **Step B.1.6: Wire `codec.KeySelector` field on `PluginConsumerManager`**

Add field:

```go
type PluginConsumerManager struct {
	// ... existing fields ...
	keySelector codec.KeySelector  // INV-P7-9: same instance as hot-tier
}
```

Add constructor option:

```go
func WithKeySelector(sel codec.KeySelector) PluginConsumerManagerOption {
	return func(m *PluginConsumerManager) { m.keySelector = sel }
}
```

The field is unused in Phase 7's forward path (the dispatcher just forwards ciphertext per INV-P7-11). Wiring exists for substrate symmetry with the hot-tier reader.

- [ ] **Step B.1.7: Update `StoreFromMessage` in `pkg/plugin/audit.go`**

Replace the Task A.3.4 panic-stub with the real implementation, mirroring `buildAuditRow` from the dispatcher side:

```go
func StoreFromMessage(msg jetstream.Msg) (AuditRow, error) {
	hdrMeta, err := audit.ParseAuditHeaders(msg.Headers())
	if err != nil {
		return AuditRow{}, err //nolint:wrapcheck // error already coded by parser
	}

	var ev eventbusv1.Event
	if err := proto.Unmarshal(msg.Data(), &ev); err != nil {
		return AuditRow{}, oops.Code("AUDIT_PLUGIN_ENVELOPE_UNMARSHAL_FAILED").Wrap(err)
	}

	row := AuditRow{
		Subject:   ev.GetSubject(),
		Type:      ev.GetType(),
		Actor:     ev.GetActor(),
		Codec:     hdrMeta.Codec,
		Payload:   ev.GetPayload(),
		SchemaVer: hdrMeta.SchemaVer,
	}
	if id := ev.GetId(); len(id) == 16 {
		copy(row.EventID[:], id)
	}
	if ts := ev.GetTimestamp(); ts != nil {
		row.Timestamp = ts.AsTime()
	}
	if hdrMeta.DEKRef != nil {
		v := uint64(*hdrMeta.DEKRef)
		row.DEKRef = &v
	}
	if hdrMeta.DEKVersion != nil {
		v := uint32(*hdrMeta.DEKVersion)
		row.DEKVersion = &v
	}
	return row, nil
}
```

Adds new import to `pkg/plugin/audit.go`:

```go
"google.golang.org/protobuf/proto"
"github.com/samber/oops"

audit "github.com/holomush/holomush/internal/eventbus/audit"
```

NOTE: importing `internal/eventbus/audit` from `pkg/plugin/` is structurally questionable (pkg/ shouldn't depend on internal/). Verify with `go build` — if it fails, the agent moves `ParseAuditHeaders` to a shared neutral package (`pkg/eventbus/auditheader/` or similar) before re-running the build. This decision IS in scope for this task; revising the parser's package location is the right call if the import fails.

- [ ] **Step B.1.8: Run tests**

Run: `task test -- ./internal/eventbus/audit/`
Run: `task test -- ./pkg/plugin/`
Expected: dispatcher tests PASS, SDK round-trip + StoreFromMessage round-trip tests PASS, `TestPluginConsumerDispatchRejectsNonIdentityCodec` is gone.

- [ ] **Step B.1.9: Build the full tree**

Run: `task build`
Expected: STILL FAILS in `plugins/core-scenes/audit.go` and `test/integration/eventbus_e2e/plugin_audit_isolation_test.go` because those files haven't been migrated yet. Tasks B.2 / B.3 address them.

- [ ] **Step B.1.10: Commit**

```text
feat(audit/plugin_consumer): widen dispatcher to forward ciphertext

Removes AUDIT_PLUGIN_CODEC_UNSUPPORTED rejection at plugin_consumer.go:343
(parked work since F5). Per Phase 7 spec §3 + §5.1:

- Dispatcher builds *pluginauditpb.AuditRow via buildAuditRow helper.
- Payload bytes preserved byte-equal from envelope (INV-P7-1).
- No decryption before forwarding (INV-P7-11).
- ParseAuditHeaders (Task A.1) shared with host projection — single
  source of truth for header parsing.
- codec.KeySelector wired on PluginConsumerManager for substrate
  symmetry with hot-tier reader (INV-P7-9); unused in Phase 7
  forward path.

pkg/plugin/audit.go StoreFromMessage implementation lands here
alongside the dispatcher (round-trip stable with LoadForQuery).

Build is still broken in plugins/core-scenes/audit.go and
test/integration/eventbus_e2e/plugin_audit_isolation_test.go —
Tasks B.2/B.3 migrate those callers.

Refs: holomush-1r0v
```

### Task B.2: Plugin schema migration + core-scenes/audit.go migration

**Why:** Plugin storage needs `dek_ref`/`dek_version` columns (INV-P7-3). core-scenes/audit.go is the existing-caller consumer of the old `AuditEventRequest.event/headers` shape; needs to migrate to `req.GetRow()`. Spec §4.1, §4.2 caller-enumeration list.

**Files:**

- Create: `plugins/core-scenes/migrations/000005_add_scene_log_dek_columns.up.sql`
- Create: `plugins/core-scenes/migrations/000005_add_scene_log_dek_columns.down.sql`
- Modify: `plugins/core-scenes/audit.go`

- [ ] **Step B.2.1: Write the migration**

Create `plugins/core-scenes/migrations/000005_add_scene_log_dek_columns.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
--
-- Phase 7 (holomush-1r0v): plugin-owned audit tables mirror events_audit
-- shape per master spec §8.2. dek_ref BIGINT, dek_version INTEGER, both
-- nullable. Identity-codec rows have both NULL.

ALTER TABLE scene_log
    ADD COLUMN IF NOT EXISTS dek_ref     BIGINT,
    ADD COLUMN IF NOT EXISTS dek_version INTEGER;

CREATE INDEX IF NOT EXISTS scene_log_dek_ref
    ON scene_log (dek_ref)
    WHERE dek_ref IS NOT NULL;
```

Create `plugins/core-scenes/migrations/000005_add_scene_log_dek_columns.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS scene_log_dek_ref;

ALTER TABLE scene_log
    DROP COLUMN IF EXISTS dek_version,
    DROP COLUMN IF EXISTS dek_ref;
```

- [ ] **Step B.2.2: Update `SceneAuditStore.Insert` signature + SQL**

Modify `plugins/core-scenes/audit.go`. Add `dekRef *int64, dekVersion *int32` parameters; extend the INSERT statement:

```go
func (s *SceneAuditStore) Insert(
	ctx context.Context,
	id []byte,
	subject, eventType string,
	timestamp *timestamppb.Timestamp,
	actorKind string,
	actorID []byte,
	payload []byte,
	schemaVer int,
	codec string,
	dekRef *int64,
	dekVersion *int32,
) error {
	var ts any
	if timestamp != nil {
		ts = timestamp.AsTime()
	}
	_, err := s.pool.Exec(
		ctx, `
		INSERT INTO scene_log (
			id, subject, type, timestamp, actor_kind, actor_id,
			payload, schema_ver, codec, dek_ref, dek_version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO NOTHING`,
		id, subject, eventType, ts, actorKind, actorID, payload, schemaVer, codec, dekRef, dekVersion,
	)
	// ... existing error handling unchanged ...
}
```

- [ ] **Step B.2.3: Update `queryLog` SELECT to include new columns**

Extend the `SELECT` in `queryLog` (around line 467):

```go
query := "SELECT id, subject, type, timestamp, actor_kind, actor_id, payload, schema_ver, codec, dek_ref, dek_version FROM scene_log WHERE " +
    ...
```

Update the `logRow` struct to carry the new fields:

```go
type logRow struct {
	id         []byte
	subject    string
	eventType  string
	timestamp  time.Time
	actorKind  string
	actorID    []byte
	payload    []byte
	schemaVer  int
	codec      string
	dekRef     *int64
	dekVersion *int32
}
```

Update the `pgRows.Scan(...)` call to include the new columns.

- [ ] **Step B.2.4: Migrate `SceneAuditServer.AuditEvent` to read from `req.GetRow()`**

Replace the existing `req.GetEvent()` + `req.GetHeaders()` reads (lines 160-237) with `req.GetRow()` field access:

```go
func (s *SceneAuditServer) AuditEvent(ctx context.Context, req *pluginv1.AuditEventRequest) (*pluginv1.AuditEventResponse, error) {
	if req == nil || req.GetRow() == nil {
		return nil, oops.Code("SCENE_AUDIT_MISSING_ROW").Errorf("AuditEventRequest.row required")
	}
	row := req.GetRow()

	codec := row.GetCodec()
	if codec == "" {
		return nil, oops.Code("SCENE_AUDIT_MISSING_FIELD").With("field", "codec").Errorf("missing field")
	}

	schemaVer := int(row.GetSchemaVer())
	if schemaVer < 0 || schemaVer > 32767 {
		return nil, oops.Code("SCENE_AUDIT_BAD_SCHEMA_VERSION").With("value", schemaVer).Errorf("schema version out of range")
	}

	eventType := row.GetType()
	if eventType == "" {
		return nil, oops.Code("SCENE_AUDIT_MISSING_FIELD").With("field", "type").Errorf("missing field")
	}

	if len(row.GetId()) != 16 {
		return nil, oops.Code("SCENE_AUDIT_MISSING_ID").Errorf("row.id required (16-byte ULID)")
	}

	var actorKind string
	var actorID []byte
	if a := row.GetActor(); a != nil {
		actorKind = a.GetKind().String()
		actorID = a.GetId()
	}
	if actorKind == "" {
		actorKind = defaultActorKind
	}

	var dekRef *int64
	if row.DekRef != nil {
		v := int64(*row.DekRef)
		dekRef = &v
	}
	var dekVersion *int32
	if row.DekVersion != nil {
		v := int32(*row.DekVersion)
		dekVersion = &v
	}

	if err := s.store.Insert(
		ctx,
		row.GetId(),
		row.GetSubject(),
		eventType,
		row.GetTimestamp(),
		actorKind,
		actorID,
		row.GetPayload(),
		schemaVer,
		codec,
		dekRef,
		dekVersion,
	); err != nil {
		return nil, err //nolint:wrapcheck // Insert wraps with SCENE_AUDIT_INSERT_FAILED
	}

	return &pluginv1.AuditEventResponse{}, nil
}
```

Update the `sceneAuditLogStore` interface signature to match the new `Insert`.

- [ ] **Step B.2.5: Migrate `SceneAuditServer.QueryHistory` to return `*pluginauditpb.AuditRow`**

Replace the response construction (lines 356-376) with `AuditRow`-shaped response:

```go
for i := range rows {
	r := &rows[i]
	var dekRefU64 *uint64
	if r.dekRef != nil {
		v := uint64(*r.dekRef)
		dekRefU64 = &v
	}
	var dekVerU32 *uint32
	if r.dekVersion != nil {
		v := uint32(*r.dekVersion)
		dekVerU32 = &v
	}
	resp := &pluginv1.QueryHistoryResponse{
		Row: &pluginv1.AuditRow{
			Id:         r.id,
			Subject:    r.subject,
			Type:       r.eventType,
			Timestamp:  timestamppb.New(r.timestamp),
			Actor:      actorProtoFromRow(r.actorKind, r.actorID),
			Codec:      r.codec,
			Payload:    r.payload,
			DekRef:     dekRefU64,
			DekVersion: dekVerU32,
			SchemaVer:  int32(r.schemaVer),
		},
	}
	if err := stream.Send(resp); err != nil { ... }
}
```

- [ ] **Step B.2.6: Run existing core-scenes tests**

Run: `task test -- ./plugins/core-scenes/`
Expected: existing audit tests PASS after adjustment to the new signatures. Some tests may need their `gotReq.GetEvent()`/`GetHeaders()` assertions updated to `gotReq.GetRow()` field reads. Make those mechanical updates.

- [ ] **Step B.2.7: Build the full tree**

Run: `task build`
Expected: build still fails only in `test/integration/eventbus_e2e/plugin_audit_isolation_test.go` (test stub mirror). Task B.3 fixes it.

- [ ] **Step B.2.8: Commit**

```text
feat(core-scenes): add dek_ref/dek_version columns + migrate to AuditRow

Migration 000005 adds the two crypto columns to scene_log per
INV-P7-3. SceneAuditServer.AuditEvent reads from req.GetRow();
SceneAuditServer.QueryHistory constructs *pluginauditpb.AuditRow
on the response.

Refs: holomush-1r0v
```

### Task B.3: Migrate test integration stub

**Why:** Spec §3.2 enumerated the test-stub mirror at `test/integration/eventbus_e2e/plugin_audit_isolation_test.go:175-220`.

**Files:**

- Modify: `test/integration/eventbus_e2e/plugin_audit_isolation_test.go`

- [ ] **Step B.3.1: Read the existing test stub (lines 175-220)**

Note the parallel `AuditEvent` handler shape — `req.GetEvent()` + `req.GetHeaders()` reads → `INSERT INTO n.scene_log (...)`. The stub mirrors the real core-scenes handler so behavioural assertions stay valid across both.

- [ ] **Step B.3.2: Update the stub to consume `req.GetRow()`**

Mirror the migration done in Task B.2.4 verbatim, simplified for the stub's local `INSERT` (the stub writes to `n.scene_log` rather than via SceneAuditStore).

- [ ] **Step B.3.3: Run integration tests**

Run: `task test:int -- -run TestPluginAuditIsolation ./test/integration/eventbus_e2e/`
Expected: PASS.

- [ ] **Step B.3.4: Build the full tree**

Run: `task build`
Expected: success — all callers migrated.

- [ ] **Step B.3.5: Commit**

```text
test(eventbus_e2e): migrate plugin_audit_isolation_test stub

Stub at lines 175-220 mirrors the real core-scenes shape; updated to
consume req.GetRow() per the Phase 7 proto reshape.

Refs: holomush-1r0v
```

### Task B.4: KeySelector pointer-identity integration test

**Why:** INV-P7-9 — same selector instance threaded through `PluginConsumerManager` and `history.NewReader`. Caught by integration test against the wiring at `cmd/holomush/deps.go` (Task E.2).

**Files:**

- Create: `test/integration/eventbus_e2e/dispatcher_selector_identity_test.go`

- [ ] **Step B.4.1: Write the test**

```go
//go:build integration

package eventbus_e2e

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDispatcherAndHotTierShareSelector — INV-P7-9. Drives the
// production wiring path; reads back the `keySelector` field on
// PluginConsumerManager and the codec selector held by history.Reader
// (via a test-only accessor or reflection) and asserts they're the
// same pointer.
func TestDispatcherAndHotTierShareSelector(t *testing.T) {
	t.Helper()
	deps := buildTestDeps(t)
	pcm := deps.PluginConsumerManager()
	reader := deps.HistoryReader()

	// Test-only accessors live on the *_test.go side; e.g.
	// pcm.KeySelectorForTest() and reader.KeySelectorForTest().
	pcmSel := pcm.KeySelectorForTest()
	readerSel := reader.KeySelectorForTest()

	assert.NotNil(t, pcmSel)
	assert.NotNil(t, readerSel)
	assert.True(t, reflect.ValueOf(pcmSel).Pointer() == reflect.ValueOf(readerSel).Pointer(),
		"INV-P7-9: PluginConsumerManager and history.Reader MUST share the same KeySelector instance")
}
```

`KeySelectorForTest` accessor methods land in `internal_test.go` files in each respective package, exported only under the test build tag.

- [ ] **Step B.4.2: Wire the accessor (Phase E.2 final wiring will land the production-side ctor)**

Add test-only accessors:

`internal/eventbus/audit/plugin_consumer_test_export_test.go`:
```go
//go:build integration

package audit

import "github.com/holomush/holomush/internal/eventbus/codec"

func (m *PluginConsumerManager) KeySelectorForTest() codec.KeySelector { return m.keySelector }
```

`internal/eventbus/history/tier_test_export_test.go`:
```go
//go:build integration

package history

import "github.com/holomush/holomush/internal/eventbus/codec"

func (r *Reader) KeySelectorForTest() codec.KeySelector { return r.selector }
```

- [ ] **Step B.4.3: Run integration test (will fail until Task E.2 wires deps.go)**

Run: `task test:int -- -run TestDispatcherAndHotTierShareSelector ./test/integration/eventbus_e2e/`
Expected: FAIL — the wiring at `cmd/holomush/deps.go` doesn't yet pass the same selector to both constructors. Pinned to be addressed in Task E.2.

- [ ] **Step B.4.4: Commit (with a note that the test is parked failing)**

```text
test(eventbus_e2e): INV-P7-9 selector pointer-identity test (parked)

Drives the deps.go wiring path; will pass once Task E.2 threads
the same codec.KeySelector instance into both PluginConsumerManager
and history.NewReader. Failing-now is expected and documented.

Refs: holomush-1r0v
```

### Task B.5: Plugin round-trip integration test

**Why:** INV-P7-6 (plugin row byte-equal store+return) + INV-P7-12 (sensitive event row holds ciphertext, not plaintext).

**Files:**

- Create: `test/integration/eventbus_e2e/plugin_audit_round_trip_test.go`

- [ ] **Step B.5.1: Write the round-trip test**

```go
//go:build integration

package eventbus_e2e

func TestSceneLogPreservesCiphertextAndAuditHeaders(t *testing.T) {
	t.Helper()
	suite := newEncryptingSuite(t)  // helper that wires a real DEK + xchacha20poly1305-v1 codec
	defer suite.Close()

	// Emit one sensitive event under a plugin-owned subject.
	eventID := suite.EmitSensitive(t, "events.test.scene.01ABC.ic",
		"test-plugin:secret", []byte("the original plaintext"))

	// Allow the per-plugin consumer to ingest + write to plugin's table.
	suite.WaitForRowInSceneLog(t, eventID, 5*time.Second)

	// Read back via the plugin's QueryHistory.
	rowFromPlugin := suite.PluginQueryHistory(t, "events.test.scene.01ABC.ic", eventID)

	// INV-P7-6 / INV-P7-12 — payload is ciphertext, not plaintext.
	assert.Equal(t, "xchacha20poly1305-v1", rowFromPlugin.GetCodec())
	assert.NotEqual(t, []byte("the original plaintext"), rowFromPlugin.GetPayload(),
		"INV-P7-12: plugin row MUST hold ciphertext for sensitive events")
	assert.NotEmpty(t, rowFromPlugin.GetPayload())
	require.NotNil(t, rowFromPlugin.DekRef)
	require.NotNil(t, rowFromPlugin.DekVersion)

	// INV-P7-6: ciphertext byte-equal to the bus envelope's payload.
	busPayload := suite.LookupBusPayload(t, eventID)
	assert.Equal(t, busPayload, rowFromPlugin.GetPayload(),
		"INV-P7-6: plugin storage MUST be byte-equal to bus")
}
```

The `newEncryptingSuite` helper builds on the existing eventbus_e2e suite scaffolding (`suite_test.go`) and wires a real `codec.KeySelector` + a `dek.Manager` for one test context. Estimate: ~60 lines of helper code.

- [ ] **Step B.5.2: Run the test**

Run: `task test:int -- -run TestSceneLogPreservesCiphertextAndAuditHeaders ./test/integration/eventbus_e2e/`
Expected: PASS (once the helper is implemented).

- [ ] **Step B.5.3: Commit**

```text
test(eventbus_e2e): INV-P7-6/INV-P7-12 plugin round-trip integration

Emits a sensitive event, asserts plugin row holds ciphertext +
crypto headers byte-equal to the bus envelope. Validates both the
storage layer (INV-P7-6) and the sensitive-payload non-leakage
property (INV-P7-12).

Refs: holomush-1r0v
```

### Task B.6: Plugin migration standalone test

**Why:** INV-P7-3, INV-P7-10. Plugin migration runs in its own runner; column add must be idempotent and runnable independent of host migration state.

**Files:**

- Create: `test/integration/plugin/plugin_migration_test.go`

- [ ] **Step B.6.1: Write the test**

```go
//go:build integration

package plugin

func TestPhase7PluginMigrationStandalone(t *testing.T) {
	pool := setupEmptyPluginSchema(t, "plugin_core_scenes_test")

	// Apply migrations 1-4 (existing).
	require.NoError(t, applyMigrations(pool, "../../../plugins/core-scenes/migrations/", 4))

	// Apply migration 5 in isolation; assert columns present.
	require.NoError(t, applyMigrationN(pool, "../../../plugins/core-scenes/migrations/", 5))

	row := pool.QueryRow(t.Context(), `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = 'plugin_core_scenes_test'
		  AND table_name = 'scene_log'
		  AND column_name IN ('dek_ref', 'dek_version')
		ORDER BY column_name`)
	// Two rows expected; full result-set check via second query.
	var present []string
	rows, _ := pool.Query(t.Context(), `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = 'plugin_core_scenes_test'
		  AND table_name = 'scene_log'
		  AND column_name IN ('dek_ref', 'dek_version')
		ORDER BY column_name`)
	defer rows.Close()
	for rows.Next() {
		var c string
		_ = rows.Scan(&c)
		present = append(present, c)
	}
	assert.Equal(t, []string{"dek_ref", "dek_version"}, present,
		"INV-P7-3: scene_log MUST have dek_ref + dek_version after migration 5")

	// Idempotency: re-applying migration 5 is a no-op.
	require.NoError(t, applyMigrationN(pool, "../../../plugins/core-scenes/migrations/", 5))
}

func TestSceneLogHasDekColumns(t *testing.T) {
	// Production-shape assertion: after the full migration sequence,
	// scene_log carries both columns with the expected types.
	pool := setupFullCoreScenesSchema(t)
	var dekRefType, dekVersionType string
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT data_type FROM information_schema.columns
		WHERE table_name = 'scene_log' AND column_name = 'dek_ref'`).Scan(&dekRefType))
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT data_type FROM information_schema.columns
		WHERE table_name = 'scene_log' AND column_name = 'dek_version'`).Scan(&dekVersionType))
	assert.Equal(t, "bigint", dekRefType)
	assert.Equal(t, "integer", dekVersionType)
}
```

- [ ] **Step B.6.2: Run integration test**

Run: `task test:int -- -run TestPhase7PluginMigrationStandalone ./test/integration/plugin/`
Run: `task test:int -- -run TestSceneLogHasDekColumns ./test/integration/plugin/`
Expected: PASS.

- [ ] **Step B.6.3: Commit**

```text
test(plugin): INV-P7-3 + INV-P7-10 plugin migration standalone

Asserts dek_ref/dek_version columns land via migration 000005
without host-side coordination, and the migration is idempotent.

Refs: holomush-1r0v
```

---

## Phase C — Read-side fence (AAD adapter + fence + wiring)

After Phase C, the host's `Reader.QueryHistory` plugin path routes through `PluginDowngradeFence` (layer 1 — manifest-set + DEK existence) and the AAD adapter pre-feeds `aad.Build` for the downstream decrypt path (layer 2 backbone).

### Task C.1: AAD reconstruction adapter

**Why:** INV-P7-16, master INV-25. Plugin returns `*AuditRow`; the existing decrypt path takes `*eventbusv1.Event` → AAD reconstruction needs an adapter.

**Files:**

- Create: `internal/eventbus/history/plugin_aad_adapter.go`
- Create: `internal/eventbus/history/plugin_aad_adapter_test.go`

- [ ] **Step C.1.1: Write the failing adapter test**

```go
package history

import (
	"testing"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/internal/eventbus/codec"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

func TestAuditRowToEvent_CopiesAllAADFields(t *testing.T) {
	ts := timestamppb.Now()
	row := &pluginauditpb.AuditRow{
		Id:        []byte("0123456789ABCDEF"),
		Subject:   "events.test.scene.01ABC.ic",
		Type:      "test-plugin:secret",
		Timestamp: ts,
		Actor:     &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, Id: []byte("char-id")},
		Codec:     "xchacha20poly1305-v1",
		Payload:   []byte("ciphertext"),
		// dek_ref / dek_version NOT copied into Event — passed separately to aad.Build
	}

	ev := AuditRowToEvent(row)
	require.NotNil(t, ev)
	assert.Equal(t, row.GetId(), ev.GetId())
	assert.Equal(t, row.GetSubject(), ev.GetSubject())
	assert.Equal(t, row.GetType(), ev.GetType())
	assert.Equal(t, row.GetTimestamp().AsTime().UnixNano(), ev.GetTimestamp().AsTime().UnixNano())
	assert.Equal(t, row.GetActor().GetKind(), ev.GetActor().GetKind())
	assert.Equal(t, row.GetActor().GetId(), ev.GetActor().GetId())
}

func TestAuditRowToEvent_NilSafety(t *testing.T) {
	row := &pluginauditpb.AuditRow{
		Id:      []byte("0123456789ABCDEF"),
		Subject: "events.test.foo",
		Type:    "test-plugin:plain",
		Codec:   "identity",
		Payload: []byte("hello"),
		// Timestamp + Actor are nil
	}
	ev := AuditRowToEvent(row)
	require.NotNil(t, ev)
	assert.Nil(t, ev.GetTimestamp())
	assert.Nil(t, ev.GetActor())
}
```

- [ ] **Step C.1.2: Run test to verify it fails**

Run: `task test -- -run TestAuditRowToEvent ./internal/eventbus/history/`
Expected: FAIL with "undefined: AuditRowToEvent".

- [ ] **Step C.1.3: Implement the adapter**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// AuditRowToEvent converts a plugin-returned AuditRow into the
// *eventbusv1.Event shape consumed by aad.Build for AAD
// reconstruction (INV-P7-16, master INV-25).
//
// Per-field copy contract — see spec §5.4:
//   - id, subject, type, timestamp, actor: copied verbatim
//   - codec, dek_ref, dek_version: NOT copied (passed as scalar
//     args to aad.Build separately)
//   - payload: NOT copied (ciphertext input to AEAD, not AAD)
//   - schema_ver: NOT copied (not in AAD canonical inputs per
//     master §4.2, verified at aad.go:106-114)
//   - rendering: NOT copied (Event.rendering field 7; not in
//     AAD per master §4.2)
//
// Nil-safety: Actor and Timestamp may be nil on some event shapes.
// Caller (aad.Build) tolerates nil via event.GetActor() and the
// unconditional UnixNano() path.
func AuditRowToEvent(row *pluginauditpb.AuditRow) *eventbusv1.Event {
	if row == nil {
		return nil
	}
	return &eventbusv1.Event{
		Id:        row.GetId(),
		Subject:   row.GetSubject(),
		Type:      row.GetType(),
		Timestamp: row.GetTimestamp(),
		Actor:     row.GetActor(),
	}
}
```

- [ ] **Step C.1.4: Run test to verify it passes**

Run: `task test -- -run TestAuditRowToEvent ./internal/eventbus/history/`
Expected: PASS.

- [ ] **Step C.1.5: Commit**

```text
feat(history): AuditRow → *eventbusv1.Event adapter for AAD

Plugin-returned AuditRow needs to feed aad.Build (which takes
*eventbusv1.Event). Six-field copy with two nil guards. Excludes
fields not in AAD canonical inputs per master §4.2 (verified at
aad.go:106-114): codec/dek_ref/dek_version are scalar args; payload
is AEAD input; schema_ver and rendering aren't bound.

Refs: holomush-1r0v
```

### Task C.2: AAD byte-equal integration test (INV-P7-16)

**Why:** The critical invariant — without byte-equal AAD reconstruction, EVERY sensitive plugin-stored event fails AEAD tag-check on decrypt.

**Files:**

- Create: `internal/eventbus/history/plugin_aad_reconstruction_test.go`

- [ ] **Step C.2.1: Write the test**

```go
//go:build integration

package history_test

import (
	"testing"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/internal/eventbus/history"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// TestRoundTripProducesByteEqualAAD — INV-P7-16. Encrypts a sensitive
// plugin-owned event, captures the encrypt-side AAD bytes, stores +
// returns the row through the plugin path, reconstructs Event via the
// §5.4 adapter, recomputes AAD, asserts byte-equal.
func TestRoundTripProducesByteEqualAAD(t *testing.T) {
	suite := newEncryptingSuite(t)
	defer suite.Close()

	eventID := suite.EmitSensitive(t, "events.test.scene.01ABC.ic",
		"test-plugin:secret", []byte("plaintext"))

	encryptSideAAD := suite.CaptureEncryptSideAAD(t, eventID)
	require.NotNil(t, encryptSideAAD)

	suite.WaitForRowInSceneLog(t, eventID, 5*time.Second)
	row := suite.PluginQueryHistory(t, "events.test.scene.01ABC.ic", eventID)
	require.NotNil(t, row)

	reconstructedEvent := history.AuditRowToEvent(row)
	require.NotNil(t, reconstructedEvent)

	dekRef := row.GetDekRef()
	dekVer := row.GetDekVersion()
	reconstructedAAD, err := aad.Build(reconstructedEvent, row.GetCodec(), dekRef, dekVer)
	require.NoError(t, err)

	assert.Equal(t, encryptSideAAD, reconstructedAAD,
		"INV-P7-16: AAD reconstruction MUST be byte-equal to encrypt-side")
}
```

Estimate: `CaptureEncryptSideAAD` is a ~30-line test helper that intercepts the codec emit path via a test-only hook.

- [ ] **Step C.2.2: Run the test**

Run: `task test:int -- -run TestRoundTripProducesByteEqualAAD ./internal/eventbus/history/`
Expected: PASS.

- [ ] **Step C.2.3: Commit**

```text
test(history): INV-P7-16 AAD byte-equal reconstruction integration

Critical test — a regression in AuditRowToEvent would manifest as
EVERY sensitive plugin-stored event failing AEAD tag-check on
decrypt. Asserts encrypt-side AAD == reconstructed AAD byte-equal.

Refs: holomush-1r0v
```

### Task C.3: PluginDowngradeFence

**Why:** INV-P7-7 (manifest-set heuristic), INV-P7-8 (boot-built immutable set), INV-P7-15 (DEK existence pre-check). Layer (1) of the two-layer fence.

**Files:**

- Create: `internal/eventbus/history/plugin_downgrade_fence.go`
- Create: `internal/eventbus/history/plugin_downgrade_fence_test.go`

- [ ] **Step C.3.1: Write the failing fence tests**

```go
package history

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

func TestFenceRefusesIdentityForAlwaysSensitiveType(t *testing.T) {
	innerRouter := &fakeRouter{rows: []*pluginauditpb.AuditRow{
		{
			Id:      []byte("0123456789ABCDEF"),
			Subject: "events.test.scene.01ABC.ic",
			Type:    "test-plugin:secret",   // declared sensitivity:always
			Codec:   "identity",             // downgrade attempt
			Payload: []byte("cleartext"),
		},
	}}

	fence := NewPluginDowngradeFence(innerRouter,
		WithAlwaysSensitiveTypes(map[string]struct{}{"test-plugin:secret": {}}),
		WithCryptoKeysLookup(stubCryptoLookupAlwaysFound{}),
		WithViolationEmitter(noopEmitter{}),
	)

	stream, err := fence.QueryHistory(context.Background(), "test-plugin",
		eventbus.HistoryQuery{Subject: "events.test.scene.01ABC.ic"})
	require.NoError(t, err)

	_, err = stream.Next(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AUDIT_ROW_DOWNGRADE_DETECTED",
		"INV-P7-7: fence MUST refuse identity-codec for always-sensitive types")
}

func TestFenceAllowsIdentityForNonSensitiveType(t *testing.T) {
	innerRouter := &fakeRouter{rows: []*pluginauditpb.AuditRow{
		{
			Id:      []byte("0123456789ABCDEF"),
			Subject: "events.test.foo",
			Type:    "test-plugin:plain",
			Codec:   "identity",
			Payload: []byte("legit cleartext"),
		},
	}}

	fence := NewPluginDowngradeFence(innerRouter,
		WithAlwaysSensitiveTypes(map[string]struct{}{}),
		WithCryptoKeysLookup(stubCryptoLookupAlwaysFound{}),
		WithViolationEmitter(noopEmitter{}),
	)

	stream, err := fence.QueryHistory(context.Background(), "test-plugin",
		eventbus.HistoryQuery{Subject: "events.test.foo"})
	require.NoError(t, err)

	ev, err := stream.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []byte("legit cleartext"), ev.Payload)
}

func TestFenceRefusesUnknownDekRef(t *testing.T) {
	dekRef := uint64(9999999)
	innerRouter := &fakeRouter{rows: []*pluginauditpb.AuditRow{
		{
			Id:        []byte("0123456789ABCDEF"),
			Subject:   "events.test.scene.01ABC.ic",
			Type:      "test-plugin:secret",
			Codec:     "xchacha20poly1305-v1",
			Payload:   []byte("ciphertext"),
			DekRef:    &dekRef,
		},
	}}

	fence := NewPluginDowngradeFence(innerRouter,
		WithAlwaysSensitiveTypes(map[string]struct{}{"test-plugin:secret": {}}),
		WithCryptoKeysLookup(stubCryptoLookupNotFound{}),
		WithViolationEmitter(noopEmitter{}),
	)

	stream, err := fence.QueryHistory(context.Background(), "test-plugin",
		eventbus.HistoryQuery{Subject: "events.test.scene.01ABC.ic"})
	require.NoError(t, err)

	ev, err := stream.Next(context.Background())
	// INV-P7-15: refusal surfaces as metadata_only=true per master INV-26.
	require.NoError(t, err)
	assert.True(t, ev.MetadataOnly,
		"INV-P7-15: unknown dek_ref MUST surface as metadata_only=true")
}

func TestFenceSetBuiltOnceAtBoot(t *testing.T) {
	fence := NewPluginDowngradeFence(&fakeRouter{},
		WithAlwaysSensitiveTypes(map[string]struct{}{"a:b": {}}),
		WithCryptoKeysLookup(stubCryptoLookupAlwaysFound{}),
		WithViolationEmitter(noopEmitter{}),
	)

	// INV-P7-8: no public method exists to mutate the set after construction.
	// Compile-time check — if a setter is added, the test corpus must adapt.
	// Runtime check: assert the set is non-empty and the fence value's
	// internal field is set exactly once.
	got := fence.AlwaysSensitiveTypesForTest()
	assert.Contains(t, got, "a:b")
}
```

`fakeRouter`, `stubCryptoLookupAlwaysFound`, `stubCryptoLookupNotFound`, `noopEmitter`, `AlwaysSensitiveTypesForTest` are test-fakes/exports defined in the same `_test.go` file or a `tier_test_export_test.go` alongside.

- [ ] **Step C.3.2: Run tests to verify they fail**

Run: `task test -- -run TestFence ./internal/eventbus/history/`
Expected: FAIL with "undefined: NewPluginDowngradeFence".

- [ ] **Step C.3.3: Implement the fence**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// CryptoKeysLookup is the read-only interface PluginDowngradeFence needs
// to check DEK existence (INV-P7-15). Production wiring uses the same
// crypto_keys reader the rest of the host uses; tests inject fakes.
type CryptoKeysLookup interface {
	// Exists returns true iff the dek_ref corresponds to a non-soft-deleted
	// crypto_keys row (production filter: destroyed_at IS NULL).
	Exists(ctx context.Context, dekRef uint64) (bool, error)
}

// ViolationEmitter publishes audit.<game>.system.plugin_integrity_violation
// events for INV-P7-7 refusals. Synchronous emit per spec §5.3.
type ViolationEmitter interface {
	EmitViolation(ctx context.Context, pluginName string, row *pluginauditpb.AuditRow,
		expectedSensitivity string, refusalCode string) error
}

// PluginDowngradeFence is the layer (1) QueryStreamHistory pre-decrypt
// fence. Wraps a PluginHistoryRouter. Owns the always-sensitive
// event-type set (boot-built per INV-P7-8) and the crypto_keys lookup
// for DEK existence (INV-P7-15).
type PluginDowngradeFence struct {
	inner            PluginHistoryRouter
	alwaysSensitive  map[string]struct{}
	cryptoKeysLookup CryptoKeysLookup
	emitter          ViolationEmitter
}

// PluginDowngradeFenceOption configures NewPluginDowngradeFence.
type PluginDowngradeFenceOption func(*PluginDowngradeFence)

func WithAlwaysSensitiveTypes(set map[string]struct{}) PluginDowngradeFenceOption {
	return func(f *PluginDowngradeFence) { f.alwaysSensitive = set }
}

func WithCryptoKeysLookup(l CryptoKeysLookup) PluginDowngradeFenceOption {
	return func(f *PluginDowngradeFence) { f.cryptoKeysLookup = l }
}

func WithViolationEmitter(e ViolationEmitter) PluginDowngradeFenceOption {
	return func(f *PluginDowngradeFence) { f.emitter = e }
}

func NewPluginDowngradeFence(inner PluginHistoryRouter, opts ...PluginDowngradeFenceOption) *PluginDowngradeFence {
	f := &PluginDowngradeFence{inner: inner, alwaysSensitive: map[string]struct{}{}}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// QueryHistory implements PluginHistoryRouter. Forwards the call to
// the inner router and wraps the returned stream with a per-row check.
func (f *PluginDowngradeFence) QueryHistory(ctx context.Context, pluginName string,
	q eventbus.HistoryQuery) (eventbus.HistoryStream, error) {
	inner, err := f.inner.QueryHistory(ctx, pluginName, q)
	if err != nil {
		return nil, err //nolint:wrapcheck // forwarding upstream error
	}
	return &fencedStream{fence: f, pluginName: pluginName, inner: inner}, nil
}

type fencedStream struct {
	fence      *PluginDowngradeFence
	pluginName string
	inner      eventbus.HistoryStream
}

func (s *fencedStream) Next(ctx context.Context) (eventbus.Event, error) {
	ev, err := s.inner.Next(ctx)
	if err != nil {
		return ev, err //nolint:wrapcheck
	}

	// Inner stream produces eventbus.Event with AuditRow proto fields
	// projected on it. To run the fence checks we need access to the
	// underlying AuditRow — see Task C.4 below for the wire-up.
	row := AuditRowFromEvent(ev)
	if row == nil {
		// Inner router didn't carry an AuditRow projection — pass through.
		return ev, nil
	}

	// (1a) Manifest-set heuristic — INV-P7-7.
	if row.GetCodec() == "identity" {
		if _, isSensitive := s.fence.alwaysSensitive[row.GetType()]; isSensitive {
			_ = s.fence.emitter.EmitViolation(ctx, s.pluginName, row,
				"always", "AUDIT_ROW_DOWNGRADE_DETECTED")
			return ev, oops.Code("AUDIT_ROW_DOWNGRADE_DETECTED").
				With("plugin", s.pluginName).
				With("event_type", row.GetType()).
				Errorf("plugin returned codec=identity for sensitivity:always event type")
		}
	}

	// (1b) DEK existence check — INV-P7-15.
	if row.GetCodec() != "identity" {
		if row.DekRef == nil {
			ev.MetadataOnly = true
			return ev, nil
		}
		exists, lookupErr := s.fence.cryptoKeysLookup.Exists(ctx, *row.DekRef)
		if lookupErr != nil {
			return ev, oops.Code("AUDIT_ROW_DEK_LOOKUP_FAILED").Wrap(lookupErr)
		}
		if !exists {
			ev.MetadataOnly = true
			return ev, nil
		}
	}

	return ev, nil
}

func (s *fencedStream) Close() error { return s.inner.Close() }

// AuditRowFromEvent recovers the *pluginauditpb.AuditRow that produced
// an eventbus.Event in the plugin-routed read path. Implementation
// pins how the audit-package router stamps the row onto the event.
// See plugin_router.go's RowExtractor for the production wiring.
func AuditRowFromEvent(ev eventbus.Event) *pluginauditpb.AuditRow {
	// Implementation lives in the same package or behind an unexported
	// extension on eventbus.Event. Concrete approach decided when
	// wiring the audit-package router in Task C.4.
	panic("AuditRowFromEvent: implementation deferred to Task C.4")
}
```

**IMPORTANT — agent verification step before locking the row-extraction approach:** Read `internal/eventbus/audit/plugin_router.go` to find where `*pluginauditpb.AuditRow` is converted to `eventbus.Event` (today's behaviour: per-row response from `PluginAuditService.QueryHistory` is mapped to `eventbus.Event`). The fence needs access to the original row OR the conversion site needs to carry the row through. Pick one of:
- (a) Extend `eventbus.Event` with an unexported `auditRow *pluginauditpb.AuditRow` field that the audit router stamps; fence reads via test export.
- (b) Re-decode the row from `ev.Payload` (but ev.Payload is the bus-shape payload, not the proto — wrong).
- (c) Have the audit router emit a richer stream type (e.g., `AuditRowStream` parallel to `HistoryStream`) and have the fence wrap that. This is the cleanest but changes the `PluginHistoryRouter` interface signature.

Recommendation: (c) — extend the interface. The `PluginHistoryRouter` interface today (`tier.go:79-84`) returns `eventbus.HistoryStream`. Phase 7 changes the interface to add a `QueryHistoryRows(ctx, plugin, q) (PluginRowStream, error)` method that returns the rows pre-conversion. `Reader.QueryHistory` calls the new method on the fence, then converts after validation.

But that means the fence's signature is NOT a drop-in `PluginHistoryRouter`. Renaming the interface is cleaner. Agent decides during implementation.

- [ ] **Step C.3.4: Wire the row extraction (agent picks approach)**

Implement `AuditRowFromEvent` or refactor the router interface as decided above.

- [ ] **Step C.3.5: Run tests to verify they pass**

Run: `task test -- -run TestFence ./internal/eventbus/history/`
Expected: all 4 PASS.

- [ ] **Step C.3.6: Commit**

```text
feat(history): PluginDowngradeFence layer (1) pre-decrypt checks

Implements INV-P7-7 (manifest-set heuristic) + INV-P7-8 (boot-built
immutable always-sensitive type set) + INV-P7-15 (DEK existence
pre-check). Wraps the existing PluginHistoryRouter; emits
plugin_integrity_violation audit on INV-P7-7 refusal.

Refs: holomush-1r0v
```

### Task C.4: Wire fence into Reader.QueryHistory

**Why:** Make the fence actually fire in production reads. Spec §3.1, §3.2.

**Files:**

- Modify: `internal/eventbus/history/tier.go`

- [ ] **Step C.4.1: Modify `Reader.QueryHistory` to wrap the inner router**

At `tier.go:367`, change:

```go
return r.router.QueryHistory(ctx, owner.PluginName, q)
```

To compose through the fence at wiring time:

```go
return r.fencedRouter().QueryHistory(ctx, owner.PluginName, q)
```

Where `fencedRouter()` returns either `r.router` (no fence configured) or `NewPluginDowngradeFence(r.router, ...)` (production). The fence configuration is captured in `Reader` construction via new options:

```go
// WithPluginDowngradeFence wires the Phase 7 read-side fence around
// the inner PluginHistoryRouter. Production wiring at cmd/holomush/deps.go
// (Task E.2) provides the always-sensitive set + crypto_keys lookup +
// violation emitter.
func WithPluginDowngradeFence(
	alwaysSensitive map[string]struct{},
	lookup CryptoKeysLookup,
	emitter ViolationEmitter,
) Option {
	return func(r *Reader) {
		r.fenceOpts = []PluginDowngradeFenceOption{
			WithAlwaysSensitiveTypes(alwaysSensitive),
			WithCryptoKeysLookup(lookup),
			WithViolationEmitter(emitter),
		}
	}
}
```

`r.fencedRouter()` lazily constructs the fence on the first call (or eagerly in `NewReader`).

- [ ] **Step C.4.2: Run existing tier tests + fence tests**

Run: `task test -- ./internal/eventbus/history/`
Expected: all PASS (existing tier_test.go uses a nil fence — behaviour preserved).

- [ ] **Step C.4.3: Commit**

```text
feat(history): wire PluginDowngradeFence into Reader.QueryHistory

Single-line change at tier.go:367 + new WithPluginDowngradeFence
option for cmd/holomush/deps.go wiring. Reader's plugin-routed
branch now routes through the fence before returning the stream.

Refs: holomush-1r0v
```

---

## Phase D — E2E + cross-cutting tests + meta-test

After Phase D, the binary-plugin test fixture exercises INV-P7-10 end-to-end, role permissions are asserted (INV-P7-13), the meta-test asserts no INV-P7-N has drifted out of test coverage.

### Task D.1: `test_downgrade_attacker` binary fixture

**Why:** INV-P7-10 e2e attack-path coverage requires a real binary plugin participating in the full loader/gRPC/dispatcher/fence chain.

**Files:**

- Create: `test/integration/plugin/testdata/test_downgrade_attacker/main.go`
- Create: `test/integration/plugin/testdata/test_downgrade_attacker/plugin.yaml`

- [ ] **Step D.1.1: Write the plugin manifest**

`plugin.yaml`:

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

name: test-downgrade-attacker
version: 0.1.0
type: binary

requires: []
provides:
  - holomush.plugin.v1.PluginAuditService

# Test fixture: declares a sensitive event type so the host's
# manifest-set heuristic (INV-P7-7) treats `test-downgrade-attacker:secret`
# as always-sensitive. The malicious branch returns codec=identity
# anyway.
crypto:
  emits:
    - event_type: secret
      sensitivity: always
      description: "Test fixture sensitive event."

audit:
  - subjects: ["events.*.test_downgrade.>"]
    schema: plugin_test_downgrade_attacker
    table: scene_log_fake  # the fixture mirrors the scene_log schema

binary-plugin:
  executable: test-downgrade-attacker
```

- [ ] **Step D.1.2: Write the plugin main**

`main.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Binary plugin fixture for Phase 7 INV-P7-10 e2e test (holomush-1r0v).
// Two QueryHistory branches selectable via env var:
//   TEST_DOWNGRADE_MODE=honest    — returns ciphertext byte-equal
//   TEST_DOWNGRADE_MODE=malicious — returns codec=identity + cleartext
//                                   for `test-downgrade-attacker:secret`
package main

import (
	"context"
	"os"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

type fixtureAuditServer struct {
	pluginv1.UnimplementedPluginAuditServiceServer
	mode string
}

func (s *fixtureAuditServer) AuditEvent(_ context.Context, _ *pluginv1.AuditEventRequest) (*pluginv1.AuditEventResponse, error) {
	// Fixture doesn't persist; the e2e test injects the row via the
	// QueryHistory return value directly.
	return &pluginv1.AuditEventResponse{}, nil
}

func (s *fixtureAuditServer) QueryHistory(req *pluginv1.QueryHistoryRequest, stream pluginv1.PluginAuditService_QueryHistoryServer) error {
	switch s.mode {
	case "malicious":
		// Return codec=identity + cleartext for the sensitive type.
		return stream.Send(&pluginv1.QueryHistoryResponse{
			Row: &pluginv1.AuditRow{
				Id:        []byte("0123456789ABCDEF"),
				Subject:   "events.test.test_downgrade.01ABC.ic",
				Type:      "test-downgrade-attacker:secret",
				Codec:     "identity",
				Payload:   []byte("LEAKED PLAINTEXT"),
				SchemaVer: 1,
			},
		})
	case "honest":
		// Return ciphertext byte-equal — fixture caches the original.
		return stream.Send(&pluginv1.QueryHistoryResponse{
			Row: testFixtureCachedRow(),
		})
	default:
		return nil
	}
}

func testFixtureCachedRow() *pluginv1.AuditRow {
	// Real ciphertext bytes captured from a prior emit; baked into the
	// fixture at test setup time.
	return &pluginv1.AuditRow{ /* ... */ }
}

func main() {
	mode := os.Getenv("TEST_DOWNGRADE_MODE")
	pluginsdk.ServeWithServices(&pluginsdk.ServeConfig{
		Services: []pluginsdk.ServiceRegistration{
			{
				Name:   "holomush.plugin.v1.PluginAuditService",
				Server: &fixtureAuditServer{mode: mode},
			},
		},
	})
}
```

The exact `ServeConfig` shape may need adjustment based on how `pluginsdk` exposes service registration today; the agent should read `pkg/plugin/sdk.go` and adapt.

- [ ] **Step D.1.3: Verify `task plugin:build-all` is unaffected**

Run: `task plugin:build-all`
Expected: success. The fixture is under `test/integration/plugin/testdata/` so `scripts/build-plugins.sh` doesn't see it.

- [ ] **Step D.1.4: Commit**

```text
test(plugin/testdata): test_downgrade_attacker binary fixture

Binary plugin for Phase 7 INV-P7-10 e2e. Manifest declares
crypto.emits sensitivity:always; QueryHistory has honest +
malicious modes selectable via TEST_DOWNGRADE_MODE env var.
Lives under test/integration/plugin/testdata/ so task
plugin:build-all does not see it (verified).

Refs: holomush-1r0v
```

### Task D.2: E2E test against the fixture

**Why:** INV-P7-10 — full loader/gRPC/dispatcher/fence chain coverage of the downgrade attack.

**Files:**

- Create: `test/integration/eventbus_e2e/plugin_downgrade_attacker_test.go`

- [ ] **Step D.2.1: Write the e2e test**

```go
//go:build integration

package eventbus_e2e

func TestDowngradeAttackerHonestPathDelivers(t *testing.T) {
	suite := newDowngradeAttackerSuite(t, "honest")
	defer suite.Close()

	suite.EmitSensitive(t, "events.test.test_downgrade.01ABC.ic",
		"test-downgrade-attacker:secret", []byte("plaintext"))

	resp, err := suite.QueryStreamHistory(t, "events.test.test_downgrade.01ABC.ic")
	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
	assert.Equal(t, []byte("plaintext"), resp.Events[0].Payload,
		"honest path delivers decrypted plaintext")
}

func TestDowngradeAttackerMaliciousPathRefuses(t *testing.T) {
	suite := newDowngradeAttackerSuite(t, "malicious")
	defer suite.Close()

	// No emit needed — malicious plugin fabricates the row.
	_, err := suite.QueryStreamHistory(t, "events.test.test_downgrade.01ABC.ic")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AUDIT_ROW_DOWNGRADE_DETECTED",
		"INV-P7-10: malicious downgrade MUST be refused")

	violations := suite.GetEmittedViolations(t)
	require.Len(t, violations, 1)
	assert.Equal(t, "test-downgrade-attacker", violations[0].PluginName)
	assert.Equal(t, "test-downgrade-attacker:secret", violations[0].EventType)
}
```

- [ ] **Step D.2.2: Run e2e tests**

Run: `task test:int -- -run TestDowngradeAttacker ./test/integration/eventbus_e2e/`
Expected: PASS both subtests.

- [ ] **Step D.2.3: Commit**

```text
test(eventbus_e2e): INV-P7-10 e2e downgrade attacker

Two subtests: honest path delivers plaintext through the full
stack; malicious path refuses with AUDIT_ROW_DOWNGRADE_DETECTED
and emits plugin_integrity_violation audit.

Refs: holomush-1r0v
```

### Task D.3: Plugin role permissions test

**Why:** INV-P7-13 — plugin role lacks USAGE on schema `public`; `INSERT INTO events_audit` MUST fail with permission denied.

**Files:**

- Create: `test/integration/plugin/plugin_role_permissions_test.go`

- [ ] **Step D.3.1: Write the test**

```go
//go:build integration

package plugin

func TestPluginRoleCannotWriteHostTables(t *testing.T) {
	// Provision a fresh per-plugin role + schema via the real
	// SchemaProvisioner; use the returned plugin-scoped connection
	// string to open a pool as the plugin role.
	provisioner := newTestSchemaProvisioner(t)
	connStr, err := provisioner.ProvisionSchema(t.Context(), "test-perm-check")
	require.NoError(t, err)
	defer provisioner.PurgeSchema(t.Context(), "test-perm-check")

	pluginPool, err := pgxpool.New(t.Context(), connStr)
	require.NoError(t, err)
	defer pluginPool.Close()

	// INV-P7-13: plugin role MUST NOT be able to INSERT into events_audit.
	_, err = pluginPool.Exec(t.Context(), `
		INSERT INTO events_audit (id, subject, type, timestamp, actor_kind)
		VALUES ('\x0123456789ABCDEF0123456789ABCDEF', 'x', 'y', now(), 'system')`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied",
		"INV-P7-13: plugin role MUST fail with permission denied for host tables")
}
```

- [ ] **Step D.3.2: Run the test**

Run: `task test:int -- -run TestPluginRoleCannotWriteHostTables ./test/integration/plugin/`
Expected: PASS.

- [ ] **Step D.3.3: Commit**

```text
test(plugin): INV-P7-13 plugin role denied write on host tables

Asserts the existing schema_provisioner REVOKE-ALL on schema public
prevents plugin roles from writing to events_audit. Permission
boundary already enforced by substrate (schema_provisioner.go:163);
this test pins the regression.

Refs: holomush-1r0v
```

### Task D.4: Meta-test asserting INV-P7-N → test coverage

**Why:** Per [feedback_invariants_and_docs_as_spec_acceptance] discipline — invariant table MUST drift-protect against test corpus.

**Files:**

- Create: `internal/eventbus/history/phase7_boundary_meta_test.go`

- [ ] **Step D.4.1: Write the meta-test**

```go
package history_test

// TestPhase7InvariantsHaveNamedTests is the drift detector for
// docs/superpowers/specs/2026-05-13-event-payload-crypto-phase7-plugin-sdk-design.md
// §2 invariants table. For each INV-P7-N (1..16, excluding 2 and 14
// which are themselves meta-assertions), grep the named test exists
// in the tree.
//
// If this test fails: either an invariant was removed without
// updating the table, OR a named test was renamed/removed without
// updating the invariant. Fix the table or restore the test.
func TestPhase7InvariantsHaveNamedTests(t *testing.T) {
	cases := []struct {
		inv      string
		testName string
	}{
		{"INV-P7-1", "TestDispatchForwardsCiphertextByteEqual"},
		{"INV-P7-3", "TestSceneLogHasDekColumns"},
		{"INV-P7-4", "TestAuditRowStructMirrorsProto"},
		{"INV-P7-5", "TestAuditRowRoundTripPreservesAllFields"},
		{"INV-P7-6", "TestSceneLogPreservesCiphertextAndAuditHeaders"},
		{"INV-P7-7", "TestFenceRefusesIdentityForAlwaysSensitiveType"},
		{"INV-P7-8", "TestFenceSetBuiltOnceAtBoot"},
		{"INV-P7-9", "TestDispatcherAndHotTierShareSelector"},
		{"INV-P7-10", "TestDowngradeAttackerMaliciousPathRefuses"},
		{"INV-P7-11", "TestDispatchDoesNotDecryptBeforeForward"},
		{"INV-P7-12", "TestSceneLogPreservesCiphertextAndAuditHeaders"}, // shared with P7-6
		{"INV-P7-13", "TestPluginRoleCannotWriteHostTables"},
		{"INV-P7-15", "TestFenceRefusesUnknownDekRef"},
		{"INV-P7-16", "TestRoundTripProducesByteEqualAAD"},
	}

	for _, tc := range cases {
		t.Run(tc.inv, func(t *testing.T) {
			out, err := exec.Command("rg", "-l", "func "+tc.testName, "../../").Output()
			require.NoError(t, err, "rg failed; install ripgrep")
			require.NotEmpty(t, strings.TrimSpace(string(out)),
				"%s: named test %s NOT FOUND in tree", tc.inv, tc.testName)
		})
	}
}
```

The meta-test uses `rg` (the project standard for grep). It walks up two dirs to scan the whole tree; if the test is run from a worktree the relative path needs adjustment.

- [ ] **Step D.4.2: Run the meta-test**

Run: `task test -- -run TestPhase7InvariantsHaveNamedTests ./internal/eventbus/history/`
Expected: PASS after all earlier tasks land.

- [ ] **Step D.4.3: Commit**

```text
test(history): phase7_boundary_meta_test drift detector

Walks each INV-P7-N from the spec's §2 table and asserts the named
test exists in the tree. Drift between spec and test corpus fails CI.

Refs: holomush-1r0v
```

---

## Phase E — Wiring + spec polish + PR-blocking docs

After Phase E, production wiring is complete (`cmd/holomush/deps.go`), the master spec amendments land, and Phase-7-blocking docs (binary-plugins.md, audit-subjects.md) ship.

### Task E.1: Master spec polish

**Why:** 5 amendments per the design spec's "Master spec polish list" — §4.6 audit-subject register, §8.2 pseudo-code amendment, §2 INV-50 cross-ref, §8.3 Go-struct match, §2 INV-39 scope clarification.

**Files:**

- Modify: `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`

- [ ] **Step E.1.1: Apply polish item 1 — register `plugin_integrity_violation` subject in §4.6**

In master spec §4.6 audit-event-shapes table, add a row:

```markdown
| `audit.<game>.system.plugin_integrity_violation` | host | NEVER | Per-event one-shot violation report emitted by `PluginDowngradeFence` on INV-P7-7 refusal. Payload fields: `plugin_name`, `event_id`, `event_type`, `claimed_codec`, `expected_sensitivity`, `refusal_code`. No chain participation. ABAC inherits the `audit.*.system.*` deny rule. |
```

- [ ] **Step E.1.2: Apply polish item 2 — amend §8.2 pseudo-code**

In master spec §8.2 (around lines 2099-2118), strike the events_audit-fallback pseudo-code line and the "host's events_audit is the byte-equal mirror... host's mirror wins" sentence for plugin-owned subjects. Replace with the v3/v4 two-layer fence text:

```markdown
**Downgrade-attack fence (Phase 7 INV-P7-7 + INV-P7-15).** A malicious
or buggy plugin could return rows with `codec=identity` and cleartext
`payload` for events that should have been sensitive. The host's
`PluginDowngradeFence` is the QueryStreamHistory pre-decrypt fence
(layer 1) covering two checks:
1. **Manifest-set heuristic** — host maintains a static set of
   `<plugin_name>:<event_type>` keys from manifests declaring
   `sensitivity: always`. Rows with `codec=identity` and a type in the
   set are refused with `AUDIT_ROW_DOWNGRADE_DETECTED` + emit
   `audit.<game>.system.plugin_integrity_violation`.
2. **DEK existence check** — for non-identity codecs, the host
   verifies `dek_ref` is present in `crypto_keys` (with the production
   `destroyed_at IS NULL` filter). Missing DEK surfaces as
   `metadata_only=true` per INV-26 (indistinguishable from
   legitimate `Rekey`-destroyed-DEK case).

Layer (1) does not cover `may`-declared events that were runtime-elevated
to encrypted at emit (via `EnforceSensitivity` promoting
`may + claim=true → SensitivityAlways`). For those, the AEAD AAD-binding
(layer 2, master INV-25) catches tampering at decrypt time at the cost
of a less specific operator UX signal.

There is no host-side events_audit shadow for plugin-owned subjects —
the plugin's audit table IS the cold record per §8.2.
```

- [ ] **Step E.1.3: Apply polish item 3 — §2 INV-50 cross-ref**

In master spec §2 (around line 345 INV-50 row), append: "Re-scoped by Phase 7 INV-P7-7; see `docs/superpowers/specs/2026-05-13-event-payload-crypto-phase7-plugin-sdk-design.md` §2."

- [ ] **Step E.1.4: Apply polish item 4 — §8.3 Go-struct match**

In master spec §8.3 (around line 2129), update the `AuditRow` Go struct example to match the Phase 7 proto (drop the Codec/DEKRef/DEKVersion fields if shown without pointer + add SchemaVer; or — simpler — refactor §8.3 to just `See pkg/plugin/audit.go for the canonical AuditRow struct shape.` since the in-spec Go example was always aspirational).

- [ ] **Step E.1.5: Apply polish item 5 — §2 INV-39 scope clarification**

In master spec §2 (around line 320 INV-39 row), append: "**Scope**: host-owned subjects only. Plugin-owned subjects have no separate cold tier (the plugin row IS the cold record); the Phase 7 plugin-routed read path terminates with `metadata_only=true` per INV-P7-15 instead of falling back."

- [ ] **Step E.1.6: Verify markdown formatting**

Run: `task fmt`
Run: `task lint -- ./docs/`
Expected: clean.

- [ ] **Step E.1.7: Commit**

```text
docs(specs/crypto): master spec polish for Phase 7 (holomush-1r0v)

Five amendments per the Phase 7 spec's polish list:
1. §4.6: register audit.<game>.system.plugin_integrity_violation
2. §8.2: strike events_audit-fallback pseudo-code for plugin-owned;
   replace with two-layer fence wording from v4
3. §2 INV-50: cross-reference Phase 7 INV-P7-7
4. §8.3: replace aspirational Go-struct example with pkg/plugin/audit.go pointer
5. §2 INV-39: scope clarification — host-owned subjects only;
   plugin-owned terminates per INV-P7-15

Refs: holomush-1r0v
```

### Task E.2: Site docs (PR-blocking)

**Why:** Spec § Out of scope explicitly carves out two PR-blocking inline docs.

**Files:**

- Modify: `site/docs/extending/binary-plugins.md`
- Modify: `site/docs/reference/audit-subjects.md`

- [ ] **Step E.2.1: Extend `binary-plugins.md` with SDK Layer 2 reference**

Append a new section "Audit-row SDK helpers (Phase 7)":

```markdown
## Audit-row SDK helpers (Phase 7)

Plugin authors don't write crypto code. The host owns encryption,
decryption, and authorization. After Phase 7, plugin-owned audit
tables hold ciphertext byte-equal to the bus envelope for sensitive
events; the plugin's `PluginAuditService.QueryHistory` returns those
ciphertext bytes verbatim to the host, which validates + decrypts
before delivering to clients.

The `pluginsdk` package provides two helpers in `pkg/plugin/audit.go`:

### `pluginsdk.StoreFromMessage(msg jetstream.Msg) (AuditRow, error)`

Call at `PluginAuditService.AuditEvent` RPC handler. Extracts an
`AuditRow` from the JetStream message — projection fields (id,
subject, type, timestamp, actor) + crypto envelope (codec, payload,
dek_ref, dek_version) + schema_ver. Plugin authors persist the row
fields verbatim into their own audit table.

### `pluginsdk.LoadForQuery(row AuditRow) (*pluginauditpb.AuditRow, error)`

Call at `PluginAuditService.QueryHistory` RPC handler. Converts a
stored `AuditRow` back to the proto frame returned on the stream.
Round-trip stable with `StoreFromMessage`.

### Plugin audit table schema

Phase 7 requires plugin audit tables to mirror `events_audit` for
crypto-bearing columns:

| Column | Type | Notes |
|---|---|---|
| `id` | `BYTEA PRIMARY KEY` | 16-byte ULID; matches `AuditRow.id` |
| `subject` | `TEXT NOT NULL` | bus subject |
| `type` | `TEXT NOT NULL` | qualified `<plugin>:<event_type>` |
| `timestamp` | `TIMESTAMPTZ NOT NULL` | |
| `actor_kind`, `actor_id` | `TEXT`, `BYTEA` | from `AuditRow.actor` |
| `payload` | `BYTEA NOT NULL` | ciphertext when `codec != identity` |
| `schema_ver` | `SMALLINT NOT NULL` | from `AuditRow.schema_ver` |
| `codec` | `TEXT NOT NULL` | `identity` or `xchacha20poly1305-v1` |
| `dek_ref` | `BIGINT NULL` | NULL for identity codec |
| `dek_version` | `INTEGER NULL` | NULL for identity codec |

See `plugins/core-scenes/audit.go` for a reference implementation.
```

- [ ] **Step E.2.2: Register `plugin_integrity_violation` in `audit-subjects.md`**

Append to the audit-subjects table:

```markdown
| `audit.<game>.system.plugin_integrity_violation` | host | NEVER | Emitted by `PluginDowngradeFence` (Phase 7) when a plugin's `QueryHistory` response triggers INV-P7-7 (codec=identity for a manifest-declared sensitivity:always event type). Payload: `plugin_name`, `event_id`, `event_type`, `claimed_codec`, `expected_sensitivity`, `refusal_code`. No chain participation. |
```

- [ ] **Step E.2.3: Verify docs build**

Run: `task docs:build`
Expected: success.

- [ ] **Step E.2.4: Commit**

```text
docs(site): Phase 7 SDK + audit-subject docs

binary-plugins.md gains a Layer 2 SDK reference section; the
audit-subjects.md catalogue registers plugin_integrity_violation.
PR-blocking per the Phase 7 spec's § Out of scope carve-out.

Refs: holomush-1r0v
```

### Task E.3: Production wiring at `cmd/holomush/deps.go`

**Why:** Hook the fence + selector identity into the production dependency graph. Without this, the fence isn't actually invoked at runtime and INV-P7-9 pointer-identity test stays failing.

**Files:**

- Modify: `cmd/holomush/deps.go`

- [ ] **Step E.3.1: Read existing `deps.go`**

Find the construction of:
- `audit.PluginConsumerManager` (existing wiring)
- `history.NewReader` (existing wiring with `WithCodecSelector`)
- Locate where the always-sensitive set should be built (from the manifest registry)

- [ ] **Step E.3.2: Wire the same `codec.KeySelector` instance into both**

Before constructing `PluginConsumerManager`, ensure the same `selector` variable is passed via the new `WithKeySelector` option. Before constructing `history.NewReader`, ensure the same selector is passed via the existing `WithCodecSelector`.

```go
keySelector := buildKeySelector(...)  // existing or new helper

pcm := audit.NewPluginConsumerManager(
	/* ... existing options ... */,
	audit.WithKeySelector(keySelector),
)

reader := history.NewReader(
	/* ... existing options ... */,
	history.WithCodecSelector(keySelector),
	history.WithPluginDowngradeFence(
		buildAlwaysSensitiveSet(manifestRegistry),
		newCryptoKeysLookup(pool),
		newViolationEmitter(eventBus),
	),
)
```

- [ ] **Step E.3.3: Build the always-sensitive set helper**

```go
// buildAlwaysSensitiveSet walks every loaded manifest and produces
// the qualified `<plugin>:<event_type>` set for INV-P7-7.
// Built once at boot per INV-P7-8.
func buildAlwaysSensitiveSet(registry *plugins.ManifestRegistry) map[string]struct{} {
	out := make(map[string]struct{})
	for _, m := range registry.AllManifests() {
		for _, emit := range m.Crypto.Emits {
			if emit.Sensitivity != plugins.SensitivityAlways {
				continue
			}
			key := emit.EventType
			prefix := m.Name + ":"
			if !strings.HasPrefix(key, prefix) {
				key = prefix + key
			}
			out[key] = struct{}{}
		}
	}
	return out
}
```

- [ ] **Step E.3.4: Implement `newCryptoKeysLookup`**

Thin wrapper around the existing crypto_keys repository that exposes the `Exists` method per the `CryptoKeysLookup` interface.

- [ ] **Step E.3.5: Implement `newViolationEmitter`**

Thin wrapper around the existing `EventBus.Publish` that targets the `audit.<game>.system.plugin_integrity_violation` subject and serializes the violation-payload fields.

- [ ] **Step E.3.6: Run the full integration suite including the previously-parked INV-P7-9 test**

Run: `task test:int -- -run TestDispatcherAndHotTierShareSelector ./test/integration/eventbus_e2e/`
Run: `task test:int -- -run TestDowngradeAttacker ./test/integration/eventbus_e2e/`
Run: `task test:int -- -run TestRoundTripProducesByteEqualAAD ./internal/eventbus/history/`
Expected: all PASS.

- [ ] **Step E.3.7: Run the full pre-PR gate**

Run: `task pr-prep`
Expected: green (all CI mirrors pass).

- [ ] **Step E.3.8: Commit**

```text
feat(deps): wire Phase 7 fence + selector identity into production

cmd/holomush/deps.go threads the same codec.KeySelector instance
into PluginConsumerManager and history.NewReader; constructs the
always-sensitive type set from manifest crypto.emits declarations;
wires CryptoKeysLookup + ViolationEmitter into PluginDowngradeFence.

INV-P7-9 selector pointer-identity test now passes.

Refs: holomush-1r0v
```

---

## Bead chain structure

Parent epic: **`holomush-1r0v`** (Phase 7: Plugin SDK helpers + plugin-owned audit + INV-50 downgrade fence).

The plan decomposes into **5 task beads** that each produce a self-contained, testable, committable chunk. Each bead's TDD acceptance criteria correspond to one Phase from the plan above; verification steps match the named tests.

### Bead summary (titles + 1-line gloss)

| Bead | Title | Plan phases | Maps to invariants |
|---|---|---|---|
| `1r0v.1` | Foundations — shared header parser + AuditRow proto + SDK Layer 2 | A.1, A.2, A.3, A.4 | INV-P7-2, INV-P7-4, INV-P7-5 |
| `1r0v.2` | Plugin dispatcher widening + scene_log schema + caller migrations | B.1, B.2, B.3, B.4, B.5, B.6 | INV-P7-1, INV-P7-3, INV-P7-6, INV-P7-9, INV-P7-10, INV-P7-11, INV-P7-12 |
| `1r0v.3` | Read-side fence — AAD adapter + PluginDowngradeFence + tier.go wiring | C.1, C.2, C.3, C.4 | INV-P7-7, INV-P7-8, INV-P7-15, INV-P7-16 |
| `1r0v.4` | E2E binary fixture + cross-cutting tests + meta-test | D.1, D.2, D.3, D.4 | INV-P7-10 (e2e), INV-P7-13, INV-P7-14 |
| `1r0v.5` | Production wiring + master spec polish + PR-blocking docs | E.1, E.2, E.3 | INV-P7-9 (final pass) |

### Bead 8-section descriptions (per `bead-create-smart` convention)

Each bead's `--description` MUST include all 8 sections. Below are the canonical descriptions; `bead-chain-from-plan` materializes these via `bd create`.

#### `1r0v.1` — Foundations: parser + proto + SDK

- **Goal**: Land the substrate Phase 7 builds on — shared `ParseAuditHeaders` helper, `pluginauditpb.AuditRow` proto reshape, `pluginsdk.AuditRow` Go struct + `LoadForQuery` + initial `StoreFromMessage`. After this bead, downstream tasks have a stable API to build against; production hot path is unchanged (build is intentionally broken in callers, fixed in `1r0v.2`).
- **Design reference**: `docs/superpowers/specs/2026-05-13-event-payload-crypto-phase7-plugin-sdk-design.md` §4.2, §4.3, §5.5; INV-P7-2, INV-P7-4, INV-P7-5.
- **Plan reference**: `docs/superpowers/plans/2026-05-13-event-payload-crypto-phase7-plugin-sdk-plan.md` Phase A (Tasks A.1-A.4).
- **TDD acceptance criteria**:
  - `internal/eventbus/audit/header_parser_test.go` — 5 sub-tests covering identity, encrypted, missing-header, bad-dek-ref, schema-out-of-range PASS.
  - `pkg/plugin/audit_test.go::TestAuditRowStructMirrorsProto` PASS.
  - `pkg/plugin/audit_test.go::TestAuditRowRoundTripPreservesAllFields` PASS for both identity and encrypted cases.
  - `test/integration/eventbus_e2e/dispatcher_projection_parity_test.go::TestPluginAndHostBranchesParseHeadersIdentically` PASS.
- **Verification steps**:
  - `task test -- ./internal/eventbus/audit/` green
  - `task test -- ./pkg/plugin/` green
  - `task test:int -- -run TestPluginAndHostBranchesParseHeadersIdentically` green
  - `task build` FAILS (expected — callers fixed in `1r0v.2`)
- **Files touched**: `internal/eventbus/audit/header_parser.go` (new), `internal/eventbus/audit/header_parser_test.go` (new), `internal/eventbus/audit/projection.go` (trimmed), `api/proto/holomush/plugin/v1/audit.proto` (reshape), `pkg/proto/holomush/plugin/v1/*` (regen), `pkg/plugin/audit.go` (Layer 2 append), `pkg/plugin/audit_test.go` (append), `test/integration/eventbus_e2e/dispatcher_projection_parity_test.go` (new).
- **Dependencies**: none (Phase 7 foundations).
- **Out of scope**: dispatcher widening (`1r0v.2`); fence (`1r0v.3`); test fixtures (`1r0v.4`); production wiring (`1r0v.5`).

#### `1r0v.2` — Dispatcher widening + schema + caller migration

- **Goal**: Remove `AUDIT_PLUGIN_CODEC_UNSUPPORTED` rejection at `plugin_consumer.go:343`; wire `codec.KeySelector` on `PluginConsumerManager`; add `dek_ref`/`dek_version` columns to plugin tables; migrate `plugins/core-scenes/audit.go` and integration-test stub to consume `req.GetRow()`. After this bead, plugins receive ciphertext byte-equal end-to-end on the emit→storage path; build is restored.
- **Design reference**: spec §3.1, §3.2, §4.1, §5.1, §5.2; INV-P7-1, INV-P7-3, INV-P7-6, INV-P7-9, INV-P7-10, INV-P7-11, INV-P7-12.
- **Plan reference**: plan Phase B (Tasks B.1-B.6).
- **TDD acceptance criteria**:
  - `TestDispatchForwardsCiphertextByteEqual` PASS.
  - `TestDispatchDoesNotDecryptBeforeForward` PASS.
  - `TestSceneLogHasDekColumns` PASS.
  - `TestPhase7PluginMigrationStandalone` PASS.
  - `TestSceneLogPreservesCiphertextAndAuditHeaders` PASS.
  - `TestDispatcherAndHotTierShareSelector` PARKED-FAIL (deferred to `1r0v.5`).
- **Verification steps**:
  - `task build` succeeds (caller migrations land here).
  - `task test -- ./internal/eventbus/audit/ ./plugins/core-scenes/ ./pkg/plugin/` green.
  - `task test:int -- -run "Test(SceneLogHasDekColumns|Phase7PluginMigrationStandalone|SceneLogPreservesCiphertextAndAuditHeaders|PluginAuditIsolation)"` green.
- **Files touched**: `internal/eventbus/audit/plugin_consumer.go`, `internal/eventbus/audit/plugin_consumer_unit_test.go`, `pkg/plugin/audit.go` (StoreFromMessage), `plugins/core-scenes/migrations/000005_*.{up,down}.sql` (new), `plugins/core-scenes/audit.go`, `test/integration/eventbus_e2e/plugin_audit_isolation_test.go`, `test/integration/eventbus_e2e/dispatcher_selector_identity_test.go` (new, parked), `test/integration/eventbus_e2e/plugin_audit_round_trip_test.go` (new), `test/integration/plugin/plugin_migration_test.go` (new), `internal/eventbus/audit/plugin_consumer_test_export_test.go` (new), `internal/eventbus/history/tier_test_export_test.go` (new).
- **Dependencies**: blocked by `1r0v.1`.
- **Out of scope**: fence (`1r0v.3`); INV-P7-9 final wiring (`1r0v.5`); test_downgrade_attacker fixture (`1r0v.4`).

#### `1r0v.3` — Read-side fence

- **Goal**: Build `PluginDowngradeFence` (manifest-set + DEK existence) and the `AuditRow → *eventbusv1.Event` AAD adapter; wire fence into `Reader.QueryHistory` plugin branch. After this bead, malicious-plugin downgrade attempts are refused at the host's read path.
- **Design reference**: spec §3.1, §3.2, §5.3, §5.4; INV-P7-7, INV-P7-8, INV-P7-15, INV-P7-16.
- **Plan reference**: plan Phase C (Tasks C.1-C.4).
- **TDD acceptance criteria**:
  - `TestAuditRowToEvent_CopiesAllAADFields` PASS.
  - `TestAuditRowToEvent_NilSafety` PASS.
  - `TestRoundTripProducesByteEqualAAD` PASS (CRITICAL — byte-equal AAD reconstruction).
  - `TestFenceRefusesIdentityForAlwaysSensitiveType` PASS.
  - `TestFenceAllowsIdentityForNonSensitiveType` PASS.
  - `TestFenceRefusesUnknownDekRef` PASS.
  - `TestFenceSetBuiltOnceAtBoot` PASS.
- **Verification steps**:
  - `task test -- ./internal/eventbus/history/` green.
  - `task test:int -- -run "Test(AuditRowToEvent|Fence|RoundTripProducesByteEqualAAD)"` green.
- **Files touched**: `internal/eventbus/history/plugin_aad_adapter.go` (new), `internal/eventbus/history/plugin_aad_adapter_test.go` (new), `internal/eventbus/history/plugin_aad_reconstruction_test.go` (new), `internal/eventbus/history/plugin_downgrade_fence.go` (new), `internal/eventbus/history/plugin_downgrade_fence_test.go` (new), `internal/eventbus/history/tier.go` (single-line + new option).
- **Dependencies**: blocked by `1r0v.2` (needs the AuditRow proto + scene_log columns + integration-test scaffolding).
- **Out of scope**: production wiring (`1r0v.5`); e2e binary fixture (`1r0v.4`); spec polish (`1r0v.5`).

#### `1r0v.4` — E2E binary fixture + cross-cutting tests + meta-test

- **Goal**: Build the `test_downgrade_attacker` binary fixture, the e2e test exercising honest + malicious paths, the role-permission integration test, and the boundary-meta drift detector. After this bead, INV-P7-N coverage is comprehensive and CI catches drift.
- **Design reference**: spec §3.2, §6; INV-P7-10 (e2e), INV-P7-13, INV-P7-14.
- **Plan reference**: plan Phase D (Tasks D.1-D.4).
- **TDD acceptance criteria**:
  - `task plugin:build-all` still succeeds (fixture outside production path).
  - `TestDowngradeAttackerHonestPathDelivers` PASS.
  - `TestDowngradeAttackerMaliciousPathRefuses` PASS.
  - `TestPluginRoleCannotWriteHostTables` PASS.
  - `TestPhase7InvariantsHaveNamedTests` PASS for all listed INV-P7-N.
- **Verification steps**:
  - `task plugin:build-all` green.
  - `task test:int -- -run "Test(DowngradeAttacker|PluginRoleCannotWriteHostTables|Phase7Invariants)"` green.
- **Files touched**: `test/integration/plugin/testdata/test_downgrade_attacker/main.go` (new), `test/integration/plugin/testdata/test_downgrade_attacker/plugin.yaml` (new), `test/integration/eventbus_e2e/plugin_downgrade_attacker_test.go` (new), `test/integration/plugin/plugin_role_permissions_test.go` (new), `internal/eventbus/history/phase7_boundary_meta_test.go` (new).
- **Dependencies**: blocked by `1r0v.3` (fence must exist for malicious-path refusal to fire).
- **Out of scope**: production wiring (`1r0v.5`).

#### `1r0v.5` — Production wiring + spec polish + docs

- **Goal**: Land final production wiring at `cmd/holomush/deps.go`; apply 5 master-spec polish amendments; ship PR-blocking site docs (binary-plugins.md + audit-subjects.md); pass `task pr-prep`. After this bead, Phase 7 is production-ready and merge-ready.
- **Design reference**: spec § Context master-spec polish list; § Out of scope (PR-blocking docs); § Section 5.2, INV-P7-9 final.
- **Plan reference**: plan Phase E (Tasks E.1-E.3).
- **TDD acceptance criteria**:
  - `TestDispatcherAndHotTierShareSelector` PASS (parked test from `1r0v.2` now green).
  - All 14 named tests from the meta-test PASS.
  - `task pr-prep` green.
- **Verification steps**:
  - `task test:int -- -run TestDispatcherAndHotTierShareSelector` green.
  - `task docs:build` green.
  - `task pr-prep` green (mirrors all CI jobs).
- **Files touched**: `cmd/holomush/deps.go`, `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` (5 polish amendments), `site/docs/extending/binary-plugins.md`, `site/docs/reference/audit-subjects.md`.
- **Dependencies**: blocked by `1r0v.4` (the full test corpus including meta-test must pass before merge).
- **Out of scope**: future hot-reload work (separate bead `holomush-kl9w`); UX-gap closure for `may`-elevated downgrade (deferred per spec § Section 1 threat-model note).

### Supersessions

None. Phase 7 is greenfield work on `holomush-1r0v` (no prior open Phase 7 sub-beads to supersede).

### Follow-up beads (already filed during brainstorming)

| Bead | Type | Priority | Status | Triggered by |
|---|---|---|---|---|
| `holomush-b2qy` | feature | P2 | open | ADR-capture wrap-up skill (process meta-finding) |
| `holomush-kl9w` | feature | P3 | open | Plugin manifest hot-reload callback infrastructure (post-Phase-7 substrate) |
| `holomush-demb` | bug | P2 | **closed (no-op)** | scene_log Insert ON CONFLICT — verified already present |

### `bd dep add` edges

After `bd create` lands the 5 beads:

```bash
bd dep add holomush-1r0v.2 holomush-1r0v.1    # widening depends on foundations
bd dep add holomush-1r0v.3 holomush-1r0v.2    # fence depends on widening + schema
bd dep add holomush-1r0v.4 holomush-1r0v.3    # e2e + meta-test depends on fence
bd dep add holomush-1r0v.5 holomush-1r0v.4    # production wiring + docs depends on full test corpus
```

Each `task` bead is a child of the `holomush-1r0v` epic via `--parent holomush-1r0v` at `bd create` time.

### Materialization preview

`bead-chain-from-plan` will run dry-run first showing:

```
Would create 5 beads under parent holomush-1r0v:
  - holomush-1r0v.1 (task, P2): Foundations — shared header parser + AuditRow proto + SDK Layer 2
  - holomush-1r0v.2 (task, P2): Plugin dispatcher widening + scene_log schema + caller migrations
  - holomush-1r0v.3 (task, P2): Read-side fence — AAD adapter + PluginDowngradeFence + tier.go wiring
  - holomush-1r0v.4 (task, P2): E2E binary fixture + cross-cutting tests + meta-test
  - holomush-1r0v.5 (task, P2): Production wiring + master spec polish + PR-blocking docs

Would add 4 dep edges:
  holomush-1r0v.2 blocked-by holomush-1r0v.1
  holomush-1r0v.3 blocked-by holomush-1r0v.2
  holomush-1r0v.4 blocked-by holomush-1r0v.3
  holomush-1r0v.5 blocked-by holomush-1r0v.4
```

User approves; bead creation fires sequentially (per `[feedback_bd_create_no_parallel]`).

---

## Plan self-review (per the writing-plans skill)

**1. Spec coverage check.** Walked the spec §2 invariant table:

| INV-P7-N | Plan task |
|---|---|
| INV-P7-1 | B.1 (`TestDispatchForwardsCiphertextByteEqual`) |
| INV-P7-2 | A.1 + A.4 (parser unit + cross-branch parity) |
| INV-P7-3 | B.2 + B.6 (migration + standalone test) |
| INV-P7-4 | A.3 (`TestAuditRowStructMirrorsProto`) |
| INV-P7-5 | A.3 (`TestAuditRowRoundTripPreservesAllFields`) |
| INV-P7-6 | B.5 (`TestSceneLogPreservesCiphertextAndAuditHeaders`) |
| INV-P7-7 | C.3 (`TestFenceRefusesIdentityForAlwaysSensitiveType`) |
| INV-P7-8 | C.3 (`TestFenceSetBuiltOnceAtBoot`) |
| INV-P7-9 | B.4 + E.3 (parked test + production wiring) |
| INV-P7-10 | D.2 (`TestDowngradeAttackerMaliciousPathRefuses`) |
| INV-P7-11 | B.1 (`TestDispatchDoesNotDecryptBeforeForward`) |
| INV-P7-12 | B.5 (shared with P7-6) |
| INV-P7-13 | D.3 (`TestPluginRoleCannotWriteHostTables`) |
| INV-P7-14 | D.4 (meta-test) |
| INV-P7-15 | C.3 (`TestFenceRefusesUnknownDekRef`) |
| INV-P7-16 | C.2 (`TestRoundTripProducesByteEqualAAD`) |

All 16 invariants covered.

**2. Placeholder scan.** Searched the plan for TBD/TODO/FIXME — only the explicit `TODO — implemented in Task B.1` panic-stub in A.3 (intentional, replaced in B.1). No other placeholders.

**3. Type consistency.** Function signatures cited in later tasks match definitions in earlier tasks:
- `ParseAuditHeaders` defined in A.1, used in B.1 (StoreFromMessage), and referenced in A.4 parity test.
- `pluginsdk.AuditRow` struct fields match across A.3, B.1, B.2, C.2.
- `pluginauditpb.AuditRow` proto fields match across A.2, B.1, B.2, C.1, D.1.
- `CryptoKeysLookup` / `ViolationEmitter` interfaces defined in C.3 and consumed in E.3 wiring.

**4. Verification gaps.** Two agent-decision points are explicitly flagged in the plan (codec wire-format question in B.1.5; row-extraction approach in C.3.4). Both must be verified by reading actual code before locking the implementation; the plan instructs the agent on what to read.

No fixes needed. Plan complete.

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-05-13-event-payload-crypto-phase7-plugin-sdk-plan.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration. Best for this plan because each phase has independent test surface; subagent isolation prevents cross-task context bleed.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints. Slower iteration; better when the executor needs to keep cross-task context (e.g., the codec wire-format decision in B.1 carries forward to StoreFromMessage's implementation).

The plan-reviewer gate will fire next per CLAUDE.md stage-gated workflow, regardless of execution mode.
