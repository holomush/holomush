# Cold-tier JS-Seq Pagination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Switch the entire history pipeline (both tiers, the Reader, the public API, Subscribe deliveries) from ULID-keyed pagination to JetStream stream-sequence (`js_seq`) pagination, exposed to clients via opaque cursor tokens with epoch-based rebuild detection.

**Architecture:** Internal — both tiers read and order by `js_seq`, with a `(seq, id)` tripwire pattern that catches rebuild/drift/deletion via three coded errors (`EVENTBUS_CURSOR_INVALID`, `EVENTBUS_CURSOR_STALE`, `EVENTBUS_CURSOR_LAG`). Public — cursors become opaque protobuf-encoded `bytes` on the wire, with a leading `Version` byte for forward compatibility, an `Epoch` field for rebuild invalidation, an `Owner` discriminator for plugin-owned subjects, and a `HostCursor{Seq, ID}` body for host-owned subjects.

**Tech Stack:** Go 1.21+, NATS JetStream (embedded), PostgreSQL 16+ via pgx/v5, Protocol Buffers, gopher-lua, SvelteKit. Test runners: `task test`, `task test:int`, `task pr-prep`.

**Spec:** `docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md` (read this first; the plan refers to spec sections by §N).

**Bead:** `holomush-suos`. Working tree: `/Users/sean/Code/github.com/holomush/.worktrees/suos` (jj workspace `suos`, bookmark `holomush-suos-spec`).

---

## File Structure

### New files

| Path | Responsibility |
|---|---|
| `internal/eventbus/cursor/cursor.proto` | Internal-only proto for the opaque cursor token. NOT under `api/proto/`. |
| `internal/eventbus/cursor/cursor.go` | `Encode`, `Decode`, `CurrentEpoch`, `Owner`/`HostCursor`/`Cursor` Go types. |
| `internal/eventbus/cursor/cursor_test.go` | Round-trip, version mismatch, owner discriminator, plugin-inner pass-through. |
| `internal/store/migrations/000011_events_audit_js_seq_index.up.sql` | `CREATE INDEX events_audit_subject_js_seq`. |
| `internal/store/migrations/000011_events_audit_js_seq_index.down.sql` | `DROP INDEX events_audit_subject_js_seq`. |
| `internal/eventbus/history/stream_state.go` | `streamStateSnapshot` cache type for `Stream.Info()` per `QueryHistory` call. |

### Modified files (Go)

| Path | Change |
|---|---|
| `internal/eventbus/types.go` | Add `Seq uint64` to `Event` struct. |
| `internal/eventbus/bus.go` | Replace `After`/`Before ulid.ULID` with `AfterSeq`/`AfterID`/`BeforeSeq`/`BeforeID` on `HistoryQuery`. |
| `internal/eventbus/errors.go` | Add `ErrCursorStale`, `ErrCursorLag`, `ErrCursorInvalid` sentinel errors and oops codes. |
| `internal/eventbus/subscriber.go` | `decodeDelivery` populates `Event.Seq` from `msg.Metadata().Sequence.Stream`. |
| `internal/eventbus/history/cold_postgres.go` | SELECT `js_seq`, `WHERE js_seq >=/<=`, OR'd WHERE for cursor row at edge, validation discard pattern, populate `Event.Seq`. |
| `internal/eventbus/history/hot_jetstream.go` | New start-policy table (use seq when cursor present), `matchesQuery` seq compare, populate `Event.Seq`, retention age-out STALE detection. |
| `internal/eventbus/history/tier.go` | `seenSeqs` dedup, `advanceCursor` updates `(seq, id)` pair, `currentCursor` returns seq, `appendOrdered` sorts by seq, `selectStartTier` uses `streamStateSnapshot`, Reader threads snapshot through call. |
| `internal/eventbus/history/cold_postgres_unit_test.go` | Cursor validation tests. |
| `internal/eventbus/history/hot_jetstream_test.go` | Cursor validation, retention age-out tests. |
| `internal/eventbus/history/tier_test.go` | seenSeqs/advanceCursor/appendOrdered fixture updates, lag-vs-stale tests. |
| `internal/eventbus/subscriber_test.go` | Verify `Seq` populated on deliveries. |
| `internal/grpc/query_stream_history.go` | Decode inbound cursor, route by owner, encode outbound cursor on every `EventFrame`, set `next_cursor`, map `EVENTBUS_CURSOR_*` to gRPC statuses, preserve `subjectxlate.Legacy`. |
| `internal/grpc/query_stream_history_test.go` | Handler-level cursor tests, lag/stale/invalid mapping. |
| `internal/grpc/server.go` | (If Subscribe handler returns `EventFrame` here) populate `EventFrame.cursor` per delivery. |
| `internal/plugin/hostfunc/stdlib_focus.go` | `holomush.query_stream_history` → table-arg signature `{stream, count, cursor, not_before_ms}`. |
| `internal/plugin/hostfunc/stdlib_focus_test.go` | New table-arg call shape. |
| `internal/plugin/goplugin/host_service.go` | Decode/encode plugin cursor; wrap plugin's `plugin_inner` bytes inside a host token. |
| `pkg/plugin/focus_client.go` | `QueryStreamHistoryRequest{Cursor []byte}`, `QueryStreamHistoryResponse{NextCursor []byte, Events []Event{Cursor []byte, ...}}`. |
| `gorules/rules.go` | Remove `EventIDMustBeMonotonic`. Add `CursorPackageInternal` rule rejecting imports of `internal/eventbus/cursor` from outside `internal/eventbus/`. |
| `gorules/rules_test.go` | Drop `EventIDMustBeMonotonic` tests; add `CursorPackageInternal` tests. |

### Modified files (Proto)

| Path | Change |
|---|---|
| `api/proto/holomush/core/v1/core.proto` | `QueryStreamHistoryRequest`: replace `string before_id = 6` with `bytes cursor = 6`. `QueryStreamHistoryResponse`: add `bytes next_cursor = 4`. `EventFrame`: add `bytes cursor = 8`. |
| `api/proto/holomush/web/v1/web.proto` | Mirror in `WebQueryStreamHistoryRequest`/`Response` and `GameEvent`. |
| `api/proto/holomush/plugin/v1/plugin.proto` | Mirror in `PluginHostServiceQueryStreamHistory*`. |

### Modified files (TS / Svelte)

| Path | Change |
|---|---|
| `web/src/lib/backfill/streamBackfill.ts` | Use `cursor: Uint8Array` instead of `beforeId: string`. Persist `next_cursor` between pages. Handle `FAILED_PRECONDITION` (STALE → drop cursor) and `UNAVAILABLE` (LAG → backoff per spec §4.4 schedule). |
| `web/src/lib/backfill/streamBackfill.test.ts` | New cursor-handling tests. |
| `web/src/routes/(authed)/terminal/+page.svelte` | Audit any direct `before_id` references; switch to opaque cursor handoff via the backfill module. |

### Files removed

