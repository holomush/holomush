<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# D4 Perimeter & Platform Security — Findings

**Agent:** security-auditor (Opus 4.8) · **Date:** 2026-07-11 · **Scope examined:** `internal/auth/` (argon2id, session-ownership, credential validation, rate-limit, reset, guest), `internal/totp/`, `internal/session/` (spot), `internal/telnet/` (gateway_handler, sanitize, limits, guest), `internal/web/` (cookie, cors, security_headers, server, auth_handlers, sentry_relay, otlp_relay), `internal/admin/` (socket/peercred, socket/server, approval handler+repo), `internal/plugin/goplugin/` (emit_token_store, host mTLS), `internal/plugin/event_emitter.go`, `internal/plugin/lua/state.go`, `internal/plugin/schema_provisioner.go`, `internal/eventbus/natsdial.go` + `deploy/nats/`, gRPC error hygiene, SQL parameterization.

## Summary

The perimeter is in strong shape for the project's stated hobbyist/community scale. The high-risk classes this review targets — **auth bypass, sandbox escape, credential leak, unauthenticated admin access, injection** — were each examined and none was found. Authentication (argon2id + dummy-hash timing defense + account lockout), TOTP (transactional replay defense + constant-time), session/reset tokens (crypto/rand + hashed-at-rest + constant-time + enumeration-resistant error collapsing), the binary-plugin anti-forgery emit-token store, the Lua sandbox (os/io/debug/package + load-family all removed), production-default mTLS, NATS credential redaction, and admin dual-control (atomic self-approval rejection over a UDS-only, filesystem-gated socket) are all implemented to a notably high standard. gRPC inner-error `%v` leaks are confined to `plugins/core-scenes`/`internal/world` (already tracked).

**Counts:** Blocker 0 · High 0 · Medium 2 (1 untracked, 1 tracked) · Low 4 · Strengths 13.

Top finding: **MEDIUM-1** — web `Secure`/HSTS/CSP are gated on a manually-set flag that defaults **false**, a fail-open default that silently ships session cookies without `Secure` and drops HSTS + CSP whenever an operator forgets `--secure-cookies` (the common TLS-terminated-reverse-proxy case).

## Findings

### MEDIUM-1 Session-cookie `Secure`, HSTS, and CSP all gated on a fail-open flag defaulting false

- **Severity:** Medium
- **Claim:** The `Secure` cookie attribute, `Strict-Transport-Security`, and the header-half `Content-Security-Policy` are only emitted when `Config.Secure == true`, which is bound to `--secure-cookies` (default `false`) with no coupling to actual TLS; forgetting the flag in a TLS-terminated deployment ships session cookies without `Secure` and disables HSTS + CSP.
- **Evidence:** `cmd/holomush/gateway.go:120` (`BoolVar(&cfg.SecureCookies, "secure-cookies", false, ...)`), `:314` (`Secure: cfg.SecureCookies`); `internal/web/security_headers.go:80-83` (HSTS + CSP set only `if secure`); `internal/web/cookie.go:45-59` (cookie built `Secure:true` then **downgraded** to `Secure:false`/`SameSite=Lax` when `!secure`); the only guardrail is a startup `slog.Warn` at `internal/web/server.go:55-66`.
- **Impact:** Operators fronting the gateway with a TLS-terminating proxy (stunnel/haproxy/nginx/k8s ingress — the app then sees plain HTTP) must remember to set `--secure-cookies=true` even though the app's own listener is not TLS. If forgotten: the `holomush_session` cookie can be sent/observed over any plain-HTTP hop, and the browser loses HSTS downgrade protection and the CSP clickjacking/`base-uri`/`object-src` controls. A single missed flag turns a TLS deployment into a cookie-theft + weakened-XSS-posture deployment; the WARN is easy to miss in production log volume.
- **Recommendation:** Default `SecureCookies` to `true` and require an explicit `--dev-insecure` (or `--allow-insecure-cookies`) opt-out for local plain-HTTP dev; alternatively derive `Secure` from a deployment/`X-Forwarded-Proto`-aware signal. Fail-safe defaults belong on the secure side.
- **Dedup:** none

### MEDIUM-2 No Lua execution timeout / instruction watchdog — a runaway handler wedges event delivery

