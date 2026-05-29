<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Full-System Load & Performance Testing Harness — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a repeatable k6-based harness that drives the real telnet + Connect wire protocols against the full Dockerized `holomush` binary at N≈1,000 concurrent sessions, measuring comms-path SLOs and gating them in nightly CI.

**Architecture:** A custom `xk6 build` binary bundles two Go extensions — `xk6-connectrpc` (exact Connect protocol for the web tier) and an in-tree xk6 telnet module (raw TCP) — and runs a shared JS scenario that models a comms-heavy workload. say→broadcast and page/whisper→delivery latency are measured client-side via an emit-timestamp carried in the message payload (custom k6 `Trend`s). Thresholds are the pass/fail gate; results stream to the existing Prometheus/Grafana stack. The xk6 Go extensions live in a **separate Go module** rooted at `test/load/` (`test/load/go.mod`; extension packages under `test/load/xk6/`) so k6's dependency tree never enters the root `go.mod`.

**Tech Stack:** k6 + xk6 (Go extension toolchain), `connectrpc.com/connect` (Connect client inside the extension), Docker Compose (SUT stand-up, reusing `compose.yaml`), GitHub Actions (nightly), Taskfile, Go (xk6 modules + `test/meta` invariant tests).

**Spec:** `docs/superpowers/specs/2026-05-28-load-perf-testing-harness-design.md`
**Design bead:** holomush-ql7ef · **ADR:** holomush-evggu (k6 + xk6 tooling)

---

## File Structure

| Path | Responsibility |
| --- | --- |
| `test/load/go.mod` | Separate Go module for xk6 extensions — isolates k6 deps from root `go.mod` |
| `test/load/xk6/telnet/telnet.go` | xk6 raw-TCP telnet driver (dial, two-phase login, send command, read line) |
| `test/load/xk6/telnet/telnet_test.go` | Go unit tests for the telnet driver's framing/parse helpers |
| `test/load/xk6/connectrpc/` | Vendored or forked `xk6-connectrpc` (decided by Task 1) |
| `test/load/build/manifest.txt` | Pinned extension list + commits consumed by `xk6 build` |
| `test/load/build/Dockerfile.k6` | Reproducible custom-k6 build image (CI cache layer) |
| `test/load/lib/config.js` | Scenario knobs; env-driven N/duration/seed; verb-availability validation |
| `test/load/lib/workload.js` | Seeded action-mix selection (comms-heavy) |
| `test/load/lib/latency.js` | Emit-timestamp encode/decode + custom `Trend` recording |
| `test/load/lib/session.js` | Session lifecycle (connect/auth/join/loop/churn) per transport |
| `test/load/scenarios/comms.js` | Top-level scenario wiring lib/* into k6 executors + thresholds |
| `test/load/README.md` | How to build, run smoke/full, read dashboards, update baseline |
| `.benchmarks/load-baseline.json` | Tracked SLO baseline for relative regression gating |
| `.github/workflows/nightly-load.yml` | Nightly heavy run + threshold gate |
| `test/meta/load_harness_invariants_test.go` | Meta-tests for INV-LOAD-4 (exit-masking) + INV-LOAD-8 (not in pr-prep) |
| `Taskfile.yaml` | New `loadtest:build` / `loadtest` / `loadtest:full` / `loadtest:test` targets |
| `site/src/content/docs/contributing/` | How-to + SLO/scenario reference (PR-blocking docs) |

The xk6 modules form their own Go module, so the root module's `go test ./...` and `task lint` never see k6's dependency tree. Their Go unit tests run via `task loadtest:test`.

---

## Phase 1: MVP — de-risk + minimal vertical slice

### Task 1: De-risk spike — vet `xk6-connectrpc` for `StreamEvents` server-streaming

**Files:**

- Create: `test/load/build/manifest.txt`
- Note: record verdict on bead `holomush-evggu`

This is a **spike**, not TDD. The whole tooling choice (ADR holomush-evggu) hinges on whether `xk6-connectrpc` can consume a Connect **server-stream** (`WebService.StreamEvents`, `api/proto/holomush/web/v1/web.proto:64`). A unary-only extension forces a fork.

> **API caveat:** the `xk6-connectrpc` JS surface used in later tasks (`client.loadProtos`, `client.invoke`, `client.serverStream`, `stream.on('data')`) is **illustrative** — modeled on k6's built-in `k6/net/grpc`. This spike establishes the extension's *actual* exported API; if it differs, update the JS in Tasks 5–6/9/12 to match what the spike confirms.

- [ ] **Step 1: Build a throwaway custom k6 with the extension**

```bash
go install go.k6.io/xk6/cmd/xk6@latest
mkdir -p /tmp/k6spike && cd /tmp/k6spike
xk6 build --with github.com/bumberboy/xk6-connectrpc@latest
```

Expected: a `k6` binary in `/tmp/k6spike`. If `xk6 build` fails, record the failure and proceed to Step 4 (fork path).

- [ ] **Step 2: Stand up the SUT locally**

Run: `task dev` (or `docker compose -f compose.yaml up -d`) so the gateway serves the Connect `WebService` on its configured port.

- [ ] **Step 3: Probe server-streaming with a minimal script**

```javascript
// /tmp/k6spike/probe.js
import connectrpc from 'k6/x/connectrpc';

const client = new connectrpc.Client();
client.loadProtos(['proto'], 'holomush/web/v1/web.proto');

export default function () {
  client.connect('https://localhost:8443', { plaintext: false, timeout: '5s' });
  // The capability under test: a Connect SERVER STREAM.
  const stream = client.serverStream('holomush.web.v1.WebService/StreamEvents', { /* req */ });
  stream.on('data', (msg) => console.log('stream data', JSON.stringify(msg)));
  stream.on('error', (e) => console.error('stream error', e));
  stream.on('end', () => console.log('stream end'));
}
```

Run: `/tmp/k6spike/k6 run /tmp/k6spike/probe.js`
Expected (success): `stream data ...` lines, proving server-streaming works. Expected (failure): an API error that `serverStream` / `.on('data')` is unsupported.

- [ ] **Step 4: Record the verdict and pin the source**

If streaming works: write `test/load/build/manifest.txt`. **Each line is a complete `xk6 build --with` argument** (the build command in Task 2 just prefixes `--with`): remote modules pin with `@<sha>`, local in-tree modules use xk6's `module=./path` local-replace form.

```text
# xk6 extension build manifest — one `xk6 build --with` argument per line.
# Remote: module@version  |  Local in-tree: module=./relative/path
github.com/bumberboy/xk6-connectrpc@<RESOLVED-COMMIT-SHA>
```

If streaming is absent: fork to `test/load/xk6/connectrpc/` (Connect server-stream support added by mirroring connect-go's `ServerStreamForClient` loop) and add a local line `github.com/holomush/holomush/test/load/xk6/connectrpc=./xk6/connectrpc` (xk6 resolves `=./path` as the module replacement — no separate `--replace` flag needed). Record either outcome:

```bash
bd note holomush-evggu "P1 spike verdict: xk6-connectrpc server-streaming = <WORKS @<sha> | ABSENT → forked to test/load/xk6/connectrpc>. StreamEvents drivable: <yes/after-fork>."
```

- [ ] **Step 5: Commit**

Commit `test/load/build/manifest.txt` per `references/vcs-preamble.md` with message `test(load): pin xk6-connectrpc after streaming spike (holomush-evggu)`.

---

### Task 2: Custom-k6 build pipeline (separate Go module + reproducible build)

**Files:**

- Create: `test/load/go.mod`, `test/load/build/Dockerfile.k6`
- Modify: `Taskfile.yaml` (add `loadtest:build`)
- Test: build succeeds and emits a versioned binary

- [ ] **Step 1: Initialize the isolated Go module**

```bash
cd test/load && go mod init github.com/holomush/holomush/test/load && cd -
```

Expected: `test/load/go.mod` exists. The root module is unaffected (nested module is excluded from root `./...`).

- [ ] **Step 2: Write the reproducible build image**

```dockerfile
# test/load/build/Dockerfile.k6  (build context = repo root)
FROM grafana/xk6:latest AS build
WORKDIR /src
# Copy the whole test/load module so local `=./xk6/<ext>` paths in the manifest
# resolve (the in-tree telnet/connectrpc extensions live under /src/xk6/).
COPY test/load/ /src/
# Each manifest line is already a full `--with` argument (module@version OR
# module=./local/path); we only prefix `--with`. Comments/blanks skipped.
RUN xk6 build $(grep -vE '^\s*#|^\s*$' build/manifest.txt | sed 's/^/--with /' | tr '\n' ' ') \
    --output /out/k6