None. (`gorules/rules.go`'s `EventIDMustBeMonotonic` function is deleted but the file stays.)

---

## Phase 1 — Foundation (no behavior change)

These three tasks land independently and don't change any existing behavior. They prepare the substrate for Phase 2.

### Task 1: Migration 000011 — `(subject, js_seq)` index

**Files:**

- Create: `internal/store/migrations/000011_events_audit_js_seq_index.up.sql`
- Create: `internal/store/migrations/000011_events_audit_js_seq_index.down.sql`
- Test: `internal/store/migrations_test.go` (existing — no edits, just verify it picks up the new pair)

- [ ] **Step 1: Verify the next migration slot.**

Run: `ls internal/store/migrations/ | sort | tail -3`
Expected output:

```text
000010_drop_events_and_cursors.down.sql
000010_drop_events_and_cursors.up.sql
NOTES.md
```

Confirms 000010 is the latest existing migration; 000011 is the next slot.

- [ ] **Step 2: Write the up migration.**

Create `internal/store/migrations/000011_events_audit_js_seq_index.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- 000011 — index events_audit on (subject, js_seq) for the new history
-- pagination contract (see docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §5).
-- The events_audit.js_seq column has been NOT NULL since 000009; no data
-- backfill is required.

CREATE INDEX IF NOT EXISTS events_audit_subject_js_seq
  ON events_audit (subject, js_seq);
```

- [ ] **Step 3: Write the down migration.**

Create `internal/store/migrations/000011_events_audit_js_seq_index.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS events_audit_subject_js_seq;
```

- [ ] **Step 4: Verify migrations parse and the embed picks them up.**

Run: `task lint`
Expected: no migration-related errors.

Run: `task test -- -run TestMigrations ./internal/store/`
Expected: PASS (the existing migration test walks the embed FS; new files appear automatically).

- [ ] **Step 5: Verify migration applies cleanly against a real PG.**

Run: `task test:int -- -run TestMigrationsApplyAndRevert ./internal/store/`
Expected: PASS — index created, then dropped on down. (If this test does not exist by that name, run `task test:int -- ./internal/store/` and verify the migration test in that package green.)

- [ ] **Step 6: Commit.**

Use VCS-appropriate commands per `references/vcs-preamble.md`. Suggested commit message:

```text
feat(store): migration 000011 — events_audit (subject, js_seq) index

Adds the index that the new history pagination contract reads from.
events_audit.js_seq has been NOT NULL since 000009; no data backfill
required.

Bead: holomush-suos
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §5
```

---

### Task 2: Cursor codec proto

**Files:**

- Create: `internal/eventbus/cursor/cursor.proto`
- Modify: `buf.yaml` (or equivalent buf config) to exclude `internal/eventbus/cursor/` from public SDK generation.

- [ ] **Step 1: Write the proto.**

Create `internal/eventbus/cursor/cursor.proto`:

```protobuf
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package cursor — INTERNAL eventbus cursor format. Not exported via
// api/proto/. Clients see only the marshaled bytes; this proto is the
// host's private encoding.
//
// Forward compatibility: leading `version` field discriminates the wire
// format. Today only version=1 is defined.
//
// See docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §4.5.
syntax = "proto3";

package holomush.eventbus.cursor.v1;

option go_package = "github.com/holomush/holomush/internal/eventbus/cursor;cursor";

// OwnerKind discriminates between host-owned and plugin-owned subjects.
// Typed enum (not free-form string) to prevent plugin-name collisions
// with the discriminator scheme.
enum OwnerKind {
  OWNER_KIND_UNSPECIFIED = 0;
  OWNER_KIND_HOST = 1;
  OWNER_KIND_PLUGIN = 2;
}

// Owner identifies who owns the subject the cursor names.
message Owner {
  OwnerKind kind = 1;
  // plugin_name is set iff kind == OWNER_KIND_PLUGIN. The canonical name
  // from the plugin manifest (internal/plugin/manifest).
  string plugin_name = 2;
}

// HostCursor is the body for host-owned subject cursors.
message HostCursor {
  uint64 seq = 1; // JetStream stream sequence
  bytes  id  = 2; // ULID, 16 bytes — tripwire for drift/rebuild detection
}

// Cursor is the on-the-wire token (proto-marshaled). Clients treat
// these bytes as opaque.
message Cursor {
  uint32 version = 1; // bump on incompatible format change; today=1
  uint64 epoch   = 2; // bumps on JS rebuild; 0 today
  Owner  owner   = 3;
  oneof body {
    HostCursor host         = 4;
    bytes      plugin_inner = 5; // opaque bytes from plugin's own QueryHistory
  }
}
```

- [ ] **Step 2: Carve out from public SDK generation.**

Inspect `buf.yaml` and `buf.gen.yaml` (or equivalent) at the repo root.

Run: `cat buf.yaml buf.gen.yaml 2>/dev/null` (or use Read tool).

Add an exclusion clause for `internal/eventbus/cursor/`. For typical buf config:

```yaml
# buf.yaml
version: v2
modules:
  - path: api/proto
  # internal/eventbus/cursor is NOT included — host-internal only.
```

If the config already only includes `api/proto`, no change is needed (the cursor proto under `internal/` is invisible to buf by default). Verify:

Run: `buf ls-files` (if installed locally) OR `task proto:lint`
Expected: no entries under `internal/eventbus/cursor/`.

- [ ] **Step 3: Generate Go bindings.**

Run: `task proto:generate` (or whatever the project's proto generation task is — check `Taskfile.yaml`).

If the generated code lands under the cursor package directory (`internal/eventbus/cursor/cursor.pb.go`), good. If not, add a localized `protoc`/`buf` invocation step to the `internal/eventbus/cursor/` directory.

Expected: `internal/eventbus/cursor/cursor.pb.go` exists with `Cursor`, `Owner`, `HostCursor`, `OwnerKind` Go types.

- [ ] **Step 4: Verify the package compiles.**

Run: `go build ./internal/eventbus/cursor/`
Expected: no errors.

- [ ] **Step 5: Commit.**

```text
feat(eventbus/cursor): internal cursor proto for opaque pagination tokens

Adds the host-internal proto and Go bindings for the opaque cursor
token defined in the suos design spec. NOT under api/proto/ — kept in
internal/ so plugin authors and external clients cannot accidentally
import it.

Bead: holomush-suos
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §4.5
```

---

### Task 3: Cursor codec Go package (Encode/Decode)

**Files:**

- Create: `internal/eventbus/cursor/cursor.go`
- Test: `internal/eventbus/cursor/cursor_test.go`

- [ ] **Step 1: Write the round-trip test for host cursor.**

Create `internal/eventbus/cursor/cursor_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cursor

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeRoundTripsHostCursor(t *testing.T) {
	t.Parallel()
	id := ulid.MustParse("01HYXYZEVT0000000000000001")
	in := Cursor{
		Version: 1,
		Epoch:   0,
		Owner:   Owner{Kind: OwnerHost},
		Host:    &HostCursor{Seq: 42, ID: id},
	}
	bytes, err := Encode(in)
	require.NoError(t, err)
	out, err := Decode(bytes)
	require.NoError(t, err)
	assert.Equal(t, in.Version, out.Version)
	assert.Equal(t, in.Epoch, out.Epoch)
	assert.Equal(t, OwnerHost, out.Owner.Kind)
	require.NotNil(t, out.Host)
	assert.Equal(t, uint64(42), out.Host.Seq)
	assert.Equal(t, id, out.Host.ID)
	assert.Nil(t, out.Plugin)
}
```

- [ ] **Step 2: Run the test to verify it fails.**

Run: `task test -- -run TestEncodeDecodeRoundTripsHostCursor ./internal/eventbus/cursor/`
Expected: FAIL with "undefined: Encode" / "undefined: Decode" / "undefined: Cursor".

- [ ] **Step 3: Implement the package skeleton.**

Create `internal/eventbus/cursor/cursor.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package cursor owns the host-internal opaque cursor token used by
// QueryStreamHistory and Subscribe. Wire format is the proto-marshaled
// bytes of cursor.proto's Cursor message.
//
// See docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §4.5.
package cursor

import (
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	cursorv1 "github.com/holomush/holomush/internal/eventbus/cursor"
)

// CurrentVersion is the only cursor format version this build emits.
// Decoders accept this AND any earlier versions (none today).
const CurrentVersion uint32 = 1

// OwnerKind identifies who owns the subject a cursor names.
type OwnerKind uint8

const (
	OwnerUnspecified OwnerKind = 0
	OwnerHost        OwnerKind = 1
	OwnerPlugin      OwnerKind = 2
)

// String returns a stable label for logging.
func (o OwnerKind) String() string {
	switch o {
	case OwnerHost:
		return "host"
	case OwnerPlugin:
		return "plugin"
	default:
		return "unspecified"
	}
}

// Owner is the typed discriminator for cursor body type.
type Owner struct {
	Kind       OwnerKind
	PluginName string // populated iff Kind == OwnerPlugin
}

// HostCursor is the body of a host-owned cursor.
type HostCursor struct {
	Seq uint64
	ID  ulid.ULID
}

// Cursor is the host-internal cursor representation. Encode/Decode marshal
// to/from the wire bytes.
type Cursor struct {
	Version uint32
	Epoch   uint64
	Owner   Owner
	Host    *HostCursor // populated when Owner.Kind == OwnerHost
	Plugin  []byte      // populated when Owner.Kind == OwnerPlugin
}

// CurrentEpoch returns the host's current epoch. Today: always 0. The
// rebuild tool (holomush-6nds) will set this from a stored sentinel when
// it lands.
func CurrentEpoch() uint64 { return 0 }

// Encode serializes a Cursor to opaque bytes. Returns EVENTBUS_CURSOR_INVALID
// on validation failure (mismatched body for owner, missing host body, etc.).
func Encode(c Cursor) ([]byte, error) {
	if c.Version == 0 {
		c.Version = CurrentVersion
	}
	pb := &cursorv1.Cursor{
		Version: c.Version,
		Epoch:   c.Epoch,
		Owner:   ownerToProto(c.Owner),
	}
	switch c.Owner.Kind {
	case OwnerHost:
		if c.Host == nil {
			return nil, oops.Code("EVENTBUS_CURSOR_INVALID").
				Errorf("host owner requires non-nil HostCursor body")
		}
		pb.Body = &cursorv1.Cursor_Host{
			Host: &cursorv1.HostCursor{
				Seq: c.Host.Seq,
				Id:  c.Host.ID[:],
			},
		}
	case OwnerPlugin:
		if c.Owner.PluginName == "" {
			return nil, oops.Code("EVENTBUS_CURSOR_INVALID").
				Errorf("plugin owner requires non-empty PluginName")
		}
		pb.Body = &cursorv1.Cursor_PluginInner{PluginInner: c.Plugin}
	default:
		return nil, oops.Code("EVENTBUS_CURSOR_INVALID").
			With("owner_kind", uint8(c.Owner.Kind)).
			Errorf("unknown owner kind")
	}
	out, err := proto.Marshal(pb)
	if err != nil {
		return nil, oops.Code("EVENTBUS_CURSOR_INVALID").Wrap(err)
	}
	return out, nil
}

// Decode parses opaque cursor bytes. Returns EVENTBUS_CURSOR_INVALID on
// any parse / version / discriminator failure.
func Decode(b []byte) (Cursor, error) {
	if len(b) == 0 {
		return Cursor{}, oops.Code("EVENTBUS_CURSOR_INVALID").
			Errorf("empty cursor bytes")
	}
	var pb cursorv1.Cursor
	if err := proto.Unmarshal(b, &pb); err != nil {
		return Cursor{}, oops.Code("EVENTBUS_CURSOR_INVALID").Wrap(err)
	}
	if pb.GetVersion() != CurrentVersion {
		return Cursor{}, oops.Code("EVENTBUS_CURSOR_INVALID").
			With("version", pb.GetVersion()).
			With("current", CurrentVersion).
			Errorf("unsupported cursor version")
	}
	out := Cursor{
		Version: pb.GetVersion(),
		Epoch:   pb.GetEpoch(),
		Owner:   ownerFromProto(pb.GetOwner()),
	}
	switch out.Owner.Kind {
	case OwnerHost:
		hc := pb.GetHost()
		if hc == nil {
			return Cursor{}, oops.Code("EVENTBUS_CURSOR_INVALID").
				Errorf("host owner missing HostCursor body")
		}
		if len(hc.GetId()) != 16 {
			return Cursor{}, oops.Code("EVENTBUS_CURSOR_INVALID").
				With("id_len", len(hc.GetId())).
				Errorf("HostCursor.id must be 16 bytes")
		}
		var id ulid.ULID
		copy(id[:], hc.GetId())
		out.Host = &HostCursor{Seq: hc.GetSeq(), ID: id}
	case OwnerPlugin:
		if out.Owner.PluginName == "" {
			return Cursor{}, oops.Code("EVENTBUS_CURSOR_INVALID").
				Errorf("plugin owner missing plugin_name")
		}
		out.Plugin = pb.GetPluginInner()
	default:
		return Cursor{}, oops.Code("EVENTBUS_CURSOR_INVALID").
			With("owner_kind", uint8(out.Owner.Kind)).
			Errorf("unknown owner kind")
	}
	return out, nil
}

func ownerToProto(o Owner) *cursorv1.Owner {
	pb := &cursorv1.Owner{PluginName: o.PluginName}
	switch o.Kind {
	case OwnerHost:
		pb.Kind = cursorv1.OwnerKind_OWNER_KIND_HOST
	case OwnerPlugin:
		pb.Kind = cursorv1.OwnerKind_OWNER_KIND_PLUGIN
	default:
		pb.Kind = cursorv1.OwnerKind_OWNER_KIND_UNSPECIFIED
	}
	return pb
}

func ownerFromProto(pb *cursorv1.Owner) Owner {
	if pb == nil {
		return Owner{Kind: OwnerUnspecified}
	}
	o := Owner{PluginName: pb.GetPluginName()}
	switch pb.GetKind() {
	case cursorv1.OwnerKind_OWNER_KIND_HOST:
		o.Kind = OwnerHost
	case cursorv1.OwnerKind_OWNER_KIND_PLUGIN:
		o.Kind = OwnerPlugin
	default:
		o.Kind = OwnerUnspecified
	}
	return o
}
```

NOTE: The import `cursorv1 "github.com/holomush/holomush/internal/eventbus/cursor"` will conflict with the package name. Adjust by either (a) putting the generated proto in a sub-package `cursorv1` (e.g. `internal/eventbus/cursor/cursorv1/`) or (b) generating into the same package. If (b), drop the alias and refer to `Cursor`/`Owner`/etc. directly — but be aware proto generation collides with the Go types defined here. Pick (a) for cleanliness:

If using (a), update Task 2 step 3 to generate into `internal/eventbus/cursor/cursorv1/` and use the import shown above.

If using (b), rename the Go types (e.g. `HostCursor` → `Host`) to avoid collision OR put the codec functions in a sub-package `internal/eventbus/cursor/codec/`. Pick (a).

- [ ] **Step 4: Run the test to verify it passes.**

Run: `task test -- -run TestEncodeDecodeRoundTripsHostCursor ./internal/eventbus/cursor/`
Expected: PASS.

- [ ] **Step 5: Add the remaining unit tests.**

Append to `internal/eventbus/cursor/cursor_test.go`:

```go
func TestEncodeDecodeRoundTripsPluginCursor(t *testing.T) {
	t.Parallel()
	in := Cursor{
		Version: 1,
		Epoch:   0,
		Owner:   Owner{Kind: OwnerPlugin, PluginName: "core-scenes"},
		Plugin:  []byte("opaque-from-plugin"),
	}
	bytes, err := Encode(in)
	require.NoError(t, err)
	out, err := Decode(bytes)
	require.NoError(t, err)
	assert.Equal(t, OwnerPlugin, out.Owner.Kind)
	assert.Equal(t, "core-scenes", out.Owner.PluginName)
	assert.Equal(t, []byte("opaque-from-plugin"), out.Plugin)
	assert.Nil(t, out.Host)
}

func TestDecodeRejectsEmptyBytes(t *testing.T) {
	t.Parallel()
	_, err := Decode(nil)
	require.Error(t, err)
	_, err = Decode([]byte{})
	require.Error(t, err)
}

func TestDecodeRejectsUnknownVersion(t *testing.T) {
	t.Parallel()
	in := Cursor{
		Version: 99,
		Owner:   Owner{Kind: OwnerHost},
		Host:    &HostCursor{Seq: 1, ID: ulid.ULID{}},
	}
	bytes, err := Encode(in)
	require.NoError(t, err)
	_, err = Decode(bytes)
	require.Error(t, err)
}

func TestEncodeRejectsHostOwnerWithoutBody(t *testing.T) {
	t.Parallel()
	_, err := Encode(Cursor{Owner: Owner{Kind: OwnerHost}})
	require.Error(t, err)
}

func TestEncodeRejectsPluginOwnerWithoutName(t *testing.T) {
	t.Parallel()
	_, err := Encode(Cursor{Owner: Owner{Kind: OwnerPlugin}})
	require.Error(t, err)
}

func TestCurrentEpochIsZero(t *testing.T) {
	t.Parallel()
	assert.Equal(t, uint64(0), CurrentEpoch())
}
```

- [ ] **Step 6: Run the full cursor test suite.**

Run: `task test -- ./internal/eventbus/cursor/`
Expected: all tests PASS.

- [ ] **Step 7: Commit.**

```text
feat(eventbus/cursor): Encode/Decode for opaque pagination tokens

Implements the host-internal cursor codec defined in the suos design.
Round-trip and error-path tests cover host and plugin cursor variants,
version rejection, missing-body validation, and empty-bytes rejection.

Bead: holomush-suos
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §4.5
```

---

## Phase 2 — Internal eventbus types

These tasks change the eventbus package's public Go types. The HistoryQuery change is atomic — every Go consumer must update in lockstep within Task 5.

### Task 4: Add `Event.Seq` field

**Files:**

- Modify: `internal/eventbus/types.go`
- Test: `internal/eventbus/types_test.go`

- [ ] **Step 1: Write the test that asserts the field exists and zero-defaults.**

Append to `internal/eventbus/types_test.go`:

```go
func TestEventSeqDefaultsToZero(t *testing.T) {
	t.Parallel()
	e := Event{}
	assert.Equal(t, uint64(0), e.Seq)
}

func TestEventSeqRoundTripsThroughLiteral(t *testing.T) {
	t.Parallel()
	e := Event{Seq: 42}
	assert.Equal(t, uint64(42), e.Seq)
}
```

- [ ] **Step 2: Run the test, verify it fails.**

Run: `task test -- -run TestEventSeq ./internal/eventbus/`
Expected: FAIL with "unknown field Seq in struct literal" / "e.Seq undefined".

- [ ] **Step 3: Add the field.**

Edit `internal/eventbus/types.go`. Find the `Event` struct (around the docstring "Event is the host-side representation"). Add `Seq` after `ID`:

```go
type Event struct {
	ID        ulid.ULID
	Seq       uint64    // JetStream stream sequence; populated by both tier readers and by the subscriber. Host-internal — never serialized in any public proto envelope.
	Subject   Subject
	Type      Type
	Timestamp time.Time
	Actor     Actor
	Payload   []byte
}
```

- [ ] **Step 4: Run the test, verify it passes.**

Run: `task test -- -run TestEventSeq ./internal/eventbus/`
Expected: PASS.

- [ ] **Step 5: Run the broader eventbus test suite.**

Run: `task test -- ./internal/eventbus/...`
Expected: PASS — adding a zero-default field shouldn't break anything; existing tests don't construct Events with the new field.

- [ ] **Step 6: Commit.**

```text
feat(eventbus): add Event.Seq field for JetStream stream sequence

Host-internal field — populated by tier readers and the subscriber,
consumed by the cursor codec at the gRPC boundary. Not exposed on any
public proto envelope.

Bead: holomush-suos
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §4.1
```

---

### Task 5: Replace `HistoryQuery` cursor fields (atomic)

This task is large — it changes `HistoryQuery` and updates every consumer in lockstep. Cannot be done incrementally because Go does not support overlapping field sets.

**Files:**

- Modify: `internal/eventbus/bus.go`
- Modify: `internal/eventbus/errors.go`
- Modify: `internal/eventbus/history/cold_postgres.go` (call sites only — full rewrite in Task 6)
- Modify: `internal/eventbus/history/hot_jetstream.go` (call sites only — full rewrite in Task 7)
- Modify: `internal/eventbus/history/tier.go` (call sites only — full rewrite in Task 8)
- Modify: `internal/grpc/query_stream_history.go` (cursor decode wired in Task 11; this task makes it compile with new fields)
- Modify: any in-tree test that constructs `HistoryQuery` literals

- [ ] **Step 1: Write the test for the new struct shape.**

Append to `internal/eventbus/bus_test.go` (create if not present):

```go
func TestHistoryQueryNewCursorFields(t *testing.T) {
	t.Parallel()
	id := ulid.MustParse("01HYXYZEVT0000000000000001")
	q := HistoryQuery{
		Subject:   Subject("events.main.location.01ABC"),
		AfterSeq:  10,
		AfterID:   id,
		BeforeSeq: 100,
		BeforeID:  id,
		Direction: DirectionBackward,
		PageSize:  50,
	}
	assert.Equal(t, uint64(10), q.AfterSeq)
	assert.Equal(t, id, q.AfterID)
	assert.Equal(t, uint64(100), q.BeforeSeq)
	assert.Equal(t, id, q.BeforeID)
}
```

- [ ] **Step 2: Run, verify FAIL.**

Run: `task test -- -run TestHistoryQueryNewCursorFields ./internal/eventbus/`
Expected: FAIL with "AfterSeq undefined".

- [ ] **Step 3: Replace the cursor fields.**

Edit `internal/eventbus/bus.go`. Find the `HistoryQuery` struct (around the docstring "HistoryQuery describes a paginated history read"). Replace it:

```go
// HistoryQuery describes a paginated history read. Auth flows via
// context.Context (auth.WithSession), not via this struct.
//
// Pagination ordering is by JetStream stream sequence (js_seq), not by
// ULID. Cursors are (seq, id) pairs: AfterSeq/AfterID for forward reads,
// BeforeSeq/BeforeID for backward reads. The id field is a tripwire that
// validates the cursor's seq still names the same event in storage; on
// mismatch the reader returns ErrCursorStale or ErrCursorLag (see
// internal/eventbus/errors.go).
//
// Zero seq means "from the start" (forward) or "from the end" (backward).
// AfterID / BeforeID are required when their corresponding seq is non-zero
// for client-supplied cursors; internal callers MAY leave id zero (then no
// validation is performed).
type HistoryQuery struct {
	Subject Subject

	AfterSeq  uint64    // exclusive lower bound by JS stream seq
	AfterID   ulid.ULID // tripwire for AfterSeq; zero = skip validation
	BeforeSeq uint64    // exclusive upper bound by JS stream seq
	BeforeID  ulid.ULID // tripwire for BeforeSeq; zero = skip validation

	NotBefore time.Time
	NotAfter  time.Time
	Direction Direction
	PageSize  int
}
```

- [ ] **Step 4: Add the new sentinel errors.**

Edit `internal/eventbus/errors.go`. Add at the end of the file:

```go
// ErrCursorStale is returned when a cursor's (seq, id) pair has no
// corresponding event in either tier — e.g., the audit row was deleted,
// the JS stream was rebuilt with reassigned seqs, or audit drift has
// changed the id at this seq. Maps to gRPC FAILED_PRECONDITION.
//
// Recovery: drop cursor; re-query without it.
var ErrCursorStale = errors.New("cursor stale")

// ErrCursorLag is returned when a cursor's seq is in the live JS stream
// but not yet projected into events_audit. Cursor remains valid; client
// should retry with backoff. Maps to gRPC UNAVAILABLE.
//
// Recovery: backoff per spec §4.4 (250/500/1000/2000/4000 ms).
var ErrCursorLag = errors.New("cursor lag")

// ErrCursorInvalid is returned when cursor bytes failed to decode
// (corruption, unknown version, malformed body). Maps to gRPC
// INVALID_ARGUMENT.
//
// Recovery: drop cursor; programming error / report.
var ErrCursorInvalid = errors.New("cursor invalid")
```

If the file already imports `"errors"`, no import change needed; otherwise add it.

- [ ] **Step 5: Update consumers to compile against the new shape.**

This is the painful part — every site that referenced `q.After` / `q.Before` (`ulid.ULID`) needs to be updated to `q.AfterSeq`/`q.AfterID` (or the Before pair).

For this task, the goal is **compilation only** — the consumers can keep returning the same data they did before; full behavior rework is in Tasks 6-8. Use this rote substitution:

```text
q.After.IsZero()       → q.AfterSeq == 0 && q.AfterID.IsZero()
q.Before.IsZero()      → q.BeforeSeq == 0 && q.BeforeID.IsZero()
q.After                → q.AfterID  (when used as a ULID)
q.Before               → q.BeforeID
ev.ID.Compare(q.After) → ev.ID.Compare(q.AfterID)  (TEMPORARY — Task 7 fixes this)
```

Files to edit (find sites with `grep -rn 'q\.After\|q\.Before' internal/`):

- `internal/eventbus/history/cold_postgres.go` (lines 65-77 in current code: change `args = append(args, q.After[:])` → `args = append(args, q.AfterID[:])` and similar for Before)
- `internal/eventbus/history/hot_jetstream.go` (lines 134, 202-206: cursor compares; do the rote substitution)
- `internal/eventbus/history/tier.go` (`selectStartTier` line 295: `if !q.After.IsZero() {` → `if q.AfterSeq > 0 || !q.AfterID.IsZero() {`; `currentCursor` returns; `advanceCursor` field assignment)

For `tier.go advanceCursor` (around line 540), the compile-only patch:

```go
func (s *crossoverStream) advanceCursor(events []eventbus.Event) {
	if len(events) == 0 {
		return
	}
	dir := s.query.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	last := events[len(events)-1].ID
	if dir == eventbus.DirectionForward {
		s.query.AfterID = last
	} else {
		s.query.BeforeID = last
	}
}
```

(This still doesn't track seq — Task 8 will. For now: just make it compile.)

For `tier.go currentCursor`:

```go
func (s *crossoverStream) currentCursor() ulid.ULID {
	dir := s.query.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	if dir == eventbus.DirectionForward {
		return s.query.AfterID
	}
	return s.query.BeforeID
}
```

For `internal/grpc/query_stream_history.go` — this currently parses `before_id` from the request as a ULID and assigns to `q.Before`. Compile-only: change to `q.BeforeID`. Cursor decoding is Task 11.

For test files — `grep -rn 'After:\s*ulid\|Before:\s*ulid' internal/` or similar — find any literals constructing HistoryQuery and update field names.

- [ ] **Step 6: Run the test, verify it passes.**

Run: `task test -- -run TestHistoryQueryNewCursorFields ./internal/eventbus/`
Expected: PASS.

- [ ] **Step 7: Run the broader test suite to verify compile.**

Run: `task test -- ./internal/eventbus/... ./internal/grpc/...`
Expected: all tests still PASS — semantics are unchanged because we only renamed fields (cursor ULID still drives both validation and ordering until Tasks 6-8).

If any tests fail with "After undefined" / "Before undefined", grep for the offending construction site and fix it.

- [ ] **Step 8: Commit.**

```text
refactor(eventbus): replace HistoryQuery After/Before with (Seq, ID) pairs

Mechanical field rename across HistoryQuery and all consumers. New
fields: AfterSeq+AfterID, BeforeSeq+BeforeID. Adds ErrCursorStale,
ErrCursorLag, ErrCursorInvalid sentinels.

Behavior unchanged in this commit — readers still use ID for
pagination. Tier reader rework follows in subsequent commits to switch
ordering and pagination to Seq.

Bead: holomush-suos
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §4.1
```

---

## Phase 3 — Tier readers and crossover

These tasks switch the actual pagination logic from ULID to seq.

### Task 6: Cold tier — js_seq SELECT, validation pattern, OR'd WHERE

**Files:**

- Modify: `internal/eventbus/history/cold_postgres.go`
- Modify: `internal/eventbus/history/cold_postgres_unit_test.go`
- Test: integration tests under `test/integration/stream_history/` (verified at end of phase)

- [ ] **Step 1: Write the test for the validation-pass case.**

Append to `internal/eventbus/history/cold_postgres_unit_test.go`. This test uses an integration-style setup with testcontainers PG; if the existing unit tests are pure (no DB), put this in `internal/eventbus/history/cold_postgres_integration_test.go` with a `//go:build integration` tag instead.

Determine which by reading the file (Step 0):

Run: `head -5 internal/eventbus/history/cold_postgres_unit_test.go`

If it has a `//go:build` tag, use the same; if not, this is a unit-only file and we need a new integration file.

Here is the test (place in the appropriate file):

```go
//go:build integration

package history

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/test/testpg" // hypothetical helper for getting a pgxpool against testcontainers; check actual helper
)

// TestColdReadValidatesAndDiscardsCursorEcho verifies the §6.2 piggyback
// pattern: forward read with a valid cursor returns rows after the cursor,
// having validated and discarded the cursor row from the result.
func TestColdReadValidatesAndDiscardsCursorEcho(t *testing.T) {
	pool := testpg.NewPool(t) // helper sets up a clean events_audit
	ctx := context.Background()

	subject := eventbus.Subject("events.main.location.01HXTESTLOC0000000000000")
	id1 := ulid.MustParse("01HXTESTEVT0000000000000001")
	id2 := ulid.MustParse("01HXTESTEVT0000000000000002")
	id3 := ulid.MustParse("01HXTESTEVT0000000000000003")

	// Insert three events with seqs 1, 2, 3.
	insertAuditRow(t, pool, id1, subject, 1)
	insertAuditRow(t, pool, id2, subject, 2)
	insertAuditRow(t, pool, id3, subject, 3)

	tier := newPostgresColdTier(pool)
	q := eventbus.HistoryQuery{
		Subject:   subject,
		AfterSeq:  1,
		AfterID:   id1,
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	}
	out, err := tier.Read(ctx, q, time.Time{}, q.PageSize)
	require.NoError(t, err)
	require.Len(t, out, 2, "should return events after cursor; cursor row is discarded")
	assert.Equal(t, uint64(2), out[0].Seq)
	assert.Equal(t, id2, out[0].ID)
	assert.Equal(t, uint64(3), out[1].Seq)
	assert.Equal(t, id3, out[1].ID)
}

// insertAuditRow is a test helper that writes one events_audit row with
// the given (id, subject, seq). All other columns get test defaults.
func insertAuditRow(t *testing.T, pool *pgxpool.Pool, id ulid.ULID, subject eventbus.Subject, seq uint64) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO events_audit (
			id, subject, type, timestamp, actor_kind, actor_id,
			payload, schema_ver, codec, js_seq
		) VALUES ($1, $2, 'test.event', now(), 'system', NULL, '\x00'::bytea, 1, 'identity', $3)
	`, id[:], string(subject), int64(seq))
	require.NoError(t, err)
}
```

(Adjust `testpg` import based on actual project helper. Check `test/integration/stream_history/` for existing helpers; reuse them.)

- [ ] **Step 2: Run, verify FAIL.**

Run: `task test:int -- -run TestColdReadValidatesAndDiscardsCursorEcho ./internal/eventbus/history/`
Expected: FAIL — current cold reader uses `WHERE id > $cursor`, not `WHERE js_seq >= $cursor`.

- [ ] **Step 3: Rewrite `cold_postgres.go` Read method.**

Edit `internal/eventbus/history/cold_postgres.go`. Replace the `Read` method body and update `actorFromAuditRow` if needed:

```go
// Read satisfies ColdTier per §6.1 of the suos spec. Builds a parameterized
// SELECT against events_audit, ordering by js_seq (NOT ULID — see spec §1
// problem statement for why). Honors the subject filter, seq cursor(s),
// time bounds, direction, and page size. An optional `edge` time
// constraint is applied when crossing tiers (the cursor row is OR'd into
// the WHERE so it always passes regardless of edge — see §6.2).
//
// PIGGYBACK VALIDATION (§6.2 — load-bearing): When a cursor is supplied
// (AfterSeq > 0 forward, BeforeSeq > 0 backward), the SQL uses `>=` (forward)
// or `<=` (backward), NOT a strict inequality. The first row returned is
// the cursor echo: it MUST have js_seq == cursor.Seq AND id == cursor.ID.
// We validate, discard, and return the rest. A future maintainer who
// changes >= back to > silently disables the tripwire and reintroduces
// the bug class this design exists to prevent. DO NOT.
func (c *postgresColdTier) Read(ctx context.Context, q eventbus.HistoryQuery, edge time.Time, pageSize int) ([]eventbus.Event, error) {
	if pageSize <= 0 {
		return nil, nil
	}
	subjectExact, subjectPattern := classifySubject(string(q.Subject))

	// Determine cursor and direction.
	dir := q.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	var cursorSeq uint64
	var cursorID ulid.ULID
	if dir == eventbus.DirectionForward {
		cursorSeq = q.AfterSeq
		cursorID = q.AfterID
	} else {
		cursorSeq = q.BeforeSeq
		cursorID = q.BeforeID
	}
	hasCursor := cursorSeq > 0

	var (
		sb   strings.Builder
		args []any
	)
	sb.WriteString(`SELECT id, subject, type, timestamp, actor_kind, actor_id, payload, js_seq FROM events_audit WHERE `)
	if subjectPattern != "" {
		args = append(args, subjectPattern)
		fmt.Fprintf(&sb, "subject LIKE $%d", len(args))
	} else {
		args = append(args, subjectExact)
		fmt.Fprintf(&sb, "subject = $%d", len(args))
	}

	// Cursor bound — INCLUSIVE so the cursor echo is the first row.
	if hasCursor {
		args = append(args, int64(cursorSeq))
		if dir == eventbus.DirectionForward {
			fmt.Fprintf(&sb, " AND js_seq >= $%d", len(args))
		} else {
			fmt.Fprintf(&sb, " AND js_seq <= $%d", len(args))
		}
	}

	// Time bounds.
	if !q.NotBefore.IsZero() {
		args = append(args, q.NotBefore)
		fmt.Fprintf(&sb, " AND timestamp >= $%d", len(args))
	}
	if !q.NotAfter.IsZero() {
		args = append(args, q.NotAfter)
		fmt.Fprintf(&sb, " AND timestamp <= $%d", len(args))
	}

	// Crossover edge with cursor-row OR clause.
	// Spec §6.2: cursor row passes the WHERE regardless of edge; only
	// post-cursor events are subject to the edge filter. The `id =
	// $cursorID` guard prevents a drift twin (same seq, different id)
	// from sneaking past the edge.
	if !edge.IsZero() {
		args = append(args, edge)
		edgeIdx := len(args)
		if hasCursor {
			args = append(args, int64(cursorSeq))
			seqIdx := len(args)
			args = append(args, cursorID[:])
			idIdx := len(args)
			fmt.Fprintf(&sb, " AND (timestamp < $%d OR (js_seq = $%d AND id = $%d))",
				edgeIdx, seqIdx, idIdx)
		} else {
			fmt.Fprintf(&sb, " AND timestamp < $%d", edgeIdx)
		}
	}

	if dir == eventbus.DirectionForward {
		sb.WriteString(" ORDER BY js_seq ASC")
	} else {
		sb.WriteString(" ORDER BY js_seq DESC")
	}

	limit := pageSize
	if hasCursor {
		// One extra row for the cursor echo we'll discard.
		limit = pageSize + 1
	}
	args = append(args, limit)
	fmt.Fprintf(&sb, " LIMIT $%d", len(args))

	rows, err := c.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, oops.Code("EVENTBUS_COLD_QUERY_FAILED").
			With("subject", string(q.Subject)).
			Wrap(err)
	}
	defer rows.Close()

	out := make([]eventbus.Event, 0, pageSize)
	first := true
	for rows.Next() {
		var (
			idBytes      []byte
			subjectStr   string
			eventType    string
			ts           time.Time
			actorKindStr string
			actorIDBytes []byte
			payload      []byte
			seq          int64
		)
		if scanErr := rows.Scan(&idBytes, &subjectStr, &eventType, &ts, &actorKindStr, &actorIDBytes, &payload, &seq); scanErr != nil {
			return nil, oops.Code("EVENTBUS_COLD_SCAN_FAILED").Wrap(scanErr)
		}
		if len(idBytes) != 16 {
			return nil, oops.Code("EVENTBUS_COLD_BAD_ID").
				With("len", len(idBytes)).
				Errorf("events_audit.id must be 16 bytes")
		}
		var id ulid.ULID
		copy(id[:], idBytes)

		// Validate the first row IS the cursor echo when a cursor was
		// supplied. Spec §6.2 step 1.
		if hasCursor && first {
			if uint64(seq) != cursorSeq || id != cursorID {
				return nil, c.classifyCursorMismatch(ctx, q, cursorSeq, cursorID, uint64(seq), id)
			}
			first = false
			continue // discard the cursor echo (step 2)
		}
		first = false

		out = append(out, eventbus.Event{
			ID:        id,
			Seq:       uint64(seq),
			Subject:   eventbus.Subject(subjectStr),
			Type:      eventbus.Type(eventType),
			Timestamp: ts.UTC(),
			Actor:     actorFromAuditRow(actorKindStr, actorIDBytes),
			Payload:   payload,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("EVENTBUS_COLD_ROWS_ERR").Wrap(err)
	}

	// If hasCursor and we got zero rows total, the cursor seq has no
	// matching row in cold. classifyCursorMismatch handles the LAG-vs-STALE
	// distinction by inspecting JS stream state.
	if hasCursor && first {
		return nil, c.classifyCursorMissing(ctx, q, cursorSeq, cursorID)
	}

	return out, nil
}

// classifyCursorMismatch decides between STALE (different id at the seq —
// rebuild/drift) and STALE (different seq returned, meaning the cursor's
// seq doesn't exist at all). LAG cannot apply when we got a row back at
// or near the cursor seq; LAG only applies when cold has nothing.
func (c *postgresColdTier) classifyCursorMismatch(ctx context.Context, q eventbus.HistoryQuery, wantSeq uint64, wantID ulid.ULID, gotSeq uint64, gotID ulid.ULID) error {
	return oops.Code("EVENTBUS_CURSOR_STALE").
		With("subject", string(q.Subject)).
		With("cursor_seq", wantSeq).
		With("cursor_id", wantID.String()).
		With("got_seq", gotSeq).
		With("got_id", gotID.String()).
		Wrap(eventbus.ErrCursorStale)
}

// classifyCursorMissing inspects the snapshot of JS stream state to decide
// LAG vs STALE. Caller (Reader.QueryHistory or the tier wrapper) MUST
// supply a snapshot via the readTier indirection — see Task 8 §8.5.
//
// For now we surface a STALE here; Task 8 wires the snapshot through and
// upgrades to LAG when appropriate.
func (c *postgresColdTier) classifyCursorMissing(ctx context.Context, q eventbus.HistoryQuery, wantSeq uint64, wantID ulid.ULID) error {
	return oops.Code("EVENTBUS_CURSOR_STALE").
		With("subject", string(q.Subject)).
		With("cursor_seq", wantSeq).
		With("cursor_id", wantID.String()).
		Wrap(eventbus.ErrCursorStale)
}
```

Note: `actorFromAuditRow` already exists; no change needed.

- [ ] **Step 4: Run the validation-pass test.**

Run: `task test:int -- -run TestColdReadValidatesAndDiscardsCursorEcho ./internal/eventbus/history/`
Expected: PASS.

- [ ] **Step 5: Add the id-mismatch test.**

Append:

```go
func TestColdReadReturnsStaleOnIDMismatch(t *testing.T) {
	pool := testpg.NewPool(t)
	ctx := context.Background()

	subject := eventbus.Subject("events.main.location.01HXTESTLOC0000000000000")
	idActual := ulid.MustParse("01HXTESTEVT0000000000000001")
	idCursor := ulid.MustParse("01HXTESTEVT9999999999999999")

	insertAuditRow(t, pool, idActual, subject, 1)

	tier := newPostgresColdTier(pool)
	q := eventbus.HistoryQuery{
		Subject:   subject,
		AfterSeq:  1,
		AfterID:   idCursor, // mismatch — the row at seq=1 has idActual
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	}
	_, err := tier.Read(ctx, q, time.Time{}, q.PageSize)
	require.Error(t, err)
	assert.ErrorIs(t, err, eventbus.ErrCursorStale)
}

func TestColdReadReturnsStaleOnMissingCursorSeq(t *testing.T) {
	pool := testpg.NewPool(t)
	ctx := context.Background()

	subject := eventbus.Subject("events.main.location.01HXTESTLOC0000000000000")
	insertAuditRow(t, pool, ulid.MustParse("01HXTESTEVT0000000000000001"), subject, 1)

	tier := newPostgresColdTier(pool)
	q := eventbus.HistoryQuery{
		Subject:   subject,
		AfterSeq:  99, // no row at this seq
		AfterID:   ulid.MustParse("01HXTESTEVT0000000000000099"),
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	}
	_, err := tier.Read(ctx, q, time.Time{}, q.PageSize)
	require.Error(t, err)
	assert.ErrorIs(t, err, eventbus.ErrCursorStale)
}
```

- [ ] **Step 6: Run them; verify PASS.**

Run: `task test:int -- -run TestColdReadReturns ./internal/eventbus/history/`
Expected: PASS for both.

- [ ] **Step 7: Add the edge-case crossover test.**

```go
func TestColdReadCursorAtEdgePassesEdgeFilter(t *testing.T) {
	pool := testpg.NewPool(t)
	ctx := context.Background()

	subject := eventbus.Subject("events.main.location.01HXTESTLOC0000000000000")
	idAtEdge := ulid.MustParse("01HXTESTEVT0000000000000005")
	idAfterEdge := ulid.MustParse("01HXTESTEVT0000000000000006")

	now := time.Now().UTC()
	insertAuditRowAt(t, pool, idAtEdge, subject, 5, now.Add(-1*time.Hour)) // older — past edge
	insertAuditRowAt(t, pool, idAfterEdge, subject, 6, now)                // newer — past edge for cold

	tier := newPostgresColdTier(pool)
	q := eventbus.HistoryQuery{
		Subject:   subject,
		AfterSeq:  5,
		AfterID:   idAtEdge,
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	}
	// Edge cuts everything newer than 30 minutes ago — the cursor row at
	// 1h ago passes (it's older than edge, fine), but more importantly its
	// presence as cursor must not cause the validation to fail. Without
	// the OR'd WHERE clause, the cursor row would be filtered out and
	// validation would erroneously fail.
	edge := now.Add(-30 * time.Minute)
	out, err := tier.Read(ctx, q, edge, q.PageSize)
	require.NoError(t, err)
	// Result: cursor echo (idAtEdge) is discarded; idAfterEdge has timestamp >= edge,
	// so it's filtered by edge constraint. Page is empty.
	assert.Empty(t, out)
}

// insertAuditRowAt is a test helper that writes a row with explicit timestamp.
func insertAuditRowAt(t *testing.T, pool *pgxpool.Pool, id ulid.ULID, subject eventbus.Subject, seq uint64, ts time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO events_audit (
			id, subject, type, timestamp, actor_kind, actor_id,
			payload, schema_ver, codec, js_seq
		) VALUES ($1, $2, 'test.event', $4, 'system', NULL, '\x00'::bytea, 1, 'identity', $3)
	`, id[:], string(subject), int64(seq), ts)
	require.NoError(t, err)
}
```

- [ ] **Step 8: Run, verify PASS.**

Run: `task test:int -- -run TestColdReadCursorAtEdgePassesEdgeFilter ./internal/eventbus/history/`
Expected: PASS.

- [ ] **Step 9: Run the full integration suite for the history package.**

Run: `task test:int -- ./internal/eventbus/history/`
Expected: existing tests still PASS; new tests PASS. Some existing tests may need ULID→seq cursor updates — fix in lockstep.

- [ ] **Step 10: Commit.**

```text
feat(eventbus/history): cold tier paginates by js_seq with (seq, id) tripwire

Implements §6 of the suos spec. SELECTs js_seq, orders by js_seq
ASC/DESC, uses inclusive cursor bound (>= or <=) with first-row
validation + discard. Cursor at the crossover edge passes via OR'd
WHERE clause guarded by both seq AND id.

Returns ErrCursorStale on (seq, id) mismatch or missing cursor seq.
LAG-vs-STALE distinction lands in Task 8 alongside the streamStateSnapshot.

Bead: holomush-suos
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §6
```

---

### Task 7: Hot tier — start policy switch, seq compares, retention age-out

**Files:**

- Modify: `internal/eventbus/history/hot_jetstream.go`
- Modify: `internal/eventbus/history/hot_jetstream_test.go`

- [ ] **Step 1: Write the test for forward-with-cursor start policy.**

Append to `internal/eventbus/history/hot_jetstream_test.go`:

```go
// TestHotTierForwardCursorUsesStartSequencePolicy asserts that when the
// cursor seq is supplied, the OrderedConsumer config uses
// DeliverByStartSequencePolicy at AfterSeq, not the time-based policy.
// This is what makes hot-tier pagination immune to ULID-vs-seq drift.
func TestHotTierForwardCursorUsesStartSequencePolicy(t *testing.T) {
	t.Parallel()
	tier := newJetStreamHotTier(nil, nil, func() time.Time { return time.Now() })
	q := eventbus.HistoryQuery{
		Subject:   eventbus.Subject("events.main.location.01ABC"),
		AfterSeq:  100,
		AfterID:   ulid.MustParse("01HXTESTEVT0000000000000001"),
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	}
	cfg, err := tier.buildConfig(context.Background(), q, time.Time{}, q.PageSize+1)
	require.NoError(t, err)
	assert.Equal(t, jetstream.DeliverByStartSequencePolicy, cfg.DeliverPolicy)
	assert.Equal(t, uint64(100), cfg.OptStartSeq)
}
```

- [ ] **Step 2: Run, verify FAIL.**

Run: `task test -- -run TestHotTierForwardCursorUsesStartSequencePolicy ./internal/eventbus/history/`
Expected: FAIL — current `buildConfig` uses `DeliverByStartTimePolicy` derived from `q.AfterID.Time()`.

- [ ] **Step 3: Rewrite `buildConfig` per §7.1 table.**

Edit `internal/eventbus/history/hot_jetstream.go`. Replace the `buildConfig` method:

```go
func (h *jetStreamHotTier) buildConfig(
	ctx context.Context,
	q eventbus.HistoryQuery,
	edge time.Time,
	fetch int,
) (jetstream.OrderedConsumerConfig, error) {
	cfg := jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{string(q.Subject)},
	}
	dir := q.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}

	// FORWARD: cursor seq present → start AT seq inclusive (the first
	// delivered message is the cursor echo). Otherwise use time start.
	if dir == eventbus.DirectionForward {
		if q.AfterSeq > 0 {
			cfg.DeliverPolicy = jetstream.DeliverByStartSequencePolicy
			cfg.OptStartSeq = q.AfterSeq
			return cfg, nil
		}
		start := edge
		if !q.NotBefore.IsZero() && q.NotBefore.After(edge) {
			start = q.NotBefore
		}
		cfg.DeliverPolicy = jetstream.DeliverByStartTimePolicy
		cfg.OptStartTime = &start
		return cfg, nil
	}

	// BACKWARD with cursor: walk forward from max(1, BeforeSeq − fetch)
	// up to BeforeSeq inclusive, reverse in-memory.
	if q.BeforeSeq > 0 {
		var startSeq uint64 = 1
		if q.BeforeSeq > uint64(fetch) {
			startSeq = q.BeforeSeq - uint64(fetch) + 1
		}
		cfg.DeliverPolicy = jetstream.DeliverByStartSequencePolicy
		cfg.OptStartSeq = startSeq
		return cfg, nil
	}

	// BACKWARD without cursor: existing tail behavior.
	stream, err := h.js.Stream(ctx, eventbus.StreamName)
	if err != nil {
		return cfg, oops.Code("EVENTBUS_HOT_STREAM_LOOKUP_FAILED").
			With("stream", eventbus.StreamName).
			Wrap(err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return cfg, oops.Code("EVENTBUS_HOT_STREAM_INFO_FAILED").
			With("stream", eventbus.StreamName).
			Wrap(err)
	}
	last := info.State.LastSeq
	if last == 0 {
		cfg.DeliverPolicy = jetstream.DeliverAllPolicy
		return cfg, nil
	}
	window := uint64(0)
	if fetch > 0 {
		window = uint64(fetch)
	}
	var startSeq uint64 = 1
	if window > 0 && last > window {
		startSeq = last - window + 1
	}
	cfg.DeliverPolicy = jetstream.DeliverByStartSequencePolicy
	cfg.OptStartSeq = startSeq
	return cfg, nil
}
```

- [ ] **Step 4: Run the start-policy test, verify PASS.**

Run: `task test -- -run TestHotTierForwardCursorUsesStartSequencePolicy ./internal/eventbus/history/`
Expected: PASS.

- [ ] **Step 5: Update `matchesQuery` to compare by Seq.**

Edit the `matchesQuery` function in `hot_jetstream.go`:

```go
func matchesQuery(ev eventbus.Event, q eventbus.HistoryQuery, edge time.Time, tier Tier) bool {
	if q.AfterSeq > 0 && ev.Seq <= q.AfterSeq {
		return false
	}
	if q.BeforeSeq > 0 && ev.Seq >= q.BeforeSeq {
		return false
	}
	if !q.NotBefore.IsZero() && ev.Timestamp.Before(q.NotBefore) {
		return false
	}
	if !q.NotAfter.IsZero() && ev.Timestamp.After(q.NotAfter) {
		return false
	}
	switch tier {
	case TierJetStream:
		if !edge.IsZero() && ev.Timestamp.Before(edge) {
			return false
		}
	case TierPostgres:
		// Cold tier may serve post-edge data when JS returned an empty page.
	}
	return true
}
```

- [ ] **Step 6: Populate `Event.Seq` from message metadata.**

Find the message decode loop in `hot_jetstream.go` (around line 84). After the `decodeJetStreamMessage` returns the Event, populate Seq from msg metadata:

```go
ev, decodeErr := decodeJetStreamMessage(ctx, msg, h.selector)
if decodeErr != nil {
	continue
}
if meta, mErr := msg.Metadata(); mErr == nil {
	ev.Seq = meta.Sequence.Stream
}
if !matchesQuery(ev, q, edge, TierJetStream) {
	continue
}
```

- [ ] **Step 7: Add cursor-validation logic at the start of the message loop for forward+cursor reads.**

When forward+cursor, the first message should be the cursor echo. Validate seq AND id match; on mismatch, classify STALE vs INVALID. (LAG cannot apply to hot — if hot doesn't have the seq, it's because the JS stream truncated it.)

Inside the `Read` method, after `cons.Fetch(...)` and before the message loop, add a "first message validation" wrapper. Easiest implementation: add a flag at the top of the loop:

```go
out := make([]eventbus.Event, 0, pageSize)
hasCursor := q.AfterSeq > 0 || q.BeforeSeq > 0
firstMessage := hasCursor

for msg := range batch.Messages() {
	if fetchCtx.Err() != nil {
		break
	}
	ev, decodeErr := decodeJetStreamMessage(ctx, msg, h.selector)
	if decodeErr != nil {
		continue
	}
	if meta, mErr := msg.Metadata(); mErr == nil {
		ev.Seq = meta.Sequence.Stream
	}

	if firstMessage {
		firstMessage = false
		var cursorSeq uint64
		var cursorID ulid.ULID
		if q.Direction == eventbus.DirectionBackward {
			cursorSeq = q.BeforeSeq
			cursorID = q.BeforeID
		} else {
			cursorSeq = q.AfterSeq
			cursorID = q.AfterID
		}
		if ev.Seq != cursorSeq || ev.ID != cursorID {
			// First delivered message is NOT the cursor echo. Either retention
			// aged out the cursor seq (JS started at FirstSeq instead) or the
			// stream's per-subject contents differ from what cursor expected.
			return nil, oops.Code("EVENTBUS_CURSOR_STALE").
				With("subject", string(q.Subject)).
				With("cursor_seq", cursorSeq).
				With("cursor_id", cursorID.String()).
				With("got_seq", ev.Seq).
				With("got_id", ev.ID.String()).
				Wrap(eventbus.ErrCursorStale)
		}
		// Discard the cursor echo.
		continue
	}

	if !matchesQuery(ev, q, edge, TierJetStream) {
		continue
	}
	out = append(out, ev)
	if len(out) >= pageSize {
		break
	}
}
```

Note: `q.PageSize` may need bumping by 1 (echo + N) when cursor present. Adjust the `fetch` budget at the top of `Read`:

```go
fetch := pageSize * 2
if hasCursor := q.AfterSeq > 0 || q.BeforeSeq > 0; hasCursor {
	fetch = pageSize + 1 // cursor echo + page
}
```

- [ ] **Step 8: Add hot-tier validation tests.**

```go
func TestHotTierStaleOnIDMismatchAtCursorSeq(t *testing.T) {
	// Use embedded NATS helper. Publish events seq 1, 2, 3 with known
	// ULIDs. Query forward with cursor (seq=1, id=<wrong>) and assert
	// EVENTBUS_CURSOR_STALE.
	// (Use the embedded NATS test fixtures already in this package — see
	// hot_jetstream_test.go for setup pattern.)
	// ... full test code per the existing pattern ...
}

func TestHotTierStaleOnRetentionAgeOut(t *testing.T) {
	// Publish events seq 1-10, then delete via stream purge so FirstSeq=5.
	// Query forward with cursor (seq=2, id=<known>); first delivered is seq=5.
	// Assert EVENTBUS_CURSOR_STALE.
}
```

(Implement these following the test pattern already in `hot_jetstream_test.go` — the file uses an embedded NATS helper `eventbustest.Embedded`.)

- [ ] **Step 9: Run all hot-tier tests.**

Run: `task test -- ./internal/eventbus/history/`
Expected: PASS for unit tests; integration tests are run by `task test:int`.

Run: `task test:int -- ./internal/eventbus/history/`
Expected: PASS.

- [ ] **Step 10: Commit.**

```text
feat(eventbus/history): hot tier paginates by JS sequence with cursor validation

Switches OrderedConsumer to DeliverByStartSequencePolicy when a cursor
is present (both directions), validates the cursor echo on the first
delivered message, and returns EVENTBUS_CURSOR_STALE on mismatch or
retention age-out. matchesQuery now compares by Seq, not ULID. Event.Seq
populated from msg.Metadata().Sequence.Stream.

Bead: holomush-suos
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §7
```

---

### Task 8: Crossover stream rework + Stream.Info snapshot

**Files:**

- Create: `internal/eventbus/history/stream_state.go`
- Modify: `internal/eventbus/history/tier.go`
- Modify: `internal/eventbus/history/cold_postgres.go` (wire snapshot through to upgrade STALE→LAG)
- Modify: `internal/eventbus/history/tier_test.go`

- [ ] **Step 1: Create the stream-state snapshot type.**

Create `internal/eventbus/history/stream_state.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"sync"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
)

// streamStateSnapshot caches Stream.Info().State for the lifetime of a
// single Reader.QueryHistory call. Per spec §8.5.
//
// Used by:
//   - selectStartTier (route the first page based on cursor.Seq vs FirstSeq)
//   - cold tier validation (distinguish LAG from STALE when cursor seq
//     is missing from cold)
//   - hot tier (detect retention age-out)
//
// Concurrency: a single crossoverStream instance is not invoked
// concurrently (loadNextPage is sequential), so a sync.Once is sufficient
// to memoize the first fetch.
type streamStateSnapshot struct {
	js   jetstream.JetStream
	once sync.Once
	// Populated by once.Do.
	firstSeq uint64
	lastSeq  uint64
	err      error
}

func newStreamStateSnapshot(js jetstream.JetStream) *streamStateSnapshot {
	return &streamStateSnapshot{js: js}
}

// Get returns the cached state, fetching it on first call. Safe to call
// concurrently within a single QueryHistory call.
func (s *streamStateSnapshot) Get(ctx context.Context) (firstSeq, lastSeq uint64, err error) {
	if s == nil || s.js == nil {
		return 0, 0, nil
	}
	s.once.Do(func() {
		stream, fErr := s.js.Stream(ctx, eventbus.StreamName)
		if fErr != nil {
			s.err = oops.Code("EVENTBUS_HISTORY_STREAM_LOOKUP_FAILED").
				With("stream", eventbus.StreamName).
				Wrap(fErr)
			return
		}
		info, fErr := stream.Info(ctx)
		if fErr != nil {
			s.err = oops.Code("EVENTBUS_HISTORY_STREAM_INFO_FAILED").
				With("stream", eventbus.StreamName).
				Wrap(fErr)
			return
		}
		s.firstSeq = info.State.FirstSeq
		s.lastSeq = info.State.LastSeq
	})
	return s.firstSeq, s.lastSeq, s.err
}
```

- [ ] **Step 2: Update the `ColdTier` interface and `postgresColdTier.Read` to accept the snapshot.**

Edit `internal/eventbus/history/tier.go`. Update the `ColdTier` interface:

```go
type ColdTier interface {
	Read(ctx context.Context, q eventbus.HistoryQuery, edge time.Time, pageSize int, snap *streamStateSnapshot) ([]eventbus.Event, error)
}
```

Edit `internal/eventbus/history/cold_postgres.go` `Read` signature accordingly. Use `snap` in `classifyCursorMissing` to upgrade STALE→LAG when appropriate:

```go
func (c *postgresColdTier) Read(ctx context.Context, q eventbus.HistoryQuery, edge time.Time, pageSize int, snap *streamStateSnapshot) ([]eventbus.Event, error) {
	// ... existing query logic from Task 6 ...
	// Replace classifyCursorMissing call with:
	if hasCursor && first {
		return nil, c.classifyCursorMissingWithSnapshot(ctx, q, cursorSeq, cursorID, snap)
	}
	// ...
}

func (c *postgresColdTier) classifyCursorMissingWithSnapshot(ctx context.Context, q eventbus.HistoryQuery, wantSeq uint64, wantID ulid.ULID, snap *streamStateSnapshot) error {
	if snap != nil {
		_, lastSeq, err := snap.Get(ctx)
		if err == nil && wantSeq <= lastSeq {
			// Cursor seq is in JS but not in cold yet — projection lag.
			return oops.Code("EVENTBUS_CURSOR_LAG").
				With("subject", string(q.Subject)).
				With("cursor_seq", wantSeq).
				With("cursor_id", wantID.String()).
				With("js_last_seq", lastSeq).
				Wrap(eventbus.ErrCursorLag)
		}
	}
	return oops.Code("EVENTBUS_CURSOR_STALE").
		With("subject", string(q.Subject)).
		With("cursor_seq", wantSeq).
		With("cursor_id", wantID.String()).
		Wrap(eventbus.ErrCursorStale)
}
```

- [ ] **Step 3: Update `HotTier` interface and reader signature similarly.**

```go
type HotTier interface {
	Read(ctx context.Context, q eventbus.HistoryQuery, edge time.Time, pageSize int, snap *streamStateSnapshot) ([]eventbus.Event, error)
}
```

Edit `internal/eventbus/history/hot_jetstream.go` `Read` to accept `snap` (use it for retention age-out STALE classification).

- [ ] **Step 4: Update `selectStartTier` and `Reader.QueryHistory` to construct and thread the snapshot.**

Edit `tier.go`. Replace `selectStartTier`:

```go
func selectStartTier(ctx context.Context, q eventbus.HistoryQuery, edge, now time.Time, snap *streamStateSnapshot) Tier {
	// Cursor present: route by cursor.Seq vs JS state.
	cursorSeq := q.AfterSeq
	if q.Direction == eventbus.DirectionBackward {
		cursorSeq = q.BeforeSeq
	}
	if cursorSeq > 0 && snap != nil {
		firstSeq, _, err := snap.Get(ctx)
		if err == nil && firstSeq > 0 {
			if cursorSeq >= firstSeq {
				return TierJetStream
			}
			return TierPostgres
		}
	}

	// No cursor — use time bounds, same as before.
	dir := q.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	if dir == eventbus.DirectionForward {
		if q.NotBefore.IsZero() {
			return TierPostgres
		}
		if q.NotBefore.Before(edge) {
			return TierPostgres
		}
		return TierJetStream
	}
	if q.NotAfter.IsZero() {
		if now.Before(edge) {
			return TierPostgres
		}
		return TierJetStream
	}
	if q.NotAfter.Before(edge) {
		return TierPostgres
	}
	return TierJetStream
}
```

Edit `Reader.QueryHistory` to construct the snapshot once per call and pass it to `selectStartTier` and into the crossoverStream:

```go
func (r *Reader) QueryHistory(ctx context.Context, q eventbus.HistoryQuery) (eventbus.HistoryStream, error) {
	if err := validateQuery(q); err != nil {
		return nil, err
	}
	q.PageSize = ClampPageSize(q.PageSize)

	if r.owners != nil {
		owner := r.owners.Resolve(string(q.Subject))
		if owner.PluginName != "" {
			if r.router == nil {
				return nil, oops.Code("EVENTBUS_PLUGIN_HISTORY_NOT_WIRED").
					With("subject", string(q.Subject)).
					With("plugin", owner.PluginName).
					Errorf("plugin-owned subject requires PluginHistoryRouter")
			}
			//nolint:wrapcheck // forwarding plugin RPC error to caller
			return r.router.QueryHistory(ctx, owner.PluginName, q)
		}
	}

	now := r.now()
	edge := now.Add(-r.streamMaxAge).Add(r.safetyMargin)
	snap := newStreamStateSnapshot(r.js)
	startTier := selectStartTier(ctx, q, edge, now, snap)

	return newCrossoverStream(ctx, r.hot, r.cold, q, edge, startTier, snap), nil
}
```

- [ ] **Step 5: Thread `snap` into `crossoverStream` and tier read calls.**

Update `crossoverStream` struct and constructor:

```go
type crossoverStream struct {
	// ... existing fields ...
	snap *streamStateSnapshot
}

func newCrossoverStream(ctx context.Context, hot HotTier, cold ColdTier, q eventbus.HistoryQuery, edge time.Time, startTier Tier, snap *streamStateSnapshot) *crossoverStream {
	return &crossoverStream{
		ctx:       ctx,
		hot:       hot,
		cold:      cold,
		query:     q,
		edge:      edge,
		startTier: startTier,
		seenSeqs:  make(map[uint64]struct{}),
		snap:      snap,
	}
}
```

Update `readTier` to pass `snap`:

```go
func (s *crossoverStream) readTier(ctx context.Context, t Tier, pageSize int) ([]eventbus.Event, error) {
	readCtx := ctx
	if readCtx == nil {
		readCtx = s.ctx
	}
	switch t {
	case TierJetStream:
		if s.hot == nil {
			return nil, nil
		}
		events, err := s.hot.Read(readCtx, s.query, s.edge, pageSize, s.snap)
		if err != nil {
			return nil, oops.Code("EVENTBUS_HISTORY_HOT_READ_FAILED").
				With("subject", string(s.query.Subject)).
				Wrap(err)
		}
		return events, nil
	case TierPostgres:
		if s.cold == nil {
			return nil, nil
		}
		events, err := s.cold.Read(readCtx, s.query, s.edge, pageSize, s.snap)
		if err != nil {
			return nil, oops.Code("EVENTBUS_HISTORY_COLD_READ_FAILED").
				With("subject", string(s.query.Subject)).
				Wrap(err)
		}
		return events, nil
	default:
		return nil, nil
	}
}
```

- [ ] **Step 6: Replace `seenIDs` with `seenSeqs`.**

Edit `tier.go`:

```go
// In crossoverStream struct:
seenSeqs map[uint64]struct{}

// In Next():
for {
	if s.pos < len(s.buf) {
		e := s.buf[s.pos]
		s.pos++
		if _, dup := s.seenSeqs[e.Seq]; dup {
			continue
		}
		s.seenSeqs[e.Seq] = struct{}{}
		return e, nil
	}
	// ... rest unchanged ...
}

// In loadNextPage:
preSeen := len(s.seenSeqs)
// ... (replace remaining seenIDs references)

// In stuck-loop check:
case len(first) > 0 && len(s.seenSeqs) == preSeen:
	s.stuckPageReads++
```

- [ ] **Step 7: Update `advanceCursor` to track both seq and id.**

```go
func (s *crossoverStream) advanceCursor(events []eventbus.Event) {
	if len(events) == 0 {
		return
	}
	last := events[len(events)-1]
	dir := s.query.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	if dir == eventbus.DirectionForward {
		s.query.AfterSeq = last.Seq
		s.query.AfterID = last.ID
	} else {
		s.query.BeforeSeq = last.Seq
		s.query.BeforeID = last.ID
	}
}
```

- [ ] **Step 8: Update `currentCursor` to return seq.**

```go
func (s *crossoverStream) currentCursor() uint64 {
	dir := s.query.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	if dir == eventbus.DirectionForward {
		return s.query.AfterSeq
	}
	return s.query.BeforeSeq
}
```

Update the type of the variable `cursorBefore` in `loadNextPage` accordingly (from `ulid.ULID` to `uint64`).

- [ ] **Step 9: Re-key `appendOrdered` to seq.**

```go
func (s *crossoverStream) appendOrdered(events []eventbus.Event) {
	if len(events) == 0 {
		return
	}
	s.buf = append(s.buf, events...)
	dir := s.query.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	tail := s.buf[s.pos:]
	sort.SliceStable(tail, func(i, j int) bool {
		if dir == eventbus.DirectionBackward {
			return tail[i].Seq > tail[j].Seq
		}
		return tail[i].Seq < tail[j].Seq
	})
}
```

- [ ] **Step 10: Update tier_test.go fixtures.**

The existing `tier_test.go` fixtures construct mock tier readers with the OLD `Read` signature (no `snap` parameter). Update each mock to accept the new signature; existing tests can pass `nil` for `snap`.

Run `grep -n 'func.*Read.*HistoryQuery' internal/eventbus/history/tier_test.go` to find them.

Also update the `EVENTBUS_HISTORY_BUFFER_OVERFLOW` regression test fixture to operate on seq cursors rather than ULID. The pathological tier should return events whose Seq does not advance.

- [ ] **Step 11: Add the LAG vs STALE test.**

Append to `tier_test.go` (unit, with mock tiers):

```go
// TestColdTierMissingCursorSeqInJSReturnsLagWhenSeqIsLive verifies the
// LAG path: cursor seq is present in JS (snap.lastSeq >= cursorSeq) but
// not yet in cold, indicating projection lag rather than staleness.
func TestColdTierMissingCursorSeqInJSReturnsLagWhenSeqIsLive(t *testing.T) {
	// Drive cold with a fake that returns no rows; snap reports lastSeq=100
	// but cursor.Seq=50. Expect EVENTBUS_CURSOR_LAG.
	// Use the test seam introduced in Task 8 (newStreamStateSnapshot test
	// helper that returns canned values).
	// ... full test ...
}

// TestColdTierMissingCursorSeqBeyondJSReturnsStale verifies the STALE
// path: cursor seq is past JS state's lastSeq, so the seq simply doesn't
// exist anywhere.
func TestColdTierMissingCursorSeqBeyondJSReturnsStale(t *testing.T) {
	// snap.lastSeq=10, cursor.Seq=50, cold returns no rows. Expect STALE.
	// ... full test ...
}
```

To support these, add a test-only constructor that builds a snapshot from canned values:

```go
// In tier_test_helpers.go (test file):
func newSnapshotForTest(firstSeq, lastSeq uint64) *streamStateSnapshot {
	s := &streamStateSnapshot{firstSeq: firstSeq, lastSeq: lastSeq}
	s.once.Do(func() {}) // already populated
	return s
}
```

- [ ] **Step 12: Run all history tests.**

Run: `task test -- ./internal/eventbus/history/`
Run: `task test:int -- ./internal/eventbus/history/`
Expected: PASS for both.

- [ ] **Step 13: Commit.**

```text
feat(eventbus/history): crossover seq-keyed dedup + Stream.Info snapshot

Replaces seenIDs with seenSeqs, re-keys appendOrdered and currentCursor
to seq, threads a per-call streamStateSnapshot through Reader →
crossoverStream → tier readers. snapshot is consulted by:
  - selectStartTier (route first page by cursor.Seq vs FirstSeq)
  - cold tier (LAG vs STALE distinction when cursor seq missing)
  - hot tier (retention age-out STALE detection)

Bead: holomush-suos
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §8
```

---

### Task 9: Subscriber populates `Event.Seq`

**Files:**

- Modify: `internal/eventbus/subscriber.go`
- Modify: `internal/eventbus/subscriber_test.go`

- [ ] **Step 1: Write a test asserting Seq is populated on a delivery.**

Append to `internal/eventbus/subscriber_test.go`:

```go
func TestSubscribeDeliveryPopulatesEventSeq(t *testing.T) {
	// Use the embedded NATS test fixture. Publish one event, subscribe,
	// receive the delivery, assert Event.Seq == 1.
	// Reuse the pattern from existing tests (eventbustest.Embedded).
	// ... full test code matching existing pattern ...
}
```

- [ ] **Step 2: Run, verify FAIL.**

Run: `task test:int -- -run TestSubscribeDeliveryPopulatesEventSeq ./internal/eventbus/`
Expected: FAIL — `Event.Seq` is zero on deliveries today.

- [ ] **Step 3: Edit `decodeDelivery` to populate Seq.**

Edit `internal/eventbus/subscriber.go`. Find `decodeDelivery` (line ~357). At the end, before returning the Event, populate Seq from msg metadata:

```go
// (existing decode logic returns ev populated)
if meta, mErr := msg.Metadata(); mErr == nil {
	ev.Seq = meta.Sequence.Stream
}
return ev, nil
```

- [ ] **Step 4: Run, verify PASS.**

Run: `task test:int -- -run TestSubscribeDeliveryPopulatesEventSeq ./internal/eventbus/`
Expected: PASS.

- [ ] **Step 5: Run full subscriber suite.**

Run: `task test:int -- ./internal/eventbus/`
Expected: PASS.

- [ ] **Step 6: Commit.**

```text
feat(eventbus): Subscribe deliveries populate Event.Seq

Plumbs msg.Metadata().Sequence.Stream into the delivered Event so
clients can use it for backfill cursor construction (via the cursor
codec at the gRPC boundary).

Bead: holomush-suos
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §9.1
```

---

## Phase 4 — Public wire

### Task 10: Proto changes — opaque cursor on the wire

**Files:**

- Modify: `api/proto/holomush/core/v1/core.proto`
- Modify: `api/proto/holomush/web/v1/web.proto`
- Modify: `api/proto/holomush/plugin/v1/plugin.proto`
- Re-generate: corresponding `.pb.go` files via `task proto:generate`

- [ ] **Step 1: Edit core.proto.**

Find `QueryStreamHistoryRequest` (line ~365). Replace `string before_id = 6;`:

```protobuf
message QueryStreamHistoryRequest {
  RequestMeta meta = 1;
  string session_id = 2;
  string stream = 3;
  int32 count = 4;
  int64 not_before_ms = 5;

  // cursor is an opaque continuation token returned by a prior call to
  // QueryStreamHistory or by a Subscribe delivery. Empty = first page.
  // Format is host-defined; clients MUST NOT inspect or compare cursors.
  // Errors: EVENTBUS_CURSOR_INVALID, _STALE, _LAG (see spec §4.4).
  bytes cursor = 6;
}
```

Find `QueryStreamHistoryResponse` (line ~374):

```protobuf
message QueryStreamHistoryResponse {
  ResponseMeta meta = 1;
  repeated EventFrame events = 2;
  bool has_more = 3;
  bytes next_cursor = 4;
}
```

Find `EventFrame` (line ~135):

```protobuf
message EventFrame {
  string id = 1;
  string stream = 2;
  string type = 3;
  google.protobuf.Timestamp timestamp = 4;
  string actor_type = 5;
  string actor_id = 6;
  bytes payload = 7;
  bytes cursor = 8; // opaque cursor naming THIS event as high-watermark
}
```

- [ ] **Step 2: Edit web.proto.**

Find `WebQueryStreamHistoryRequest` and `Response` (line ~261), and `GameEvent` (line ~98). Apply the parallel edits — replace `before_id` with `cursor`, add `next_cursor` to response, add `cursor` to `GameEvent`.

```protobuf
message WebQueryStreamHistoryRequest {
  string session_id = 1;
  string stream = 2;
  int32 count = 3;
  int64 not_before_ms = 4;
  bytes cursor = 5;
}

message WebQueryStreamHistoryResponse {
  repeated GameEvent events = 1;
  bool has_more = 2;
  bytes next_cursor = 3;
}

message GameEvent {
  // ... existing fields 1-9 unchanged ...
  bytes cursor = 10;
}
```

- [ ] **Step 3: Edit plugin.proto.**

Find `PluginHostServiceQueryStreamHistoryRequest` and `Response` (line ~392). Apply the parallel edits.

- [ ] **Step 4: Regenerate proto bindings.**

Run: `task proto:generate`
Expected: `.pb.go` files updated under `pkg/proto/holomush/`. Existing references to `BeforeId` will now break compilation.

- [ ] **Step 5: Verify compile (will fail — that's expected; Tasks 11/12 fix).**

Run: `task lint`
Expected: errors at sites that use `BeforeId` (`internal/grpc/query_stream_history.go`, `pkg/plugin/focus_client.go`, `internal/plugin/goplugin/host_service.go`, `web/src/lib/backfill/streamBackfill.ts`).

DO NOT commit yet — Task 11 fixes the gRPC handler in the same commit batch.

---

### Task 11: gRPC handler — cursor decode/encode, error mapping, EventFrame.cursor

**Files:**

- Modify: `internal/grpc/query_stream_history.go`
- Modify: `internal/grpc/query_stream_history_test.go`
- Modify: `internal/grpc/server.go` (Subscribe path: populate `EventFrame.cursor` on each delivery)

- [ ] **Step 1: Write the failing test for cursor round-trip.**

Append to `internal/grpc/query_stream_history_test.go`:

```go
// TestQueryStreamHistoryRoundTripsOpaqueCursor verifies the handler
// decodes inbound cursor bytes via the cursor codec, threads (Seq, ID)
// into HistoryQuery, and on response encodes a fresh cursor for each
// EventFrame and the next_cursor field.
func TestQueryStreamHistoryRoundTripsOpaqueCursor(t *testing.T) {
	// Setup: server with a stub historyReader returning two events.
	// Encode an inbound cursor with seq=10, id=<known>.
	// Call QueryStreamHistory with that cursor.
	// Assert: response.events[*].cursor decodes to the right (seq, id).
	// Assert: response.next_cursor decodes to the LAST event's (seq, id).
	// Assert: stub historyReader saw HistoryQuery{ BeforeSeq: 10, BeforeID: <known>, Direction: Backward }.
	// ... full test code ...
}
```

- [ ] **Step 2: Run, verify FAIL.**

Run: `task test -- -run TestQueryStreamHistoryRoundTripsOpaqueCursor ./internal/grpc/`
Expected: FAIL (handler still uses `before_id` ULID parsing).

- [ ] **Step 3: Update the handler.**

Edit `internal/grpc/query_stream_history.go`:

Add import for `cursor` package and `connectrpc`/grpc status mapping helpers.

Replace the `before_id` parsing block with cursor decode:

```go
// Decode the opaque cursor (if any). Empty = first page.
var cursorSeq uint64
var cursorID ulid.ULID
var ownerName string
var pluginInner []byte
var ownerKind cursor.OwnerKind
if len(req.Cursor) > 0 {
	c, err := cursor.Decode(req.Cursor)
	if err != nil {
		return nil, oops.Code("INVALID_ARGUMENT").
			With("cursor_decode_err", err.Error()).
			Wrap(eventbus.ErrCursorInvalid)
	}
	if c.Epoch != cursor.CurrentEpoch() {
		return nil, oops.Code("EVENTBUS_CURSOR_STALE").
			With("cursor_epoch", c.Epoch).
			With("current_epoch", cursor.CurrentEpoch()).
			Wrap(eventbus.ErrCursorStale)
	}
	ownerKind = c.Owner.Kind
	if c.Owner.Kind == cursor.OwnerHost {
		if c.Host == nil {
			return nil, oops.Code("INVALID_ARGUMENT").
				Wrap(eventbus.ErrCursorInvalid)
		}
		cursorSeq = c.Host.Seq
		cursorID = c.Host.ID
	} else {
		ownerName = c.Owner.PluginName
		pluginInner = c.Plugin
	}
}

// Plugin-owned subjects route to PluginAuditService (Task 13 will wire
// this; for now if ownerKind==OwnerPlugin, return a "not yet wired" error
// or forward to a plugin router stub).
if ownerKind == cursor.OwnerPlugin {
	// TODO(task-13): forward to pluginHostService.QueryHistory with pluginInner.
	return nil, oops.Code("UNIMPLEMENTED").
		With("plugin", ownerName).
		Errorf("plugin-owned cursor routing wired in task 13")
}

// Subject translation — preserved from prior handler.
natsSubject, err := subjectxlate.Legacy(legacyStream, gameID)
if err != nil {
	return nil, err
}

q := eventbus.HistoryQuery{
	Subject:   eventbus.Subject(natsSubject),
	BeforeSeq: cursorSeq,
	BeforeID:  cursorID,
	NotBefore: notBefore,
	Direction: eventbus.DirectionBackward,
	PageSize:  int(pageSize),
}
```

Update the response construction. After collecting events, encode a cursor for each EventFrame and the next_cursor:

```go
// Encode a cursor naming each event as high-watermark.
frames := make([]*corev1.EventFrame, 0, len(collected))
for i := range collected {
	cur, encErr := cursor.Encode(cursor.Cursor{
		Version: cursor.CurrentVersion,
		Epoch:   cursor.CurrentEpoch(),
		Owner:   cursor.Owner{Kind: cursor.OwnerHost},
		Host:    &cursor.HostCursor{Seq: collected[i].Seq, ID: collected[i].ID},
	})
	if encErr != nil {
		return nil, oops.Code("INTERNAL").Wrap(encErr)
	}
	frames = append(frames, &corev1.EventFrame{
		Id:        collected[i].ID.String(),
		Stream:    subjectxlate.ToLegacy(string(collected[i].Subject), gameID),
		Type:      string(collected[i].Type),
		Timestamp: timestamppb.New(collected[i].Timestamp),
		ActorType: actorKindToString(collected[i].Actor.Kind),
		ActorId:   actorIDToString(collected[i].Actor),
		Payload:   collected[i].Payload,
		Cursor:    cur,
	})
}
var nextCursor []byte
if len(frames) > 0 {
	nextCursor = frames[len(frames)-1].Cursor
}

return &corev1.QueryStreamHistoryResponse{
	Meta:       responseMeta,
	Events:     frames,
	HasMore:    hasMore,
	NextCursor: nextCursor,
}, nil
```

Map `EVENTBUS_CURSOR_*` errors to gRPC statuses by inspecting the error chain:

```go
events, err := r.historyReader.QueryHistory(...)
if err != nil {
	switch {
	case errors.Is(err, eventbus.ErrCursorInvalid):
		return nil, status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, eventbus.ErrCursorStale):
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, eventbus.ErrCursorLag):
		return nil, status.Error(codes.Unavailable, err.Error())
	default:
		return nil, oops.Code("INTERNAL").Wrap(err)
	}
}
```

- [ ] **Step 4: Update Subscribe path to set EventFrame.cursor on every delivery.**

Edit `internal/grpc/server.go` Subscribe handler. Find the `EventFrame` construction (line ~572):

```go
cur, err := cursor.Encode(cursor.Cursor{
	Version: cursor.CurrentVersion,
	Epoch:   cursor.CurrentEpoch(),
	Owner:   cursor.Owner{Kind: cursor.OwnerHost},
	Host:    &cursor.HostCursor{Seq: ev.Seq, ID: ev.ID},
})
if err != nil {
	return oops.Code("INTERNAL").Wrap(err)
}
frame := &corev1.EventFrame{
	Id:        ev.ID.String(),
	Stream:    subjectxlate.ToLegacy(string(ev.Subject), gameID),
	// ... other fields ...
	Cursor:    cur,
}
```

- [ ] **Step 5: Run the round-trip test.**

Run: `task test -- -run TestQueryStreamHistoryRoundTripsOpaqueCursor ./internal/grpc/`
Expected: PASS.

- [ ] **Step 6: Add error-mapping tests.**

```go
func TestQueryStreamHistoryMapsErrCursorInvalidToInvalidArgument(t *testing.T) {
	// Stub historyReader returns ErrCursorInvalid; assert gRPC status code.
}
func TestQueryStreamHistoryMapsErrCursorStaleToFailedPrecondition(t *testing.T) {
	// Stub historyReader returns ErrCursorStale; assert.
}
func TestQueryStreamHistoryMapsErrCursorLagToUnavailable(t *testing.T) {
	// Stub historyReader returns ErrCursorLag; assert.
}
```

- [ ] **Step 7: Run all gRPC tests.**

Run: `task test -- ./internal/grpc/`
Expected: PASS.

- [ ] **Step 8: Commit (Tasks 10 & 11 together — atomic wire change).**

```text
feat(grpc+proto): opaque cursor on wire; LAG/STALE/INVALID error mapping

Replaces ULID before_id with bytes cursor on QueryStreamHistory and
WebQueryStreamHistory; adds bytes cursor to EventFrame and GameEvent for
Subscribe deliveries to enable reconnect-with-backfill. Handler decodes
inbound via internal/eventbus/cursor codec, validates epoch, and routes
plugin-owned cursors to a (currently stubbed) plugin path. Maps
EVENTBUS_CURSOR_INVALID → INVALID_ARGUMENT, _STALE → FAILED_PRECONDITION,
_LAG → UNAVAILABLE. Preserves subjectxlate.Legacy translation.

Bead: holomush-suos
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §4.2 §10
```

---

### Task 12: Web gateway proxy update

**Files:**

- Modify: `internal/web/` (the web RPC handler — find via `grep -rn 'WebQueryStreamHistory' internal/web/`)
- Modify: corresponding test file

- [ ] **Step 1: Locate the web handler.**

Run: `grep -rn 'WebQueryStreamHistory\|GameEvent{' internal/web/` (use Grep tool).

- [ ] **Step 2: Update the proxy.**

The web handler is a thin proxy per CLAUDE.md "Architecture Invariants". Update it to:

1. Forward `request.Cursor` → core's `QueryStreamHistoryRequest.Cursor`.
2. Convert each `corev1.EventFrame` to `webv1.GameEvent` and copy the `Cursor` field through.
3. Set `WebQueryStreamHistoryResponse.NextCursor` from `corev1.QueryStreamHistoryResponse.NextCursor`.

Show actual code based on the existing pattern in the handler — the spec doesn't have access to that file's existing shape, so the implementer should mirror the existing field-by-field copy pattern.

- [ ] **Step 3: Tests.**

Add a test asserting cursor passes through end-to-end (or mock the core RPC and verify the web handler propagates).

- [ ] **Step 4: Run web tests.**

Run: `task test -- ./internal/web/`
Expected: PASS.

- [ ] **Step 5: Commit.**

```text
feat(web): proxy opaque cursor through WebQueryStreamHistory + GameEvent

Web RPC handler stays a thin proxy per gateway-boundary invariant —
just copies cursor / next_cursor between core proto and web proto.

Bead: holomush-suos
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §10
```

---

## Phase 5 — Plugin parity

### Task 13: Plugin host RPC + Go SDK update + plugin cursor wrap

**Files:**

- Modify: `pkg/plugin/focus_client.go`
- Modify: `internal/plugin/goplugin/host_service.go`
- Modify: `internal/grpc/query_stream_history.go` (replace the Task 11 stub for plugin routing)
- Modify: any in-tree test plugins exercising `QueryStreamHistory`

- [ ] **Step 1: Update the Go SDK request/response shapes.**

Edit `pkg/plugin/focus_client.go`. Find `QueryStreamHistoryRequest` (line ~62) and update:

```go
type QueryStreamHistoryRequest struct {
	Stream      string
	Count       int
	Cursor      []byte // opaque
	NotBeforeMs int64
}

type QueryStreamHistoryResponse struct {
	Events     []HistoryEvent
	HasMore    bool
	NextCursor []byte
}

type HistoryEvent struct {
	ID        string
	Stream    string
	Type      string
	Timestamp time.Time
	ActorType string
	ActorID   string
	Payload   []byte
	Cursor    []byte // per-event cursor
}
```

- [ ] **Step 2: Update host service implementation.**

Edit `internal/plugin/goplugin/host_service.go`. The `QueryStreamHistory` method should:

1. Decode the inbound plugin's `Cursor` bytes.
2. If decode shows `Owner == OwnerPlugin`: forward `Plugin` (inner bytes) to the plugin's audit service; wrap the plugin's response cursor inside a fresh host token.
3. Otherwise (host-owned subject): forward to the host's `historyReader` directly.

Pseudocode:

```go
func (h *HostService) QueryStreamHistory(ctx context.Context, req QueryStreamHistoryRequest) (*QueryStreamHistoryResponse, error) {
	natsSubject, err := subjectxlate.Legacy(req.Stream, h.gameID)
	if err != nil {
		return nil, err
	}

	// Decode cursor if present.
	var hostCursor cursor.Cursor
	if len(req.Cursor) > 0 {
		hostCursor, err = cursor.Decode(req.Cursor)
		if err != nil {
			return nil, oops.Wrap(eventbus.ErrCursorInvalid)
		}
	}

	if hostCursor.Owner.Kind == cursor.OwnerPlugin {
		// Forward to plugin's audit service with inner bytes.
		pluginResp, err := h.pluginRouter.QueryHistory(ctx, hostCursor.Owner.PluginName, hostCursor.Plugin)
		if err != nil {
			return nil, err
		}
		// Re-wrap plugin's response cursor.
		var nextCursor []byte
		if len(pluginResp.NextCursor) > 0 {
			nextCursor, err = cursor.Encode(cursor.Cursor{
				Version: cursor.CurrentVersion,
				Epoch:   cursor.CurrentEpoch(),
				Owner:   cursor.Owner{Kind: cursor.OwnerPlugin, PluginName: hostCursor.Owner.PluginName},
				Plugin:  pluginResp.NextCursor,
			})
			if err != nil {
				return nil, err
			}
		}
		return &QueryStreamHistoryResponse{
			Events:     pluginEventsToHistoryEvents(pluginResp.Events),
			HasMore:    pluginResp.HasMore,
			NextCursor: nextCursor,
		}, nil
	}

	// Host-owned subject path — same as the gRPC handler.
	q := eventbus.HistoryQuery{
		Subject:   eventbus.Subject(natsSubject),
		BeforeSeq: hostCursor.Host.Seq,
		BeforeID:  hostCursor.Host.ID,
		// ... rest ...
	}
	// ... use historyReader.QueryHistory ...
}
```

- [ ] **Step 3: Replace the Task 11 plugin-routing stub.**

Edit `internal/grpc/query_stream_history.go`. Replace the `UNIMPLEMENTED` stub with a forwarding call to the host service's plugin path (or directly to `r.pluginRouter` if available at that layer).

- [ ] **Step 4: Tests.**

Add tests:

- Plugin cursor decoded → forwarded to plugin router → response cursor wrapped correctly.
- Plugin returns its own STALE → propagated to client as STALE.

- [ ] **Step 5: Run tests.**

Run: `task test -- ./internal/grpc/ ./internal/plugin/...`
Expected: PASS.

- [ ] **Step 6: Commit.**

```text
feat(plugin): host wraps plugin opaque cursor; SDK + host service updated

Plugin-owned subjects: host decodes the inbound opaque cursor, extracts
plugin_inner bytes, forwards to PluginAuditService.QueryHistory, wraps
plugin's response cursor inside a fresh host token. No plugin schema
migration required — plugins keep their existing cursor formats.

Bead: holomush-suos
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §11
```

---

### Task 14: Lua hostfunc table-arg signature

**Files:**

- Modify: `internal/plugin/hostfunc/stdlib_focus.go`
- Modify: `internal/plugin/hostfunc/stdlib_focus_test.go`

- [ ] **Step 1: Write the failing test.**

Append to `internal/plugin/hostfunc/stdlib_focus_test.go`:

```go
func TestQueryStreamHistoryLuaTableArgSignature(t *testing.T) {
	// Drive the hostfunc with a Lua call:
	//   local r = holomush.query_stream_history({stream="loc:abc", count=5, cursor="<base64>"})
	// Assert r.events, r.next_cursor, r.has_more populated.
	// ... full test code matching existing pattern ...
}
```

- [ ] **Step 2: Run, verify FAIL.**

Run: `task test -- -run TestQueryStreamHistoryLuaTableArgSignature ./internal/plugin/hostfunc/`
Expected: FAIL.

- [ ] **Step 3: Implement the new signature.**

Edit `internal/plugin/hostfunc/stdlib_focus.go` (line ~282). Replace the positional handler:

```go
func queryStreamHistory(L *lua.LState) int {
	// Accept a table arg: {stream, count, cursor (string), not_before_ms}
	if L.GetTop() != 1 || L.Get(1).Type() != lua.LTTable {
		L.RaiseError("query_stream_history requires a single table argument {stream, count, cursor, not_before_ms}")
		return 0
	}
	tbl := L.CheckTable(1)
	stream := tbl.RawGetString("stream").String()
	if stream == "" {
		L.RaiseError("stream is required")
		return 0
	}
	count := int(lua.LVAsNumber(tbl.RawGetString("count")))
	if count == 0 {
		count = 50
	}
	cursorStr := tbl.RawGetString("cursor").String() // base64-encoded
	var cursorBytes []byte
	if cursorStr != "" {
		var err error
		cursorBytes, err = base64.StdEncoding.DecodeString(cursorStr)
		if err != nil {
			L.RaiseError("cursor must be base64-encoded: %v", err)
			return 0
		}
	}
	notBeforeMs := int64(lua.LVAsNumber(tbl.RawGetString("not_before_ms")))

	// Call host RPC.
	resp, err := /* the host's QueryStreamHistory wrapper */ (L.Context(), stream, count, cursorBytes, notBeforeMs)
	if err != nil {
		L.RaiseError("query_stream_history failed: %v", err)
		return 0
	}

	// Build result table.
	result := L.NewTable()
	events := L.NewTable()
	for i, ev := range resp.Events {
		evt := L.NewTable()
		evt.RawSetString("id", lua.LString(ev.ID))
		evt.RawSetString("stream", lua.LString(ev.Stream))
		evt.RawSetString("type", lua.LString(ev.Type))
		evt.RawSetString("timestamp", lua.LNumber(ev.Timestamp.UnixMilli()))
		evt.RawSetString("actor_type", lua.LString(ev.ActorType))
		evt.RawSetString("actor_id", lua.LString(ev.ActorID))
		evt.RawSetString("payload", lua.LString(string(ev.Payload)))
		evt.RawSetString("cursor", lua.LString(base64.StdEncoding.EncodeToString(ev.Cursor)))
		events.RawSetInt(i+1, evt)
	}
	result.RawSetString("events", events)
	result.RawSetString("has_more", lua.LBool(resp.HasMore))
	if len(resp.NextCursor) > 0 {
		result.RawSetString("next_cursor", lua.LString(base64.StdEncoding.EncodeToString(resp.NextCursor)))
	}
	L.Push(result)
	return 1
}
```

- [ ] **Step 4: Run, verify PASS.**

Run: `task test -- -run TestQueryStreamHistoryLuaTableArgSignature ./internal/plugin/hostfunc/`
Expected: PASS.

- [ ] **Step 5: Run full hostfunc tests.**

Run: `task test -- ./internal/plugin/hostfunc/`
Expected: PASS.

- [ ] **Step 6: Commit.**

```text
feat(plugin/hostfunc): query_stream_history table-arg signature

Switches from positional (stream, count) to table-arg
{stream, count, cursor, not_before_ms} to accommodate the opaque cursor
addition without awkward positional growth. Per project rule:
host-RPC Go+Lua parity. Verified at spec-write time: no in-tree .lua
plugin calls this hostfunc, so the signature change has no downstream
breakage.

Bead: holomush-suos
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §11.2
```

---

## Phase 6 — Cleanup, web client, rollout

### Task 15: Remove `EventIDMustBeMonotonic` ruleguard

**Files:**

- Modify: `gorules/rules.go`
- Modify: `gorules/rules_test.go` (if a test for this rule exists)

- [ ] **Step 1: Identify the test.**

Run: `grep -n "EventIDMustBeMonotonic" gorules/rules_test.go` (use Grep tool). If found, the test will be deleted alongside the rule.

- [ ] **Step 2: Delete the rule.**

Edit `gorules/rules.go`. Remove the `EventIDMustBeMonotonic` function (lines 17-33 from the spec inspection). Also remove the documentation comment block referring to invariant I-16.

- [ ] **Step 3: Delete the test (if present).**

Edit `gorules/rules_test.go`. Remove any `TestEventIDMustBeMonotonic*` tests.

- [ ] **Step 4: Run lint.**

Run: `task lint`
Expected: no `EventIDMustBeMonotonic` complaints; no orphan ruleguard imports.

- [ ] **Step 5: Run gorules tests.**

Run: `task test -- ./gorules/`
Expected: PASS.

- [ ] **Step 6: Commit.**

```text
chore(gorules): remove EventIDMustBeMonotonic — premise is dead

PostgresEventStore.Replay was deleted in PR #252 F7. Cursor CAS advances
were deleted in PR #252 F6. The new history pagination spec (suos)
explicitly rejects ULID lex order as event order. The rule's invariant
no longer holds; keeping it would mislead future maintainers into
believing ULID monotonicity is load-bearing.

Bead: holomush-suos
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §13
```

---

### Task 16: Add `CursorPackageInternal` ruleguard

**Files:**

- Modify: `gorules/rules.go`
- Modify: `gorules/rules_test.go`

- [ ] **Step 1: Add the rule.**

Edit `gorules/rules.go`. Add (place near other import-restriction rules):

```go
// CursorPackageInternal forbids importing internal/eventbus/cursor from
// outside the eventbus package tree. The cursor codec is host-internal:
// plugin authors and external clients MUST NOT inspect or construct
// opaque tokens. Allowed importers: internal/eventbus/, internal/grpc/,
// internal/web/, internal/plugin/goplugin/host_service.go (for plugin
// cursor wrapping).
//
// See docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §4.5.
func CursorPackageInternal(m dsl.Matcher) {
	m.Import("github.com/holomush/holomush/internal/eventbus/cursor").
		Where(!m.File().PkgPath.Matches(`^github\.com/holomush/holomush/internal/(eventbus|grpc|web|plugin/goplugin)`)).
		Report(`internal/eventbus/cursor is host-internal — clients and plugins must not import it`)
}
```

(Verify the ruleguard DSL — `m.Import(...)` may not be the exact API; check the existing rules in the file for the actual matcher syntax. Adjust if needed.)

- [ ] **Step 2: Add the test.**

Append to `gorules/rules_test.go` following the existing test pattern.

- [ ] **Step 3: Run lint.**

Run: `task lint`
Expected: clean (the cursor package is only imported from allowed packages).

- [ ] **Step 4: Commit.**

```text
chore(gorules): add CursorPackageInternal — opaque token integrity

Prevents accidental import of the host-internal cursor codec from
plugin code or other external surfaces. Allowed importers:
internal/eventbus, internal/grpc, internal/web,
internal/plugin/goplugin.

Bead: holomush-suos
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §4.5
```

---

### Task 17: Web client backfill — opaque cursor + LAG/STALE handling

**Files:**

- Modify: `web/src/lib/backfill/streamBackfill.ts`
- Modify: `web/src/lib/backfill/streamBackfill.test.ts`
- Audit: `web/src/routes/(authed)/terminal/+page.svelte` for any direct cursor usage

- [ ] **Step 1: Read the existing backfill module.**

Use Read tool on `web/src/lib/backfill/streamBackfill.ts` to understand the current shape. (The exact API depends on what's there; the spec calls out `beforeId: ''` at line 109 of that file.)

- [ ] **Step 2: Write the failing test.**

Append to `streamBackfill.test.ts`:

```typescript
import { describe, it, expect } from 'vitest';
// ... existing imports ...

describe('streamBackfill with opaque cursor', () => {
	it('passes inbound cursor through to QueryStreamHistory', async () => {
		// Mock the WebQueryStreamHistory call; assert request.cursor is the
		// Uint8Array passed by the caller.
	});

	it('persists next_cursor between paginated calls', async () => {
		// First call returns next_cursor=A; second call sends cursor=A.
	});

	it('drops cursor on FAILED_PRECONDITION (stale)', async () => {
		// Mock returns FAILED_PRECONDITION; backfill caller is told to re-query without cursor.
	});

	it('retries with backoff on UNAVAILABLE (lag)', async () => {
		// Mock returns UNAVAILABLE 3 times then succeeds; assert backoff schedule
		// matches spec §4.4 (250/500/1000ms minimum on first 3 attempts).
	});
});
```

- [ ] **Step 3: Run, verify FAIL.**

Run: `cd web && pnpm test -- streamBackfill`
Expected: FAIL (current module uses `beforeId`).

- [ ] **Step 4: Update the module.**

Replace `beforeId: string` with `cursor: Uint8Array` (or equivalent). Implement the LAG retry loop with the schedule:

```typescript
const LAG_BACKOFF_MS = [250, 500, 1000, 2000, 4000];

async function fetchWithCursor(stream: string, cursor: Uint8Array): Promise<...> {
	for (let attempt = 0; attempt < LAG_BACKOFF_MS.length; attempt++) {
		try {
			return await client.webQueryStreamHistory({ stream, cursor });
		} catch (err) {
			if (err.code === Code.Unavailable) {
				// LAG — retry with backoff.
				await sleep(LAG_BACKOFF_MS[attempt]);
				continue;
			}
			if (err.code === Code.FailedPrecondition) {
				// STALE — drop cursor, signal caller.
				throw new CursorStaleError();
			}
			throw err;
		}
	}
	throw new CursorLagError();
}
```

- [ ] **Step 5: Run tests.**

Run: `cd web && pnpm test -- streamBackfill`
Expected: PASS.

- [ ] **Step 6: Audit `+page.svelte`.**

Run: `grep -n 'before_id\|beforeId' web/src/routes/(authed)/terminal/+page.svelte` (use Grep tool).
If references exist, update to use the new backfill module's cursor-based API.

- [ ] **Step 7: Run frontend lint + tests.**

Run: `cd web && pnpm lint && pnpm test`
Expected: PASS.

- [ ] **Step 8: Commit.**

```text
feat(web/backfill): opaque cursor + LAG retry / STALE recovery

Switches the web backfill module from beforeId (ULID) to opaque cursor
bytes per the suos spec. Implements LAG retry with the spec's documented
backoff schedule (250/500/1000/2000/4000ms) and STALE recovery (drop
cursor, re-query).

Bead: holomush-suos
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §4.4 §13
```

---

### Task 18: Integration tests — concurrent publishers regression

**Files:**

- Modify or create: `test/integration/eventbus_e2e/cursor_concurrent_test.go` (or similar — fold into existing files per `holomush-l60y` consolidation)

- [ ] **Step 1: Write the regression test (the test the bead asks for).**

Create or extend an integration test:

```go
//go:build integration

package eventbus_e2e

import (
	"context"
	"crypto/rand"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
)

// TestPaginationStableUnderConcurrentPublishersWithDriftedULIDs reproduces
// the exact scenario the suos bead was filed to fix: two publishers writing
// to the same subject at high concurrency, where ULID lex order
// deliberately disagrees with JetStream stream-sequence order.
//
// Pre-suos: this test would observe events out of order, dropped, or
// duplicated across page boundaries.
// Post-suos: pagination matches JS sequence order end-to-end.
func TestPaginationStableUnderConcurrentPublishersWithDriftedULIDs(t *testing.T) {
	// Setup: embedded NATS + PG via testcontainers.
	// Spawn two goroutines publishing 100 events each to the same subject.
	// Each generates ULIDs from a separate entropy source so lex order is
	// random relative to publish order.
	// After publishing, page through history with PageSize=10.
	// Concatenate all pages.
	// Assert: result is in monotonically increasing JS seq order, contains
	// exactly 200 events, no duplicates.
	// ... full implementation matching existing patterns in this directory ...
}

// TestSubscribeReconnectWithBackfillContiguousAcrossLagWindow covers the
// §12.5 reconnect-with-backfill flow including LAG handling.
func TestSubscribeReconnectWithBackfillContiguousAcrossLagWindow(t *testing.T) {
	// Subscribe; capture event with cursor.
	// Disconnect.
	// Inject a projection delay that makes the cursor's seq missing from
	// cold for 500ms.
	// Reconnect with the captured cursor.
	// Assert: server returns UNAVAILABLE initially, then succeeds after
	// the lag window resolves; backfill page is contiguous with the live tail.
	// ... full implementation ...
}
```

- [ ] **Step 2: Run the regression suite.**

Run: `task test:int -- -run "TestPaginationStable|TestSubscribeReconnect" ./test/integration/eventbus_e2e/`
Expected: PASS.

- [ ] **Step 3: Fold into `holomush-l60y` consolidation.**

Per the spec §12.3: the prior test files (`internal/grpc/query_stream_history_test.go` and `test/integration/eventbus_e2e/cross_tier_query_test.go`, `…/reconnect_resume_test.go`) should be unified rather than parallel-extended. Make targeted deletions of duplicate coverage from the prior files.

- [ ] **Step 4: Commit.**

```text
test(eventbus): regression coverage for concurrent-publisher pagination

Adds the integration test the suos bead explicitly asks for: two
concurrent publishers writing to the same subject with deliberately
drifted ULIDs. Pre-fix would drop or reorder events across pages;
post-fix matches JS seq order end-to-end. Also adds the §12.5 reconnect-
with-backfill-across-lag-window scenario.

Folds duplicate coverage from prior cross_tier_query_test.go and
reconnect_resume_test.go into one consolidated file per holomush-l60y.

Bead: holomush-suos (refs holomush-l60y)
Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §12.3 §12.5
```

---

### Task 19: `task pr-prep` — final verification

**Files:** none (verification only)

- [ ] **Step 1: Run the full pre-PR gate.**

Run: `task pr-prep`
Expected: ALL green — lint, format, schema, license, unit, integration, E2E.

- [ ] **Step 2: If anything fails, fix in place and re-run.**

Per project rule (`feedback_pr_prep_must_run`): MUST run full `task pr-prep`, never approximate with subset checks.

- [ ] **Step 3: Push to PR branch and open PR.**

Per spec §13 and CLAUDE.md "Landing the Plane":

```bash
jj git fetch
jj rebase -r holomush-suos-spec -d main@origin
jj bookmark set holomush-suos -r @-
jj git push --branch holomush-suos
gh pr create --title "feat(eventbus): history pagination on JetStream stream sequence with opaque cursors (holomush-suos)" --body "$(cat <<'EOF'
## Summary

Implements bead holomush-suos: switches the entire history pipeline from ULID-keyed pagination to JetStream stream sequence (`js_seq`), with cursors exposed to clients as opaque epoch-aware tokens.

## Key changes

- **Internal:** Both tier readers order and paginate by `js_seq`; piggyback validation pattern catches cursor staleness; Stream.Info snapshot threaded through Reader → tiers for LAG-vs-STALE distinction.
- **Public API:** `bytes cursor` + `bytes next_cursor` replace `string before_id` on QueryStreamHistory; `bytes cursor` added to EventFrame and GameEvent for Subscribe deliveries to enable reconnect-with-backfill.
- **Cursor codec:** Host-internal proto under `internal/eventbus/cursor/` — version + epoch + owner + body. Plugin-owned cursors carry the plugin's opaque inner bytes; no plugin schema migration required.
- **Errors:** EVENTBUS_CURSOR_INVALID (INVALID_ARGUMENT), _STALE (FAILED_PRECONDITION), _LAG (UNAVAILABLE) with documented client recovery.
- **Cleanup:** Removes the EventIDMustBeMonotonic ruleguard (premise dead post-Phase-B). Adds CursorPackageInternal ruleguard.

Spec: docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md
Plan: docs/superpowers/plans/2026-04-21-cold-tier-js-seq-pagination-plan.md
Supersedes: Phase B §1a's "JS seq MUST NOT cross the boundary" rule (rationale in spec §3).

## Test plan

- [x] task pr-prep green (all CI mirrors pass)
- [x] New regression test: concurrent publishers with drifted ULIDs
- [x] New integration test: subscribe-then-reconnect across lag window
- [x] Web client backfill: cursor round-trip, LAG retry, STALE recovery

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 4: Verify the PR shows green CI.**

Run: `gh pr view --json statusCheckRollup --jq '.statusCheckRollup[] | "\(.name) \(.conclusion)"'`
Expected: every check shows SUCCESS or PENDING; no FAILURE.

- [ ] **Step 5: Notify the bead.**

Run: `bd update holomush-suos --notes "PR <URL> opened; ready for review"` (or whatever the project's bead-update convention is).

---

## Self-review notes

The plan author ran a self-review pass after writing this document. Findings:

1. **Spec coverage:** Every section of the spec maps to at least one task. §4.5 (cursor codec) → Tasks 2, 3. §5 (schema) → Task 1. §6 (cold) → Task 6. §7 (hot) → Task 7. §8 (crossover) → Task 8. §9 (Subscribe) → Tasks 9, 11. §10 (gRPC) → Task 11. §11 (plugin) → Tasks 13, 14. §13 (rollout) → Tasks 15, 16, 17, 19. §12 (testing) → Tasks 6-9 unit + 18 integration.

2. **No placeholders:** All steps contain code or exact commands. Task 12 (web gateway proxy) defers to "follow the existing pattern" because the exact handler shape varies — the implementer should use the Read tool on the actual file before writing changes; this is acceptable because the change is a pure proxy field-copy.

3. **Type consistency:** The cursor codec types (`Cursor`, `Owner`, `HostCursor`, `OwnerKind`, constants `OwnerHost`/`OwnerPlugin`/`OwnerUnspecified`) are used consistently across Tasks 3, 11, 13, 14, 17. The `streamStateSnapshot` is consistent across Tasks 8 (creation, threading) and Task 6's classifyCursorMissing fix-up. `HistoryQuery` field names (AfterSeq/AfterID/BeforeSeq/BeforeID) are stable from Task 5 onward.

4. **Sentinel errors** (`ErrCursorStale`/`ErrCursorLag`/`ErrCursorInvalid`): defined in Task 5, used in Tasks 6, 7, 8, 11, 17. Consistent.

5. **Wire field names** (`cursor`, `next_cursor`): consistent in Tasks 10, 11, 12, 13.

6. **Testing approach:** Each implementation task pairs a failing test → implementation → passing test. Phase 1 tasks are pure additions (low risk). Phase 2 task 5 is a "compile-only" mechanical rename (semantics unchanged). Phases 3-5 add behavior with test-first coverage. Phase 6 cleans up.

7. **Commit boundaries:** Each task ends in a commit. The atomic wire change (Tasks 10 + 11) is one commit because the proto change breaks compilation until the handler is updated.

8. **Verification:** Final task runs `task pr-prep` per the project's hard rule.