- **Severity:** Medium
- **Claim:** `StateFactory.NewState` takes a `context.Context` that is explicitly ignored ("reserved for future cancellation/timeout support"); there is no CPU/time/instruction bound on Lua handler execution, so a plugin containing `while true do end` blocks the delivering goroutine indefinitely.
- **Evidence:** `internal/plugin/lua/state.go:72-77` (`NewState(_ context.Context)` — ctx unused; only `RegistryMaxSize` is set, which bounds the value registry, not CPU). Delivery paths create a fresh state and run plugin code with no deadline (`internal/plugin/lua/host.go:352,499,605,733`).
- **Impact:** Plugins are operator-installed (semi-trusted), so this is primarily a robustness/DoS concern rather than a hostile-code boundary: a buggy or maliciously-crafted in-tree Lua plugin can hang a delivery worker and starve event processing. `RegistryMaxSize` does not help — a tight loop consumes no registry.
- **Recommendation:** Thread the delivery `ctx` into a `lua.LState.SetContext(ctx)` (gopher-lua supports context-based cancellation) with a per-delivery deadline, so a runaway handler is aborted with an error instead of wedging the goroutine.
- **Dedup:** already-tracked:#4675 (Plugin runtime hardening: proxy goroutine cleanup, **Lua ctx watchdog**, mTLS-by-default)

### LOW-1 Argon2id time cost `t=1` is below RFC 9106's `t=3` recommendation for the 64-MiB profile