FROM debian:bookworm-slim
COPY --from=build /out/k6 /usr/local/bin/k6
ENTRYPOINT ["k6"]
```

- [ ] **Step 3: Add the `loadtest:build` task**

```yaml
  loadtest:build:
    desc: Build the custom k6 binary (xk6 extensions pinned in test/load/build/manifest.txt)
    cmds:
      - docker build -f test/load/build/Dockerfile.k6 -t holomush-k6:local .
```

- [ ] **Step 4: Verify the build**

Run: `task loadtest:build`
Expected: image `holomush-k6:local` built; `docker run --rm holomush-k6:local version` prints a k6 version with the bundled extensions listed.

- [ ] **Step 5: Commit**

`test(load): custom k6 build pipeline (isolated go module + pinned xk6)`

---

### Task 3: xk6 telnet module (raw TCP driver)

**Files:**

- Create: `test/load/xk6/telnet/telnet.go`, `test/load/xk6/telnet/telnet_test.go`
- Modify: `test/load/build/manifest.txt` (add the local telnet module)
- Reference: `internal/telnet/gateway_handler.go` (two-phase login), `internal/telnet/guest_auth.go` (guest flow)

The gateway telnet listener speaks a line protocol with a two-phase login (guest or registered). The module exposes `dial`, `login`, `send`, and `readLine` to JS.

- [ ] **Step 1: Write the failing Go test for the line framer**

```go
// test/load/xk6/telnet/telnet_test.go
package telnet

import "testing"

