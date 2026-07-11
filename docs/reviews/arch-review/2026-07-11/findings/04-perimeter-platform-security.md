# Perimeter & Platform Security (D4) — Findings

**Agent:** security-auditor/opus · **Date:** 2026-07-11 · **Scope examined:** telnet ingress (`internal/telnet/`), Web/BFF (`internal/web/`, `cmd/holomush/gateway.go`), session cookies / CORS / security headers, OTLP + Sentry relays, ConnectRPC status interceptor, TLS/mTLS cert layer (`internal/tls/certs.go`), admin UDS socket + peer-cred (`internal/admin/socket/`), auth rate-limiting & credential validation (`internal/auth/`, `internal/grpc/auth_handlers.go`), NATS external-mode scoping (`deploy/nats/`). Excluded ABAC engine internals (D2) and event-payload crypto internals (D3) per assignment.

## Summary

The perimeter is, on the whole, **carefully built and defensively minded** — this surface shows evidence of deliberate threat modeling (slowloris deadlines, output sanitization, TLS 1.3 mTLS, UDS admin socket with filesystem gating, timing-attack-resistant credential checks, DSN-validated relays, uniform password-reset responses). Most of what I found is proportionate polish rather than exposure.

The one finding that rises above the pack is an **asymmetry in request-body limits**: the internet-facing gateway ConnectRPC handler sets no message-size cap while the internal gRPC core sets a 4 MiB cap, so a single unauthenticated POST can force the gateway to buffer an arbitrarily large body into memory. Everything else is Medium/Low, and several items are already tracked.

Counts: **High 1 · Medium 2 · Low 3 · already-tracked 1** (+ strengths recorded).

## Findings

### HIGH-1 Gateway ConnectRPC handler has no request-body size cap (unauthenticated memory exhaustion)

- **Severity:** High
- **Claim:** The public web ConnectRPC handler at `:8080` is created without `connect.WithReadMaxBytes(...)`, and connect-go v1.20.0 treats an unset `readMaxBytes` (0) as *unlimited* — reading the entire request body into a pooled buffer before the handler runs — so an unauthenticated client can drive gateway memory with one large POST. The internal gRPC core caps the same messages at 4 MiB, making the gap an asymmetry rather than a deliberate choice.
- **Evidence:**
  - `internal/web/server.go:74` — handler built with only the status interceptor, no read cap:
    ```go
    path, connectHandler := webv1connect.NewWebServiceHandler(
        cfg.Handler,
        connect.WithInterceptors(statusTranslationInterceptor()),
    )
    ```
  - `connectrpc.com/connect@v1.20.0/protocol_connect.go:1118-1123` — with `readMaxBytes == 0` the LimitReader is skipped and the whole body is buffered:
    ```go
    reader := u.reader
    if u.readMaxBytes > 0 && int64(u.readMaxBytes) < math.MaxInt64 {
        reader = io.LimitReader(u.reader, int64(u.readMaxBytes)+1)
    }
    bytesRead, err := data.ReadFrom(reader) // no cap when readMaxBytes==0
    ```
  - `internal/grpc/server.go:56` + `server.go:1850` — the core gRPC server *does* cap: `MaxRecvMsgSize = 4 * 1024 * 1024` via `grpc.MaxRecvMsgSize(MaxRecvMsgSize)`. The perimeter that faces the internet is the one without the cap.
  - Compounding: `internal/web/server.go:134-139` sets `ReadHeaderTimeout` and `IdleTimeout` but **no** `http.Server.ReadTimeout`, so a slow-drip body is also not time-bounded (the relays have their own `MaxBytesReader`; the ConnectRPC path does not).