- **Severity:** Low
- **Claim:** The hasher uses `m=64 MiB, t=1, p=4`. Memory (64 MiB) is generous and exceeds OWASP's 19-MiB minimum, but RFC 9106 §4's 64-MiB profile pairs that memory with **t=3** (its `t=1` profile is the 2-GiB one). The chosen `t=1` at 64 MiB is a defensible-but-under-recommended time cost.
- **Evidence:** `internal/auth/hasher.go:30-36` (`argon2Time=1, argon2Memory=64*1024, argon2Threads=4`). RFC 9106 §4: first config `t=1,p=4,m=2 GiB`; second config `t=3,p=4,m=64 MiB` (https://www.rfc-editor.org/rfc/rfc9106.html §4). OWASP minimum `m=19 MiB,t=2,p=1` (https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html).
- **Impact:** Marginally weaker resistance for an attacker who has provisioned the 64 MiB — the memory-hardness still dominates, so the practical delta is small at hobbyist scale.
- **Recommendation:** Raise `argon2Time` to `3` to match RFC 9106's 64-MiB profile (and update the paired `dummyPasswordHash` `t=` value in lockstep — `internal/auth/auth_service.go:131`, enforced by `internal/auth/dummy_hash_test.go`). Benchmark the resulting login latency first.
- **Dedup:** none

### LOW-2 Username-enumeration timing side channel: failure path does an extra DB write only for existing users

- **Severity:** Low
- **Claim:** On an invalid password, `ValidateCredentials` calls `player.RecordFailure()` + `s.players.Update(ctx, player)` (a DB write) only when the player exists; a non-existent username skips it. The argon2 dummy-hash equalizes the *hashing* cost but not this extra DB round-trip, leaving a measurable "exists vs not" timing delta.
- **Evidence:** `internal/auth/registration.go:86-101` — the `if playerExists { player.RecordFailure(); s.players.Update(...) }` block runs only for existing users; the non-existent branch returns immediately after the dummy-hash `Verify`.
- **Impact:** An attacker measuring response-time distributions could distinguish valid from invalid usernames despite the dummy-hash mitigation. The DB-write delta is smaller and noisier than an argon2 compute delta, so exploitation is non-trivial but the channel is real.
- **Recommendation:** Make the failure-path timing uniform (e.g., perform an equivalent throw-away write, or move failure-counting to an async best-effort path that does not sit in the synchronous response), or accept as residual and document. Per-IP rate limiting (LOW-4 / #4606) also blunts the measurement.
- **Dedup:** none

### LOW-3 Admin socket bind→chmod race leaves a sub-`0600` window; non-default socket paths lose the parent-dir gate

- **Severity:** Low
- **Claim:** `net.Listen("unix", …)` creates the socket with umask-default permissions and `os.Chmod(0600)` is applied *after*; a brief window exists where the socket is more permissive than 0600. The design relies on the parent directory (XDG runtime dir, 0700) as the primary gate, which holds for the default path but not for an operator-supplied `--socket` in a world-writable directory.
- **Evidence:** `internal/admin/socket/server.go:78-92` (Listen then Chmod, with the comment naming the parent dir as the primary gate); `cmd/holomush/admin_client.go:29-45` (`--socket` override; default is `xdg.RuntimeDir()/admin.sock`). PeerCred is audit-only, not a defense factor (`internal/admin/socket/peercred.go:16-17`), so filesystem perms are the sole access control.
- **Impact:** On the default path the 0700 runtime dir closes the window. A misconfigured `--socket /tmp/admin.sock` (or any non-0700 parent) exposes the admin RPC surface — including the crypto-operator approval flow — to any local user during the race and, for a world-writable parent, persistently.
- **Recommendation:** Set `umask(0177)` immediately around the `net.Listen` call (restore after), or `MkdirTemp`-style create the socket inside a freshly-made 0700 directory; and reject a configured `SocketPath` whose parent directory is not 0700-or-tighter and owned by the process UID.
- **Dedup:** none

### LOW-4 Progressive-delay + CAPTCHA rate-limit logic is dead code; only the 7-failure account lockout is wired

- **Severity:** Low
- **Claim:** `CheckFailures` (exponential backoff delay + CAPTCHA-required signalling) is never called by any production caller; the only enforced control is the hard account lockout at `LockoutThreshold=7` failures. There is no per-IP throttle, so cross-account password spraying from one IP is unthrottled.
- **Evidence:** `internal/auth/ratelimit.go:39-69` (`CheckFailures`) — the only non-test reference to it is its own definition (verified `rg 'CheckFailures\(' internal/ | rg -v _test` returns only the declaration). Enforced lockout lives at `internal/auth/player.go:135-138` + `registration.go:104-108`.
- **Impact:** The progressive-delay/CAPTCHA hints an operator might expect from reading the code do not exist at runtime; password spraying across many usernames from one source is not rate-limited (per-account lockout only slows repeated hits on a single account).
- **Recommendation:** Either wire `CheckFailures` (delay + `RequiresCaptcha`) into the auth handlers, or delete it to avoid a false sense of coverage. Per-IP caps are the real gap.
- **Dedup:** partially already-tracked:#4606 (No IP-based rate limiting on authentication endpoints); the dead-code aspect is not tracked.

### INFO Session `client_type` stored unsanitized

- **Severity:** Low (informational — dedup)
- **Claim:** Telnet passes the constant `"telnet"` for `ClientType` (`internal/telnet/gateway_handler.go:675`), but the web/BFF path threads a client-supplied value that reaches `session_connections.client_type` unsanitized.
- **Evidence:** already characterized in the tracked issue; not re-verified end-to-end this session.
- **Recommendation:** Constrain `client_type` to an enum/allowlist at the ingest boundary.
- **Dedup:** already-tracked:#4759

## Strengths

- **Argon2id done right otherwise:** OWASP-aligned 64-MiB memory, `crypto/rand` salt, PHC encoding with overflow/`uint8`-truncation guards, `subtle.ConstantTimeCompare`, plus an oversized-password pre-hash rejection to prevent memory exhaustion, and a parameter-matched dummy hash for non-existent-user timing parity (`internal/auth/hasher.go:63-149`, `internal/auth/registration.go:50-54`, `internal/auth/auth_service.go:122-131`, guarded by `internal/auth/dummy_hash_test.go`).
- **TOTP replay/lockout/timing:** monotonic `LastUsedStep` replay defense with a typed-sentinel ROLLBACK, threshold lockout, and a skew loop that iterates all steps under `subtle.ConstantTimeCompare` *without early break* to avoid a timing leak, all inside one transaction; secret KEK-wrapped at rest (`internal/totp/service.go:255-326`).
- **Session/reset/ownership tokens:** `crypto/rand` tokens, SHA-256-hashed at rest, constant-time verification, atomic `DELETE … RETURNING` reset consumption, and `ValidateSessionOwnership` collapsing *every* failure to `SESSION_NOT_FOUND` to defeat enumeration (`internal/auth/player_session.go:145-165`, `internal/auth/reset.go:69-106`, `internal/auth/session_ownership.go:16-101`). Password-reset returns success-with-empty-token for unknown emails (`internal/auth/reset_service.go:90-99`).
- **Binary-plugin actor anti-forgery:** the emit-token store issues a per-dispatch 128-bit `crypto/rand` token, binds the *host-vouched* actor + dispatch context to it, and `EmitEvent` uses the stored actor verbatim — ignoring any kind/id the out-of-process plugin claims in gRPC metadata (INV-PLUGIN-51) — with defense-in-depth plugin-name tagging and terminal-on-close semantics (`internal/plugin/goplugin/emit_token_store.go:20-169`).
- **Universal emit gate:** the shared `PluginEventEmitter.Emit` host-resolves the actor and manifest-gates its kind (`actor_kinds_claimable`) for *both* Lua and binary runtimes at one chokepoint (`internal/plugin/event_emitter.go:112-136`).
- **Lua sandbox:** `SkipOpenLibs:true`, only base/table/string/math opened, and `os`/`io`/`debug`/`package` never loaded plus `load`/`loadstring`/`loadfile`/`dofile` explicitly nil'd — closing the filesystem and dynamic-code escape vectors (`internal/plugin/lua/state.go:23-96`).
- **mTLS on by default in production:** the binary-plugin host is wired with `WithCA` whenever a certs dir exists, and production always populates it (`ensureTLSCerts` generates a CA + certs unconditionally); the transport is TLS 1.3 with `RootCAs`/`GetClientCertificate` mutual auth (`internal/plugin/setup/subsystem.go:312-319`, `cmd/holomush/core.go:307,420,1308-1341`, `internal/plugin/goplugin/host.go:1599-1607`). The no-CA fallback is dev/test-only and emits a loud one-shot WARN (`host.go:372-384`).
- **NATS credential redaction (WR-02 discipline):** `redactURL` strips URL userinfo, handles comma-separated seed lists per-seed, prepends a scheme so scheme-less `user:pass@host` is recognized, and drops unparseable seeds rather than risk a leak; auth uses a `.creds` file, and no raw URL is logged on the success path (`internal/eventbus/natsdial.go:42-107`). Least-privilege single-principal subject scoping with a boot self-check and clearly-marked placeholder passwords (`deploy/nats/holomush-server.account.conf`, `internal/eventbus/scopecheck.go`).
- **Relay endpoints are not SSRF/open-forwarders:** the Sentry relay forwards only to the operator-configured DSN's fixed host/project and validates the inbound envelope's embedded DSN before forwarding; the OTLP relay's destination is fixed by config, not the request; both cap body size (`internal/web/sentry_relay.go:80-194`, `internal/web/otlp_relay.go:46-90`).
- **Telnet hardening:** output sanitizer strips ANSI CSI/OSC, C0/C1 controls, DEL, and handles unterminated sequences at the send boundary; Slowloris/pre-auth/write deadlines and 8-KiB line / 4-KiB command caps bound resource use; cleartext-password-over-telnet risk is documented with a TLS-front recommendation (`internal/telnet/sanitize.go`, `internal/telnet/limits.go:15-48`, `internal/telnet/gateway_handler.go:37-51,486-491,905-908`).
- **Web CSRF/cookie posture:** `HttpOnly` + `Secure` + `SameSite=Strict` session cookie; client-supplied `X-Session-Token` is stripped inbound and re-injected only from the cookie (anti-spoof); outbound token-signal headers are consumed and deleted by the cookie writer, which correctly implements `http.Flusher` + `Unwrap()` for ConnectRPC streaming. CORS reflects only allowlisted origins alongside `Allow-Credentials` and always sets `Vary: Origin` (`internal/web/cookie.go:45-165`, `internal/web/cors.go:23-55`). CSRF defense rests on SameSite + the ConnectRPC content-type/protocol-header requirement rather than an explicit token — a defensible design for this stack.
- **Admin dual-control:** self-approval is rejected atomically inside the SQL `UPDATE … AND primary_player_id != $2` (not a TOCTOU read-then-check), gated by a session-backed operator identity + `AssertOperatorAdmin`, over a UDS-only socket (never TCP) with a 0700 parent dir + `0600` socket and peer-cred used for audit only (`internal/admin/approval/repo.go:193-236`, `internal/admin/approval/handler.go:42-68`, `internal/admin/socket/server.go:78-92`).
- **Plugin-role DDL injection defense:** identifiers pass a `validPgIdentifier` allowlist and the role password — where SQL parameterization is impossible for DDL — is a `crypto/rand` base64url value from a quote-free charset *and* is run through `validatePostgresPasswordLiteral` before interpolation (`internal/plugin/schema_provisioner.go:21-23,196-249,289-297`).
- **gRPC error hygiene at the host boundary:** no `status.Errorf(codes.Internal, "…%v", err)` inner-error leaks were found in `internal/grpc`, `internal/web`, or `internal/admin`; the known leaks are confined to `plugins/core-scenes` / `internal/world` and are already tracked. Application SQL is parameterized (`$N`) throughout; the only string-interpolated queries interpolate constant column lists, not user input (`internal/access/policy/store/postgres.go:170-443`).

## Not examined

- **ABAC internals** (`internal/access/`) — owned by the ABAC review agent. I treated the emit-gate/approval ABAC checks as chokepoints without auditing the policy engine itself.
- **Event-payload crypto** (`internal/eventbus/crypto`, `codec`, `authguard`, DEK/KEK, the `EnforceSensitivity` fence) — owned by the crypto review agent.
- **Web-client XSS** (`web/src/`) — the `@html`/ansi_up concern is tracked (#4600); not re-verified in the Svelte layer.
- **go-plugin handshake internals** — I confirmed the mTLS config and the loopback/token trust model but did not audit hashicorp/go-plugin's magic-cookie handshake or subprocess-launch argv construction.
- **Full session-store transaction paths** (`internal/session/` reaper/lease, `CreateWithCap` SQL) beyond the auth-facing surface — reviewed only where they intersect eviction/ownership.
- **`internal/tls/` certificate generation internals** (key sizes, validity windows, SAN construction) beyond confirming CA/mTLS wiring — a deeper cert-lifecycle audit (rotation, expiry handling) was out of time budget.