func TestSplitLinesHandlesCRLFAndLF(t *testing.T) {
	got := splitLines("hello\r\nworld\nfin")
	want := []string{"hello", "world", "fin"}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run it, verify failure**

Run: `cd test/load && go test ./xk6/telnet/ -run TestSplitLines`
Expected: FAIL — `undefined: splitLines`.

- [ ] **Step 3: Implement the module + framer**

```go
// test/load/xk6/telnet/telnet.go
// Package telnet is an xk6 extension exposing a raw-TCP telnet driver to k6 JS
// as module "k6/x/telnet". It drives the holomush gateway telnet listener
// (see internal/telnet/gateway_handler.go) for load testing.
package telnet

import (
	"bufio"
	"net"
	"strings"
	"time"

	"go.k6.io/k6/js/modules"
)

func init() { modules.Register("k6/x/telnet", new(RootModule)) }

type RootModule struct{}
type ModuleInstance struct{ vu modules.VU }

func (*RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	return &ModuleInstance{vu: vu}
}
func (mi *ModuleInstance) Exports() modules.Exports {
	return modules.Exports{Named: map[string]any{"dial": mi.Dial}}
}

// Conn is the JS-facing handle for one telnet connection.
type Conn struct {
	c  net.Conn
	br *bufio.Reader
}

// Dial opens a telnet connection with a write/read deadline budget.
func (mi *ModuleInstance) Dial(addr string, timeoutMs int) (*Conn, error) {
	c, err := net.DialTimeout("tcp", addr, time.Duration(timeoutMs)*time.Millisecond)
	if err != nil {
		return nil, err
	}
	return &Conn{c: c, br: bufio.NewReader(c)}, nil
}

// Send writes a command line (CRLF-terminated).
func (cn *Conn) Send(line string) error {
	_, err := cn.c.Write([]byte(line + "\r\n"))
	return err
}

// ReadLine reads one line up to the deadline; returns "" on timeout.
func (cn *Conn) ReadLine(timeoutMs int) (string, error) {
	_ = cn.c.SetReadDeadline(time.Now().Add(time.Duration(timeoutMs) * time.Millisecond))
	s, err := cn.br.ReadString('\n')
	return strings.TrimRight(s, "\r\n"), err
}

func (cn *Conn) Close() error { return cn.c.Close() }

// splitLines normalizes CRLF/LF framing for tests and buffered reads.
func splitLines(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}
```

- [ ] **Step 4: Run the test, verify pass**

Run: `cd test/load && go test ./xk6/telnet/ -run TestSplitLines`
Expected: PASS.

- [ ] **Step 5: Add the local module to the build manifest**

Append the local-module line to `test/load/build/manifest.txt`, using xk6's `module=./path` local-replace form (path relative to the module root `/src` in the build image — see the Task 2 Dockerfile, which copies `test/load/` to `/src/`):

```text
github.com/holomush/holomush/test/load/xk6/telnet=./xk6/telnet
```

(No separate `--replace` flag is needed — `=./xk6/telnet` is how `xk6 build --with` resolves a local module. Document the manifest format in `test/load/README.md`.)

- [ ] **Step 6: Rebuild + commit**

Run: `task loadtest:build` (Expected: PASS, telnet module bundled). Commit `test(load): xk6 telnet raw-TCP driver`.

---

### Task 4: Scenario config + canonical verb set (INV-LOAD-7; INV-LOAD-6 enforced in Task 8)

**Files:**

- Create: `test/load/verbs.json`, `test/load/lib/config.js`, `test/load/lib/workload.js`
- Reference: `plugins/core-communication/plugin.yaml` (registered verbs)

The canonical verb set lives in **`test/load/verbs.json`** so it has exactly one source consumed by both the JS scenario and the Go meta-test (Task 8) that enforces INV-LOAD-6. There is no list-commands RPC — verbs are validated against plugin manifests at build time rather than discovered at runtime (`help` is the only runtime listing surface and its output arrives via the event stream, which is fragile to parse in `setup()`).

- [ ] **Step 1: Write the canonical verb set**

```json
{
  "roomBroadcast": ["say", "pose", "emit", "ooc"],
  "directed": ["page", "whisper"],
  "read": ["examine"]
}
```

Every verb here is a **registered player command** (verified: `say`/`pose`/`emit`/`ooc`/`page`/`whisper` in `plugins/core-communication/plugin.yaml`; `examine` in `plugins/core-objects/plugin.yaml`). There is **no `look`/`move`/`recall` command** in the codebase — reads use `examine`, occupancy/grouping uses **scenes** (Task 10), and history is the `WebQueryStreamHistory` **RPC** (not a verb; Task 9). Channel verbs are absent (no `core-channels` plugin; future per spec §5.1).

- [ ] **Step 2: Write the config module (env-driven, seeded; reads verbs.json)**

```javascript
// test/load/lib/config.js
// Central scenario knobs. N and duration are env-driven; the RNG seed is fixed
// so two runs of the same config produce the same action sequence (INV-LOAD-7).
// open() reads the canonical verb set at init time (path relative to this file).
export const VERBS = JSON.parse(open('../verbs.json'));

export const cfg = {
  webBase: __ENV.HOLOMUSH_WEB || 'https://localhost:8443',
  telnetAddr: __ENV.HOLOMUSH_TELNET || 'localhost:4000',
  vus: parseInt(__ENV.LOAD_VUS || '50', 10),       // smoke default; nightly overrides to 1000
  duration: __ENV.LOAD_DURATION || '30s',
  seed: parseInt(__ENV.LOAD_SEED || '1337', 10),
  transport: __ENV.LOAD_TRANSPORT || 'connect',     // 'connect' | 'telnet'
};
```

- [ ] **Step 3: Write the seeded action-mix selector**

```javascript
// test/load/lib/workload.js
import { cfg, VERBS } from './config.js';

// Deterministic LCG so action selection is reproducible per seed (INV-LOAD-7).
function lcg(seed) {
  let s = seed >>> 0;
  return () => (s = (1103515245 * s + 12345) >>> 0) / 0xffffffff;
}

// Comms-heavy mix (spec §5), using only real surfaces:
//   say/pose/emit/ooc 45% · page/whisper 35% · examine 12% · history-RPC 8%.
// '__history__' is a sentinel routed to the WebQueryStreamHistory RPC (not a verb).
const MIX = [
  { p: 0.45, pick: (r) => VERBS.roomBroadcast[Math.floor(r * VERBS.roomBroadcast.length)] },
  { p: 0.35, pick: (r) => VERBS.directed[Math.floor(r * VERBS.directed.length)] },
  { p: 0.12, pick: () => 'examine' },
  { p: 0.08, pick: () => '__history__' },
];

export function makeActionStream(vuSeed) {
  const rng = lcg(cfg.seed + vuSeed);
  return function nextAction() {
    let roll = rng(), acc = 0;
    for (const bucket of MIX) {
      acc += bucket.p;
      if (roll <= acc) return bucket.pick(rng());
    }
    return 'say';
  };
}
```

- [ ] **Step 4: Verify determinism with a tiny k6 check**

```javascript
// test/load/lib/workload.smoke.js
import { makeActionStream } from './workload.js';
export default function () {
  const a = makeActionStream(7), b = makeActionStream(7);
  for (let i = 0; i < 100; i++) {
    if (a() !== b()) throw new Error('non-deterministic action stream');
  }
}
```

Run: `docker run --rm -v "$PWD/test/load:/load" holomush-k6:local run /load/lib/workload.smoke.js`
Expected: PASS (no throw) — proves INV-LOAD-7.

- [ ] **Step 5: Commit**

`test(load): seeded comms-heavy action mix + canonical verb set`

---

### Task 5: Session lifecycle + minimal action set over the Connect transport

**Files:**

- Create: `test/load/lib/session.js`
- Reference: `api/proto/holomush/web/v1/web.proto` (`WebCreateGuest`, `StreamEvents`, `SendCommand`)

- [ ] **Step 1: Write the Connect session lifecycle**

```javascript
// test/load/lib/session.js
import connectrpc from 'k6/x/connectrpc';
import { cfg } from './config.js';

const SVC = 'holomush.web.v1.WebService';

// connectSession opens a guest session and its event stream. Returns a handle
// the scenario loop drives. One persistent connection per VU (spec §6 VU model).
import { connectAuthMs } from './latency.js';

export function connectSession() {
  const t0 = Date.now();
  const client = new connectrpc.Client();
  client.loadProtos(['proto'], 'holomush/web/v1/web.proto');
  client.connect(cfg.webBase, { plaintext: cfg.webBase.startsWith('http://'), timeout: '5s' });

  // Two-step guest auth: WebCreateGuest returns default_character_id (no
  // session_id, proto web.proto:244-252); WebSelectCharacter binds that
  // character and returns the session_id (proto web.proto:213-223).
  const guest = client.invoke(`${SVC}/WebCreateGuest`, {});
  const characterId = guest.message.default_character_id;
  const sel = client.invoke(`${SVC}/WebSelectCharacter`, { character_id: characterId });
  const sessionId = sel.message.session_id;
  // StreamEvents opens the per-connection event stream; the first ControlFrame
  // (STREAM_OPENED) carries connection_id, which SendCommand needs for routing.
  const stream = client.serverStream(`${SVC}/StreamEvents`, { session_id: sessionId });
  const sess = { client, stream, sessionId, connectionId: '', kind: 'connect' };
  stream.on('data', (evt) => {
    if (evt.control && evt.control.kind === 'STREAM_OPENED') sess.connectionId = evt.control.connection_id;
  });
  connectAuthMs.add(Date.now() - t0); // SLO 5: connect+auth latency
  return sess;
}

// sendCommand issues a verb via SendCommandRequest. Field is `text` (proto
// web.proto:115); session_id + connection_id bind the command to this session.
export function sendCommand(sess, verb, args, measured) {
  const text = `${verb} ${args}`.trim();
  return sess.client.invoke(`${SVC}/SendCommand`, {
    session_id: sess.sessionId, connection_id: sess.connectionId, text,
  });
}

export function closeSession(sess) {
  try { sess.stream.end(); } finally { sess.client.close(); }
}
```

> **Note:** `WebCreateGuest`/`StreamEvents`/`SendCommand` field names are grounded in `api/proto/holomush/web/v1/web.proto`. Confirm the exact JSON field casing (`session_id` vs `sessionId`) the xk6-connectrpc codec emits against the Task 1 spike — connect-go JSON uses the proto field name (`session_id`).

- [ ] **Step 2: Write the scenario skeleton wiring setup + loop**

```javascript
// test/load/scenarios/comms.js
import { sleep } from 'k6';
import connectrpc from 'k6/x/connectrpc';
import { cfg } from '../lib/config.js';
import { makeActionStream } from '../lib/workload.js';
import { connectSession, sendCommand, closeSession } from '../lib/session.js';

export const options = {
  scenarios: {
    comms: { executor: 'constant-vus', vus: cfg.vus, duration: cfg.duration },
  },
  // Thresholds filled in Task 6/8 (these ARE the SLO gate).
  thresholds: {},
};

export function setup() {
  // Connectivity preflight only. Verb-availability (INV-LOAD-6) is enforced
  // statically by the Task 8 meta-test against verbs.json + plugin manifests —
  // not at runtime (there is no list-commands RPC).
  const c = new connectrpc.Client();
  c.loadProtos(['proto'], 'holomush/web/v1/web.proto');
  c.connect(cfg.webBase, { plaintext: cfg.webBase.startsWith('http://'), timeout: '5s' });
  c.close();
  return {};
}

export default function () {
  let sess = connectSession();
  const next = makeActionStream(__VU);
  const end = Date.now() + 20_000; // per-iteration session lifetime; churn added in Task 11
  while (Date.now() < end) {
    const verb = next();
    // Directed (page/whisper) + history RPC are wired in Task 9; until then,
    // those slots exercise a room broadcast so the smoke stays representative.
    if (verb === 'examine') sendCommand(sess, 'examine', 'me');
    else if (verb === '__history__' || verb === 'page' || verb === 'whisper') sendCommand(sess, 'say', 'hello', true);
    else sendCommand(sess, verb, 'hello', true);
    sleep(exp(10)); // think-time ~exp(mean 10s), spec §5
  }
  closeSession(sess);
}

function exp(meanSec) { return -Math.log(1 - Math.random()) * meanSec; }
```

- [ ] **Step 3: Smoke-run against a local SUT**

Run: `task dev` then `task loadtest` (defined in Task 7).
Expected: VUs connect, issue commands, no connection errors in the k6 summary.

- [ ] **Step 4: Verify INV-LOAD-1 (Connect protocol, not gRPC)**

The harness MUST drive the web tier over the Connect protocol. Inspect the SUT's request telemetry for the smoke run (OTel span attribute on the `holomush-gateway` server span, or the access log): `WebService` requests MUST carry `Content-Type: application/connect+proto` — never `application/grpc`. Record the observed content-type.

Run: `docker compose logs gateway | rg -o 'application/(connect\+proto|grpc[^ ]*)' | sort -u`
Expected: only `application/connect+proto` appears (proves INV-LOAD-1; gRPC would mean the extension defaulted wrong).

- [ ] **Step 5: Commit**

`test(load): Connect-transport session lifecycle + scenario skeleton`

---

### Task 6: say→broadcast latency via emit-timestamp-in-payload (INV-LOAD-3)

**Files:**

- Create: `test/load/lib/latency.js`
- Modify: `test/load/lib/session.js`, `test/load/scenarios/comms.js`

- [ ] **Step 1: Write the latency helper (encode/decode + Trend)**

```javascript
// test/load/lib/latency.js
import { Trend } from 'k6/metrics';

export const sayBroadcast = new Trend('say_broadcast_ms', true);
export const directedDelivery = new Trend('directed_delivery_ms', true);
export const connectAuthMs = new Trend('connect_auth_ms', true); // SLO 5

const MARK = 'LT:'; // sentinel so recipients can find the emit stamp

// stamp embeds a high-res emit timestamp into the outgoing message body.
export function stamp(body) { return `${body} ${MARK}${Date.now()}`; }

// observe parses the stamp from a received broadcast and records the delta.
export function observe(trend, receivedBody) {
  const i = receivedBody.lastIndexOf(MARK);
  if (i < 0) return;
  const emit = parseInt(receivedBody.slice(i + MARK.length), 10);
  if (!Number.isNaN(emit)) trend.add(Date.now() - emit);
}

// assertClockSkewBound enforces the ≤50ms generator/SUT skew assumption (§12).
export function assertClockSkewBound(serverNowMs) {
  const skew = Math.abs(Date.now() - serverNowMs);
  if (skew > 50) throw new Error(`clock skew ${skew}ms exceeds 50ms bound (INV-LOAD-3)`);
}
```

- [ ] **Step 2: Wire stamp into sends and observe into the stream handler**

In `session.js`, stamp room-broadcast + directed sends; in `comms.js` attach a stream `on('data')` handler that routes to `observe(sayBroadcast, ...)` or `observe(directedDelivery, ...)` by event type.

```javascript
// session.js — sendCommand gains the `measured` stamp (field is `text`, with
// session_id/connection_id binding — see Task 5).
import { stamp } from './latency.js';
export function sendCommand(sess, verb, args, measured) {
  const body = measured ? stamp(args) : args;
  return sess.client.invoke(`${SVC}/SendCommand`, {
    session_id: sess.sessionId, connection_id: sess.connectionId,
    text: `${verb} ${body}`.trim(),
  });
}
```

```javascript
// comms.js — inside connectSession wiring:
import { sayBroadcast, directedDelivery, observe } from '../lib/latency.js';
sess.stream.on('data', (evt) => {
  if (evt.type === 'say' || evt.type === 'pose') observe(sayBroadcast, evt.body);
  else if (evt.type === 'page' || evt.type === 'whisper') observe(directedDelivery, evt.body);
});
```

- [ ] **Step 3: Add the SLO thresholds (provisional until baseline calibration)**

```javascript
// comms.js options.thresholds:
thresholds: {
  say_broadcast_ms: ['p(99)<250'],
  directed_delivery_ms: ['p(99)<150'],
  connect_auth_ms: ['p(99)<1000'], // SLO 5
  checks: ['rate>0.999'],          // SLO 4 (error rate < 0.1%)
},
```

- [ ] **Step 4: Smoke-run and confirm the Trends populate**

Run: `task loadtest`
Expected: summary shows `say_broadcast_ms` with a p99 value (co-located VUs receive each other's stamped broadcasts).

- [ ] **Step 5: Commit**

`test(load): client-side say/directed latency via payload timestamp (INV-LOAD-3)`

---

### Task 7: `task loadtest` smoke target + first green run

**Files:**

- Modify: `Taskfile.yaml`

- [ ] **Step 1: Add the smoke + full + module-test targets**

```yaml
  loadtest:
    desc: "Load run against a running SUT. Override via vars: LOAD_VUS/LOAD_DURATION/LOAD_SCENARIO."
    deps: ['loadtest:build']
    vars:
      LOAD_VUS: '{{.LOAD_VUS | default "50"}}'
      LOAD_DURATION: '{{.LOAD_DURATION | default "30s"}}'
      LOAD_SCENARIO: '{{.LOAD_SCENARIO | default "comms.js"}}'
      LOAD_TRANSPORT: '{{.LOAD_TRANSPORT | default "connect"}}'
    cmds:
      - >-
        docker run --rm --network host -v "{{.ROOT_DIR}}/test/load:/load"
        -v "{{.ROOT_DIR}}/api/proto:/load/proto"
        -e LOAD_VUS={{.LOAD_VUS}} -e LOAD_DURATION={{.LOAD_DURATION}}
        -e LOAD_TRANSPORT={{.LOAD_TRANSPORT}}
        holomush-k6:local run /load/scenarios/{{.LOAD_SCENARIO}}

  loadtest:full:
    desc: "Heavy local load run (N=1000); requires a Dockerized SUT"
    cmds:
      - task: loadtest
        vars: { LOAD_VUS: '1000', LOAD_DURATION: '10m' }

  loadtest:test:
    desc: "Unit-test the xk6 Go modules (separate go module)"
    cmds:
      - cd test/load && go test ./...
```

(go-task does NOT propagate a `VAR=x task ...` shell prefix into another task's `{{.VAR}}` context — call sub-tasks with the `task:`/`vars:` form, as `loadtest:full` does.)

- [ ] **Step 2: Run the smoke end-to-end**

Run: `task dev` (SUT up) then `task loadtest`
Expected: exit 0; thresholds reported; `say_broadcast_ms` p99 printed.

- [ ] **Step 3: Run the module tests**

Run: `task loadtest:test`
Expected: PASS (telnet framer test from Task 3).

- [ ] **Step 4: Commit**

`test(load): loadtest smoke/full/test task targets`

---

### Task 8: Nightly workflow, baseline, and exit-discipline meta-tests (INV-LOAD-4, INV-LOAD-5, INV-LOAD-8)

**Files:**

- Create: `.github/workflows/nightly-load.yml`, `.benchmarks/load-baseline.json`, `test/meta/load_harness_invariants_test.go`
- Reference: `.github/workflows/nightly-soak.yml`, `test/meta/pr_prep_fast_lane_test.go`, `test/meta/tooling_no_mandatory_int_test.go`

- [ ] **Step 1: Write the nightly workflow**

```yaml
# .github/workflows/nightly-load.yml
name: Nightly Load
on:
  schedule:
    - cron: "0 7 * * *"   # 07:00 UTC — after nightly-soak's 06:00 + 30m budget
  workflow_dispatch: {}
jobs:
  load:
    runs-on: namespace-profile-default
    timeout-minutes: 45
    steps:
      - uses: actions/checkout@v4
      - name: Stand up SUT
        run: docker compose -f compose.yaml up -d --wait
      - name: Build custom k6
        run: task loadtest:build
      - name: Prepare output dir
        run: mkdir -p test/load/out
      - name: Run heavy load (gate on thresholds)
        run: |
          docker run --rm --network host \
            -v "$PWD/test/load:/load" -v "$PWD/api/proto:/load/proto" \
            -e LOAD_VUS=1000 -e LOAD_DURATION=10m \
            --out json=/load/out/summary.json \
            holomush-k6:local run /load/scenarios/comms.js
      - name: Regression gate vs baseline
        run: ./scripts/check-load-regression.sh test/load/out/summary.json .benchmarks/load-baseline.json
      - uses: actions/upload-artifact@v4
        if: always()
        with: { name: load-summary, path: test/load/out/summary.json }
```

The k6 step is a **standalone command whose non-zero exit fails the job** — no `| tee`/`| tail`/trailing `echo` (INV-LOAD-4).

- [ ] **Step 2: Seed the baseline file (calibrated after first run)**

```json
{
  "_comment": "Provisional. Replace with observed p99 × headroom after the first nightly run. Gate factor 1.10.",
  "say_broadcast_ms_p99": 250,
  "directed_delivery_ms_p99": 150,
  "command_ack_ms_p99": 200,
  "history_ms_p99": 500,
  "connect_auth_ms_p99": 1000
}
```

- [ ] **Step 3: Write the regression gate script (INV-LOAD-5)**

Create `scripts/check-load-regression.sh`. It reads each `*_p99` from the baseline JSON, extracts the matching metric's p99 from the k6 end-of-test summary JSON, and exits non-zero if any observed value exceeds `baseline × 1.10` (mirrors `scripts/check-benchmark-regression.sh`).

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# check-load-regression.sh <k6-summary.json> <baseline.json>
# Exits non-zero if any metric p99 exceeds baseline × FACTOR.
set -euo pipefail
SUMMARY="$1"; BASELINE="$2"; FACTOR="${LOAD_GATE_FACTOR:-1.10}"
fail=0
# Map baseline keys (e.g. say_broadcast_ms_p99) → k6 metric name (say_broadcast_ms).
for key in $(jq -r 'keys[] | select(endswith("_p99"))' "$BASELINE"); do
  metric="${key%_p99}"
  base=$(jq -r --arg k "$key" '.[$k]' "$BASELINE")
  # k6 JSON summary stores trends under .metrics.<name>.values["p(99)"].
  obs=$(jq -r --arg m "$metric" '.metrics[$m].values["p(99)"] // empty' "$SUMMARY")
  if [ -z "$obs" ]; then echo "WARN: metric $metric absent from summary"; continue; fi
  limit=$(awk -v b="$base" -v f="$FACTOR" 'BEGIN{printf "%.3f", b*f}')
  if awk -v o="$obs" -v l="$limit" 'BEGIN{exit !(o>l)}'; then
    echo "FAIL: $metric p99=$obs > limit=$limit (baseline=$base × $FACTOR)"; fail=1
  else
    echo "ok:   $metric p99=$obs <= $limit"
  fi
done
exit "$fail"
```

- [ ] **Step 4: Write the failing meta-tests (INV-LOAD-4, INV-LOAD-6, INV-LOAD-8)**

Reuse the existing `test/meta` helpers `findRepoRoot(t)` and `taskBlock(t, tf, name)` (defined in `test/meta/pr_prep_fast_lane_test.go`) — do NOT invent new root/parse helpers.

```go
// test/meta/load_harness_invariants_test.go
package meta

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// INV-LOAD-8: the load harness MUST NOT be wired into the pr-prep fast lane.
func TestLoadHarnessNotInPrPrep(t *testing.T) {
	root := findRepoRoot(t)
	tf, err := os.ReadFile(filepath.Join(root, "Taskfile.yaml"))
	require.NoError(t, err)
	require.NotContains(t, taskBlock(t, string(tf), "pr-prep"), "loadtest",
		"INV-LOAD-8: pr-prep MUST NOT depend on any loadtest target")
}

// INV-LOAD-4: the nightly-load gate MUST derive pass/fail from the k6 exit
// code — no tee/tail/trailing-echo masking on the gating "Run heavy load" step.
func TestNightlyLoadNoExitMasking(t *testing.T) {
	root := findRepoRoot(t)
	wf, err := os.ReadFile(filepath.Join(root, ".github/workflows/nightly-load.yml"))
	require.NoError(t, err)
	body := string(wf)
	// Isolate the gating step: from its name to the next "- name:".
	i := strings.Index(body, "Run heavy load")
	require.GreaterOrEqual(t, i, 0, "gating step 'Run heavy load' not found")
	gate := body[i:]
	if j := strings.Index(gate, "- name:"); j > 0 {
		gate = gate[:j]
	}
	for _, bad := range []string{"| tee", "| tail", "&& echo", "; echo $?"} {
		require.NotContains(t, gate, bad,
			"INV-LOAD-4: gating step masks exit code via %q", bad)
	}
}

// INV-LOAD-6: every verb in test/load/verbs.json MUST be a registered command —
// present in some plugins/*/plugin.yaml commands[].name, or a known built-in.
func TestLoadScenarioVerbsRegistered(t *testing.T) {
	root := findRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "test/load/verbs.json"))
	require.NoError(t, err)
	var groups map[string][]string
	require.NoError(t, json.Unmarshal(raw, &groups))

	// Every scenario verb must be a plugin-declared command. (verbs.json holds
	// only real commands — say/pose/emit/ooc/page/whisper + examine — so no
	// built-in allowlist is needed; reads use examine, history is an RPC.)
	registered := map[string]bool{}
	manifests, _ := filepath.Glob(filepath.Join(root, "plugins/*/plugin.yaml"))
	for _, m := range manifests {
		b, err := os.ReadFile(m)
		require.NoError(t, err)
		var doc struct {
			Commands []struct {
				Name string `yaml:"name"`
			} `yaml:"commands"`
		}
		require.NoError(t, yaml.Unmarshal(b, &doc))
		for _, c := range doc.Commands {
			registered[c.Name] = true
		}
	}
	for _, verbs := range groups {
		for _, v := range verbs {
			require.True(t, registered[v],
				"INV-LOAD-6: verbs.json verb %q is not a registered command", v)
		}
	}
}
```

- [ ] **Step 5: Run meta-tests, verify they pass against the new files**

Run: `task test -- -run 'TestLoadHarness|TestNightlyLoad|TestLoadScenarioVerbs' ./test/meta/`
Expected: PASS (every verb in `verbs.json` resolves to a `commands[].name` in a plugin manifest — comms via `core-communication`, `examine` via `core-objects`).

- [ ] **Step 6: Commit**

`test(load): nightly-load workflow + baseline + exit-discipline meta-tests`

---

## Phase 2: Full — complete mix, both transports, dashboards, docs

### Task 9: Complete comms mix — directed (page/whisper) + emit/ooc + history (SLO 1b)

**Files:**

- Modify: `test/load/lib/session.js`, `test/load/scenarios/comms.js`

- [ ] **Step 1: Implement directed sends with targeting**

`page <name>=<msg>` and `whisper <name>=<msg>` require a target. Pick a random other-VU character name from a `SharedArray` of session identities built in `setup()`.

```javascript
// session.js — directed send. SendCommandRequest.text carries the verb;
// session_id + connection_id bind it to this session (proto web.proto:113).
export function sendDirected(sess, verb, targetName, msg) {
  return sess.client.invoke(`${SVC}/SendCommand`, {
    session_id: sess.sessionId, connection_id: sess.connectionId,
    text: `${verb} ${targetName}=${stamp(msg)}`,
  });
}
```

- [ ] **Step 2: Implement history queries**

```javascript
// WebQueryStreamHistoryRequest: session_id + stream + count (proto web.proto:333).
// `count` is the page size — NOT `limit`. `stream` is the subject to page
// (the session's scene/location IC stream from Task 10's occupancy assignment).
export function queryHistory(sess, stream) {
  return sess.client.invoke(`${SVC}/WebQueryStreamHistory`, {
    session_id: sess.sessionId, stream, count: 20,
  });
}
```

- [ ] **Step 3: Route the action stream to the right sender**

Update the `comms.js` loop to dispatch `page`/`whisper` → `sendDirected(sess, verb, target, 'hi')` (target = a random peer name from the scene roster `SharedArray`), `__history__` → `queryHistory(sess, sess.stream_subject)`, `examine` → `sendCommand(sess, 'examine', target)`, room verbs → `sendCommand(sess, verb, 'hello', true)`.

- [ ] **Step 4: Add command-ack + history Trends and thresholds**

```javascript
// latency.js
export const commandAck = new Trend('command_ack_ms', true);
export const historyMs = new Trend('history_ms', true);
```

```javascript
// comms.js thresholds += { command_ack_ms: ['p(99)<200'], history_ms: ['p(99)<500'] }
```

- [ ] **Step 5: Smoke-run, confirm all four Trends populate**

Run: `task loadtest`
Expected: `say_broadcast_ms`, `directed_delivery_ms`, `command_ack_ms`, `history_ms` all present.

- [ ] **Step 6: Commit**

`test(load): full comms mix (directed delivery + history) with SLO 1b`

---

### Task 10: Scene-based occupancy/concentration model (Zipf skew + hot scenes)

**Files:**

- Modify: `test/load/lib/session.js`, `test/load/lib/config.js`, `test/load/scenarios/comms.js`
- Reference: `plugins/core-scenes/plugin.yaml` (`scene <subcommand>` command; `seed:player-scene-participant` grants join/leave)

There is **no `move` command** — co-location/fanout in HoloMUSH happens via **scenes** (the RP container). VUs are assigned to scenes (Zipf-skewed sizes, a few hot scenes); `say`/`pose` inside a scene broadcast to scene participants (`scene_say_ic`/`scene_pose_ic`). This is the faithful concentration model (spec §5 "a few larger scenes").

- [ ] **Step 1: Add the scene-occupancy knob**

```javascript
// config.js
export const occupancy = {
  scenes: parseInt(__ENV.LOAD_SCENES || '150', 10),
  hotScenes: parseInt(__ENV.LOAD_HOT_SCENES || '4', 10),
  hotSceneSize: parseInt(__ENV.LOAD_HOT_SIZE || '30', 10),
};
```

- [ ] **Step 2: Create scenes in setup() and assign VUs (Zipf skew)**

In `setup()` a small pool of owner VUs run `scene create` to mint scene IDs; setup returns the scene-ID list (k6 passes the `setup()` return value to every VU). `assignScene(vu)` maps a VU to a scene: hot scenes fill first to `hotSceneSize`, the remainder spread across the cold tail. Each VU `scene join <id>`s its assigned scene at session start.

```javascript
// session.js — deterministic scene assignment (no RNG; depends only on vu).
export function assignScene(vu, sceneIds) {
  const { hotScenes, hotSceneSize } = occupancy;
  const hotCap = hotScenes * hotSceneSize;
  if (vu <= hotCap) return sceneIds[Math.floor((vu - 1) / hotSceneSize)];
  return sceneIds[hotScenes + ((vu - hotCap) % (sceneIds.length - hotScenes))];
}

export function joinScene(sess, sceneId) {
  sess.sceneId = sceneId;
  return sendCommand(sess, 'scene', `join ${sceneId}`);
}
```

The VU's `stream_subject` for history queries (Task 9) is its scene's IC stream (e.g., `scene:<id>`); set it when joining.

- [ ] **Step 3: Smoke-run, verify hot scenes amplify say_broadcast fanout**

Run: `LOAD_HOT_SIZE=40 task loadtest`
Expected: `say_broadcast_ms` sample count scales with hot-scene size (each emit in a 40-participant scene records ~39 recipient observations).

- [ ] **Step 4: Commit**

`test(load): scene-based Zipf occupancy + hot-scene concentration`

---

### Task 11: Churn lifecycle (disconnect/reattach) — exercises holomush-hfvc

**Files:**

- Modify: `test/load/scenarios/comms.js`, `test/load/lib/session.js`

- [ ] **Step 1: Add a churn probability knob + reattach path**

```javascript
// config.js
export const churnP = parseFloat(__ENV.LOAD_CHURN || '0.02'); // per-iteration disconnect chance
```

- [ ] **Step 2: Implement disconnect/reconnect in the loop**

On a churn roll, `closeSession` then `connectSession` (guest reattach). Record a `reattach_ok` check.

```javascript
// comms.js loop:
if (Math.random() < churnP) { closeSession(sess); sess = connectSession(); check(sess, { 'reattach ok': (s) => !!s.stream }); }
```

- [ ] **Step 3: Smoke-run with elevated churn**

Run: `LOAD_CHURN=0.2 task loadtest`
Expected: `reattach ok` check rate high; if it fails, that reproduces holomush-hfvc — file/annotate.

- [ ] **Step 4: Commit**

`test(load): session churn + reattach (repro surface for holomush-hfvc)`

---

### Task 12: Telnet-transport parity

**Files:**

- Modify: `test/load/lib/session.js`, `test/load/scenarios/comms.js`

- [ ] **Step 1: Implement the telnet session lifecycle**

```javascript
// session.js
import telnet from 'k6/x/telnet';
import { cfg } from './config.js';
export function connectTelnetSession() {
  const cn = telnet.dial(cfg.telnetAddr, 5000);
  // Two-phase login: read prompts, choose guest (see internal/telnet/gateway_handler.go).
  cn.readLine(2000);            // banner
  cn.send('guest');            // guest login path
  cn.readLine(2000);            // welcome
  return { cn, kind: 'telnet' };
}
```

- [ ] **Step 2: Branch transport in the scenario**

```javascript
// comms.js: const sess = cfg.transport === 'telnet' ? connectTelnetSession() : connectSession();
```

Telnet `say` latency is read from the inbound line stream (a reader loop parses lines and routes stamped broadcasts to `observe`).

- [ ] **Step 3: Verify INV-LOAD-2 (telnet login + say round-trip)**

The telnet smoke MUST complete a guest login and one `say` round-trip (the emitting VU sees its own broadcast echoed on the inbound line stream). Add a `check` in the telnet path asserting a stamped `say` is received within the deadline.

Run: `task loadtest LOAD_TRANSPORT=telnet` (small N)
Expected: the `telnet say round-trip` check passes — proves INV-LOAD-2.

- [ ] **Step 4: Smoke-run both transports**

Run: `task loadtest LOAD_TRANSPORT=telnet` and `task loadtest LOAD_TRANSPORT=connect`
Expected: both populate `say_broadcast_ms`. (Pass `LOAD_TRANSPORT` as a go-task var — see Task 7's `-e LOAD_TRANSPORT` forwarding — not a shell prefix.)

- [ ] **Step 5: Commit**

`test(load): telnet-transport scenario parity (INV-LOAD-2)`

---

### Task 13: Grafana/Prometheus output + baseline calibration

**Files:**

- Modify: `.github/workflows/nightly-load.yml`, `.benchmarks/load-baseline.json`
- Create: `test/load/dashboards/load-overview.json` (Grafana dashboard)

- [ ] **Step 1: Add Prometheus remote-write output to the nightly run**

Add `-o experimental-prometheus-rw` + `K6_PROMETHEUS_RW_SERVER_URL` env to the k6 docker run, pointing at the existing collector.

- [ ] **Step 2: Author the Grafana dashboard JSON** (panels: say/directed/ack/history p99 trend, throughput, error rate, VU count).

- [ ] **Step 3: Calibrate the baseline from the first real run**

After one nightly run, set each `*_p99` in `.benchmarks/load-baseline.json` to observed p99 × 1.2 headroom.

```bash
bd note holomush-ql7ef "Load baseline calibrated from first nightly run: <metric=value...>"
```

- [ ] **Step 4: Commit**

`test(load): Grafana/Prometheus output + calibrated baseline`

---

### Task 14: Contributor docs (PR-blocking)

**Files:**

- Create: `site/src/content/docs/contributing/how-to/load-testing.md`, `site/src/content/docs/contributing/reference/load-slos.md`

- [ ] **Step 1: Write the how-to** (build custom k6, run smoke/full, read dashboards, update baseline).

- [ ] **Step 2: Write the SLO/scenario reference** (the 9 SLOs, the scenario knobs/env vars, the invariants).

- [ ] **Step 3: Lint**

Run: `task lint:markdown`
Expected: PASS.

- [ ] **Step 4: Commit**

`docs(load): contributor how-to + SLO/scenario reference`

---

## Phase 3: Secondary — capacity ceiling + endurance soak

> These are the secondary uses (the user picked "SLOs + harness" as primary). The steady-state SLOs 1–6 are gated in Phases 1–2; the stability SLOs 7–9 are validated by the endurance soak here. Both build on the P1/P2 harness with no new transport code.

### Task 15: Ceiling-finding mode

**Files:**

- Create: `test/load/scenarios/ceiling.js`
- Modify: `Taskfile.yaml` (`loadtest:ceiling`)

- [ ] **Step 1: Write a ramping scenario that climbs past N until SLO breach**

```javascript
// ceiling.js — ramping-vus executor stepping N upward; abort on threshold breach,
// report the last passing N as the ceiling.
export const options = {
  scenarios: { ceiling: { executor: 'ramping-vus', stages: [
    { duration: '2m', target: 1000 }, { duration: '2m', target: 2000 },
    { duration: '2m', target: 4000 }, { duration: '2m', target: 8000 },
  ] } },
  thresholds: { say_broadcast_ms: [{ threshold: 'p(99)<250', abortOnFail: true }] },
};
```

- [ ] **Step 2: Add the task target + a manual-dispatch note** (ceiling runs are opt-in, not nightly — they're exploratory and infra-heavy).

- [ ] **Step 3: Run once, record the ceiling**

```bash
bd note holomush-ql7ef "Observed single-node ceiling: ~<N> sessions before say_broadcast p99 breach; first bottleneck: <gateway|gRPC|JetStream|Postgres> per OTel spans."
```

- [ ] **Step 4: Commit**

`test(load): capacity ceiling-finding scenario`

---

### Task 16: Endurance soak variant + stability SLOs (7–9)

**Files:**

- Create: `test/load/scenarios/soak.js`
- Modify: `Taskfile.yaml` (`loadtest:soak`), `.github/workflows/nightly-load.yml` (optional weekly soak job)
- Reference: `test/integration/eventbus_e2e/soak_test.go` (leak/RSS assertion shape)

The steady-state SLOs (1–6) are gated nightly. SLOs 7–9 — goroutine-leak (≈ baseline), bounded RSS growth, audit/JetStream consumer lag p99 ≤ 5s — need a multi-hour hold the 10-minute nightly run does not provide.

- [ ] **Step 1: Write the soak scenario (long hold at N, env-overridable)**

```javascript
// test/load/scenarios/soak.js — reuses the comms scenario default function;
// only the executor differs (a long constant-vus hold).
export { default, setup } from './comms.js';
import { cfg } from '../lib/config.js';
export const options = {
  scenarios: { soak: { executor: 'constant-vus', vus: cfg.vus,
    duration: __ENV.LOAD_DURATION || '4h' } },
  thresholds: { say_broadcast_ms: ['p(99)<250'], directed_delivery_ms: ['p(99)<150'] },
};
```

- [ ] **Step 2: Capture server-side stability metrics (SLOs 7–9)**

These are **server-observed**, not client-measured. A sidecar step samples the SUT's `/metrics` (goroutine count, RSS) at start and end, and computes audit lag from `events_audit` (the same projection the eventbus soak asserts on). `scripts/check-soak-stability.sh` fails if: end-goroutines > start × 1.2 (SLO 7), RSS growth > 50 MB/hour (SLO 8), or audit-lag p99 > 5s (SLO 9).

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# check-soak-stability.sh <metrics-start.txt> <metrics-end.txt> <hours> <pg-dsn>
set -euo pipefail
g0=$(rg -o 'go_goroutines (\d+)' -r '$1' "$1"); g1=$(rg -o 'go_goroutines (\d+)' -r '$1' "$2")
r0=$(rg -o 'process_resident_memory_bytes (\d+)' -r '$1' "$1")
r1=$(rg -o 'process_resident_memory_bytes (\d+)' -r '$1' "$2")
hours="$3"; dsn="$4"; fail=0
awk -v a="$g0" -v b="$g1" 'BEGIN{exit !(b > a*1.2)}' && { echo "FAIL: goroutine leak $g0→$g1 (SLO 7)"; fail=1; }
awk -v a="$r0" -v b="$r1" -v h="$hours" 'BEGIN{exit !((b-a)/1048576/h > 50)}' && { echo "FAIL: RSS growth >50MB/h (SLO 8)"; fail=1; }
# SLO 9: audit-lag p99 = persisted_at − event emit time, over the soak window.
# events_audit carries the event's ULID-derived emit time and the projection's
# persisted_at; p99 of the delta must be ≤ 5s. (Adjust column names to the
# events_audit schema — see internal/eventbus/audit/.)
lag_p99=$(psql "$dsn" -tAc "select coalesce(percentile_disc(0.99) within group (order by extract(epoch from (persisted_at - emitted_at))), 0) from events_audit where persisted_at > now() - interval '${hours} hours';")
awk -v l="$lag_p99" 'BEGIN{exit !(l > 5)}' && { echo "FAIL: audit lag p99=${lag_p99}s > 5s (SLO 9)"; fail=1; }
exit "$fail"
```

(Audit-lag p99 ≤ 5s is asserted by reusing the eventbus soak's lag query against `events_audit`; INV/SLO-9.)

- [ ] **Step 3: Add the soak task target**

```yaml
  loadtest:soak:
    desc: "Multi-hour endurance soak (stability SLOs 7-9); opt-in, not nightly-gating"
    cmds:
      - task: loadtest
        vars: { LOAD_VUS: '1000', LOAD_SCENARIO: 'soak.js', LOAD_DURATION: '{{.LOAD_DURATION | default "4h"}}' }
```

- [ ] **Step 4: Run a shortened soak locally to validate the assertions**

Run: `task loadtest:soak LOAD_DURATION=10m`
Expected: completes; `check-soak-stability.sh` reports no leak / bounded RSS.

- [ ] **Step 5: Commit**

`test(load): endurance soak variant + stability SLOs 7-9`

> **Out of scope (spec NG3/NG4):** the in-process Go core-tier load driver and distributed multi-node load generation are non-goals for this plan. File a follow-up bead only if CI-deterministic in-process load or >8k single-origin load becomes a real need.

---

## Post-Implementation Checklist

- [ ] `task loadtest:test` green (xk6 module unit tests)
- [ ] `task test -- ./test/meta/` green (INV-LOAD-4/8 meta-tests)
- [ ] `task lint` + `task lint:markdown` green
- [ ] First `nightly-load.yml` run green; baseline calibrated and committed
- [ ] `site/` docs merged; reference lists all 9 SLOs + INV-LOAD-1..8
- [ ] Spec invariants INV-LOAD-1..8 each have a verifying test or smoke assertion
- [ ] Bead `holomush-ql7ef` notes capture: spike verdict, baseline calibration, observed ceiling
<!-- adr-capture: sha256=66c909a317bb2608; session=cli; ts=2026-05-29T02:30:39Z; adrs=holomush-2344h -->