- **Impact:** Any unauthenticated client (the body is read before session validation) can POST a multi-GB body to any `/holomush.web.v1.WebService/*` unary method and force the gateway process to allocate it, risking OOM/crash. Trivially triggerable; one request suffices.
- **Recommendation:** Add `connect.WithReadMaxBytes(4<<20)` (mirror the core's 4 MiB) to `NewWebServiceHandler`. Optionally wrap the mux in a `http.MaxBytesReader`-based limit and set a modest `ReadTimeout` for non-streaming routes. One-line fix; proportionate even at hobbyist scale because the cost/impact ratio is lopsided.
- **Dedup:** none (nearest is #4699 "Gateway-side request validation for ConnectRPC endpoints", which is about semantic validation, not body-size DoS — related but distinct).

### MEDIUM-1 Whole web security posture collapses behind one default-`false` flag

- **Severity:** Medium
- **Claim:** Cookie `Secure`, cookie `SameSite`, `Strict-Transport-Security`, and the `Content-Security-Policy` response header are *all* gated on a single `--secure-cookies` boolean that **defaults to false**. An operator who terminates TLS at a reverse proxy (the project's own recommended telnet setup, and a common web setup) but forgets the flag ships session cookies without `Secure`, downgraded to `SameSite=Lax`, with no HSTS and no CSP header.
- **Evidence:**
  - `cmd/holomush/gateway.go:120` — `BoolVar(&cfg.SecureCookies, "secure-cookies", false, ...)` (default false).
  - `internal/web/cookie.go:55-58` — `if !secure { c.Secure = false; c.SameSite = http.SameSiteLaxMode }` (Strict→Lax downgrade).
  - `internal/web/security_headers.go:80-83` — HSTS and CSP are emitted only `if secure`.
  - Mitigation present: `internal/web/server.go:55-66` logs a loud startup WARN when `!Secure`, and the flag help text says it MUST be true for TLS deployments.
- **Impact:** Behind a TLS proxy the app doesn't see TLS, so the safe value is non-obvious to operators. The failure is silent apart from a log line; `SameSite=Lax` still blocks the worst CSRF, but the `Secure` flag and HSTS are the load-bearing losses (session cookie can leak over an accidental plain-HTTP hop; no HSTS pinning).
- **Recommendation:** Consider inverting the default (secure-by-default with an explicit `--insecure-dev` opt-out), or at minimum split HSTS/CSP emission from the cookie flag so header hardening isn't coupled to the dev-cookie toggle. Keep the WARN.
- **Dedup:** none.

### MEDIUM-2 Unauthenticated telemetry relays forward to internal infra with no rate limit

- **Severity:** Medium
- **Claim:** `/api/otlp/v1/traces` and `/api/sentry-relay` are unauthenticated ingress points that forward to the internal OTel collector and to Sentry respectively. Both bound body size but neither rate-limits, and the Sentry relay's only gate (matching DSN) uses a public key that ships in the browser bundle.
- **Evidence:**
  - `internal/web/otlp_relay.go:83-147` — no auth; 4 MiB cap; forwards verbatim (incl. `Content-Encoding`) to the fixed collector URL. Code itself notes (lines 27-33) the cap is on *encoded* bytes, so a small gzip body can inflate past 4 MiB at the collector.
  - `internal/web/sentry_relay.go:90-164` — DSN-validated, 1 MiB cap; but the DSN public key is client-side by design, so a crafted envelope with the correct DSN passes `validateEnvelopeDSN`.
  - `internal/web/server.go:105-114` — both mounted on the mux with no auth middleware in the chain (auth lives inside the ConnectRPC handlers, which these endpoints bypass).
- **Impact:** An attacker can flood the internal collector with garbage spans (polluting/DoSing observability) or burn the project's Sentry quota and pollute error data. The OTLP target is fixed (not an open forwarder — good), so the worst case is amplification into internal infra, not SSRF.
- **Recommendation:** Proportionate to hobbyist scale — add a lightweight per-IP throttle (or a shared token the SPA holds) on these two routes, or explicitly accept and document the residual (the OTLP file already documents part of it). Not urgent, but it's the only unauthenticated path into internal infra.
- **Dedup:** none.

### LOW-1 Telnet ingress does not process IAC/telnet negotiation; IAC bytes reach the command parser

- **Severity:** Low
- **Claim:** The telnet read loop is a raw line scanner with no telnet-protocol (IAC / option-negotiation) handling; `sanitizeTelnetOutput` is applied on the *output* boundary only, so IAC (`0xFF`) and negotiation bytes from a conforming telnet client flow into the command line as input.
- **Evidence:**
  - `internal/telnet/gateway_handler.go:206-216` — `bufio.NewScanner(h.reader)` reads `strings.TrimSpace(scanner.Text())` with no IAC filtering. A repo-wide grep for `IAC|0xff|WILL|DO |subnegotiat` in `gateway_handler.go` returns zero hits.
  - `internal/telnet/sanitize.go:29` — `sanitizeTelnetOutput` exists but is an output-side flattener; there is no input-side equivalent.
- **Impact:** Primarily compatibility/UX: a real telnet client's initial `IAC WILL/DO` negotiation corrupts the first command(s); binary control bytes reach the downstream command parser (which validates and is defended by output sanitization on the way back). Low security impact, but a robustness gap.
- **Recommendation:** Strip/answer IAC sequences at the input boundary (a small telnet state machine, or at minimum drop `0xFF`-introduced sequences) before line assembly.
- **Dedup:** none.

### LOW-2 CSP response header has no script-src/default-src fallback

- **Severity:** Low
- **Claim:** The CSP *header* sets only `frame-ancestors`, `base-uri`, and `object-src`; the script/style/connect policy lives solely in the SvelteKit `<meta>` tag. If the meta tag is ever stripped or a build misconfigures its hashes, there is no header-level script-execution fallback.
- **Evidence:** `internal/web/security_headers.go:53-55` — `hdrCSPValue` = `"frame-ancestors 'none'; base-uri 'self'; object-src 'none'"`, deliberately omitting `default-src`/`script-src` (documented at `security_headers.go:44-52`, load-bearing to avoid the `holomush-11ape` blank-page regression).
- **Impact:** Defense-in-depth gap only — the split is intentional and correct for the static-adapter hashed-bootstrap constraint. Recording it so the coupling is visible.
- **Recommendation:** No action required; if feasible, a header-level `default-src 'self'` that still permits the hashed inline bootstrap would restore the fallback, but this is genuinely constrained by adapter-static. Accept as-is.
- **Dedup:** none.

### TRACKED-1 No per-IP auth throttling; IP not threaded to auth; account-lockout is itself a DoS vector

- **Severity:** Medium (folded into existing tracking)
- **Claim:** Auth rate-limiting is **per-account** (7 failures → 15-min lockout, progressive delay, CAPTCHA at 4) with no per-IP dimension, and the client IP is never threaded from the perimeter to the auth service, so (a) credential-spray across many usernames is unthrottled and (b) any *known* username can be locked out by an attacker making 7 failed attempts.
- **Evidence:**
  - `internal/auth/ratelimit.go:39-69` + `internal/auth/registration.go:88,104-105` — per-account `RecordFailure`/`IsLocked`/`AUTH_ACCOUNT_LOCKED`.
  - `internal/grpc/auth_handlers.go:224` — `s.authService.AuthenticatePlayer(ctx, req.Username, req.Password, "", "")` passes empty `userAgent`/`ipAddress`; the audit IP field is always blank.
- **Impact:** Griefing (lock a rival's account for 15 min) and unthrottled username enumeration/spray at the network layer. Bounded and self-healing, so proportionate for hobbyist scale.
- **Recommendation:** Thread remote IP (respecting a trusted `X-Forwarded-For` from the operator's proxy) into `AuthenticatePlayer`/`CreateGuest` and add per-IP caps — exactly the tracked work.
- **Dedup:** already-tracked:#4606 (no IP-based auth rate limiting), already-tracked:#4676 (thread UA/IP + per-IP caps). The account-lockout-as-DoS nuance is worth adding to #4606's notes.

## Strengths

- **Slowloris defenses are real and correct.** `internal/telnet/deadline_reader.go:25-40` refreshes the read deadline before *every* Read (idle measured from last byte, not connect), and `Limits`/`WriteTimeout`/`PreAuthTimeout` (`internal/telnet/limits.go:15-48`, wired `cmd/holomush/gateway.go:359-363`) bound write and pre-auth resource holding. Input is capped: `maxLineSize = 8 KiB` scanner buffer (`gateway_handler.go:38,208`) and `maxCommandSize = 4 KiB` on forwarded generic commands (`gateway_handler.go:905`).
- **Connection cap + graceful refusal.** Global semaphore `slots` (default 1000) with `RefuseOverCapacity` (`cmd/holomush/gateway.go:358,550`, `internal/telnet/refuse.go:26`) and a metric on refusal.
- **Auth-before-command posture is enforced at the gateway.** `handleSay`/`handlePose`/`handleGenericCommand` (`gateway_handler.go:813,854,895`) all reject with `if !h.authed` before forwarding; `SelectCharacter` verifies the character belongs to the authenticated player (`internal/grpc/auth_handlers.go:276-294`) — no IDOR.
- **Output sanitization prevents terminal-escape injection.** `sanitizeTelnetOutput` (`internal/telnet/sanitize.go:29-111`) strips ANSI CSI/OSC, C0/C1 controls, and DEL on the send boundary, so crafted names/messages can't hijack another user's terminal.
- **Session cookie hardening + anti-spoofing.** `HttpOnly`, `Secure` (default-on in construction), `SameSite=Strict` in secure mode (`internal/web/cookie.go:45-60`); the internal `X-Session-Token` inject header is stripped from inbound requests (`cookie.go:80`) and set only from the cookie — a client cannot spoof the session via header. This SameSite-strict cookie is the CSRF defense for the ConnectRPC POSTs and is the right shape for a same-origin SPA.
- **CORS is exact-match with credentials, no reflected-origin bug.** `internal/web/cors.go:36-43` reflects `Origin` only after `slices.Contains(allowlist, origin)`; `Vary: Origin` always set.
- **ConnectRPC error translation is opacity-preserving.** `internal/web/status_interceptor.go:84-88` extracts the innermost `GRPCStatus()` via `errors.As` and uses `st.Message()` (the core handler's already-sanitized message), never the oops chain — compliant with `.claude/rules/grpc-errors.md`.
- **TLS/mTLS is modern.** P256 ECDSA, `crypto/rand`, `MinVersion: TLS13`, server uses `RequireAndVerifyClientCert`, client pins `ServerName` to the game-id SAN; keys written `0o600`, dirs `0o700` (`internal/tls/certs.go:311-366, 209, 234, 139`).
- **Admin socket access model is sound.** UDS-only (TCP "structurally impossible"), socket `0o600` inside an `0700` XDG runtime dir, flock double-start guard; peer-cred is explicitly *audit-only, not a defense factor* (`internal/admin/socket/server.go:78-101`, `peercred.go:16-17`) with authorization handled by TOTP/approval handlers — the correct model for a local admin socket.
- **Credential checks resist enumeration & timing.** Dummy argon2id hash for non-existent users with params matched to the real hasher (`internal/auth/auth_service.go:122-131`, `hasher.go:28`); `RequestPasswordReset` returns uniform `Success: true` regardless of email existence (`internal/grpc/auth_handlers.go:642-656`); reset tokens never logged; username regex is allowlist-only (`internal/auth/player.go:31,153-171`).
- **NATS external-mode scoping is fail-closed by design.** Single-principal account grants only `events.>/audit.>/internal.>/_INBOX.>` with a boot-time over-scope self-check (`deploy/nats/holomush-server.account.conf`, `cluster-server.conf`); placeholders are clearly marked and README points to nsc/JWT for production.
- **Telemetry relays are not open forwarders.** OTLP target is fixed server-side (`otlp_relay.go:69`); Sentry relay validates the envelope DSN against config (`sentry_relay.go:170-193`); both fail-closed (route left unregistered) on config parse error (`server.go:87-114`).

## Not examined

- **Per-RPC authorization inside each web BFF handler** (`handler.go`, `scene_handlers.go`): I confirmed the token-flow pattern (stripped cookie → `headerInjectSessionToken` → core validates, e.g. `scene_handlers.go:30,65,97`) but did not audit every individual RPC for a missing-authz path — that overlaps D2 (ABAC) and the gateway-boundary structural-write rules.
- **Binary-plugin mTLS / emit-token store internals** (`internal/plugin/goplugin/`): the broker/mTLS handshake and `emit_token_store.go` are perimeter-adjacent but sit inside the plugin trust boundary (D1/plugin dimension); I only confirmed the host uses mTLS. Plugin-runtime hardening is already tracked (#4675).
- **Guest-creation flood bound** (`CreateGuest`): a guest reaper exists (`internal/auth/guest_reaper.go`) for cleanup, but I did not fully trace whether guest *creation* rate is bounded independently of the per-IP gap in TRACKED-1.
- **Event-payload crypto and ABAC engine internals** — excluded by assignment (D3/D2); only their perimeter integration points (session-token flow, mTLS, subject scoping) were considered.
- **`session_connections.client_type` unsanitized storage** — noted as already-tracked:#4759; the telnet side hardcodes `"telnet"` (`gateway_handler.go:675`), the web side takes it from the client. Not re-analyzed.
