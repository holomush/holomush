# Skeptic verification — D6-HIGH-1 (audit-DLQ replay CLI `game_id` split)

**Verdict: CONFIRMED** (core mechanism), with one factual correction to the
claim's deployment-scope framing that changes blast radius but not severity
within that scope.

## What I set out to refute

The claim: the replay CLI reads a different `game_id` than the server's DLQ
subject uses, there is no `--game-id` flag to bridge them, and this breaks
recovery "in the documented zero-config default deployment."

I tried to find any of the following that would refute it: the two values
actually coincide by default; a bridging flag/mechanism exists; the docs
already tell operators to align the two settings; or the "zero-config
default deployment" framing is simply correct as stated. Three of four hold
up. The fourth — the deployment-scope framing — does not; embedded/zero-config
mode cannot run this CLI at all, so the real-world manifestation is scoped to
the *external-NATS* deployment, not the zero-config default.

## (a) Server DLQ subject's `game_id` source

`cmd/holomush/core.go:300-304`:

```go
gameID := cfg.GameID
if gameID == "" {
    gameID = dbSub.GameID()
}
```

`cfg` here is `coreConfig` (`cmd/holomush/core.go:65-70`), bound to koanf
section `"core"` via `config.Load(configFile, cmd, cfg, "core")`
(`cmd/holomush/core.go:125`) and to CLI flag `--game-id`
(`cmd/holomush/core.go:161`: `cmd.Flags().StringVar(&cfg.GameID, "game-id", "",
"game ID (default: auto-generated from database)")`). When unset,
`dbSub.GameID()` resolves to a value minted once and persisted in Postgres:
`internal/store/postgres.go:96-113` (`InitGameID`) —
`gameID = core.NewULID().String()` — a ULID, never the literal `"main"`.

This resolved `gameID` is then used for the actual DLQ subject at
`cmd/holomush/core.go:561-570`:

```go
auditSub := audit.NewSubsystem(eventBusSub, dbSub, audit.Config{
    DLQ: audit.DLQConfig{
        Subject:  fmt.Sprintf("internal.%s.audit.dlq", gameID),
        ...
```

The comment immediately above (`core.go:563-566`) is a self-aware
acknowledgment of exactly the split under review: *"Use the resolved gameID
(cfg.GameID, else dbSub.GameID()) so the DLQ subject matches the rest of the
process; `eventBusConfig.GameID` can still be the unresolved default. The
replay CLI must target the same game_id — a mismatch fails loud (WR-06),
never silently."*

Note this same resolved `gameID` — NOT `eventBusConfig.GameID` — also drives
TLS CA SAN, `cluster.Config.ClusterID`, `pluginsetup.PluginSubsystemConfig.GameID`,
`dek.SetGameIDForRekey(gameID)`, and `totp.Config{GameID: gameID}`
(`core.go:421,534,696,808`). It is the "real" game identity for the process.
`eventBusConfig.GameID` (`event_bus.game_id`) is a *second*, independently
defaulted value used only to qualify `events.<game_id>...` publish/subscribe
subjects (`cmd/holomush/sub_grpc.go:783`, `b.bus.GameID()` →
`internal/eventbus/subsystem.go:412`, `return s.cfg.GameID`). Confirmed via
`internal/eventbus/config.go:28,42,153-154`: koanf tag `game_id`, default
`"main"` applied by `Config.Defaults()`.

So the codebase already runs two independent `game_id` concepts server-side;
the DLQ-subject one is the "real" resolved identity, and it is architecturally
guaranteed to differ from `event_bus.game_id`'s default unless an operator
manually sets both to the same literal value (nothing forces or checks this).

## (b) CLI's `game_id` source

`cmd/holomush/cmd_audit.go:141-149` (`loadEventBusConfig`):

```go
func loadEventBusConfig(cmd *cobra.Command) (eventbus.Config, error) {
    cfg := eventbus.Config{}
    if err := config.Load(configFile, cmd, &cfg, "event_bus"); err != nil {
        return cfg, oops.Code("AUDIT_DLQ_CONFIG_FAILED").Wrap(err)
    }
    return cfg, nil
}
```

Only the `event_bus` koanf section is loaded — `core.game_id` is never read
by the CLI path. `config.Load` (`internal/config/config.go:244-277`) does
**not** call `.Defaults()` on the target (confirmed by reading the function
body — it stops after `UnmarshalWithConf`); the caller is responsible for
that. `loadEventBusConfig` never calls `eventBusConfig.Defaults()` either
(unlike the server boot path at `core.go:140`, which does:
`eventBusConfig = eventBusConfig.Defaults()`). So `cfg.GameID` is genuinely
`""` unless `event_bus.game_id` is explicitly set in the config file.

`cmd/holomush/cmd_audit.go:325`: `audit.ReplayDLQ(cmd.Context(), js, pool,
dlqConfigForGame(cfg.GameID), opts)`.

`cmd/holomush/cmd_audit.go:337-343` (`dlqConfigForGame`):

```go
func dlqConfigForGame(gameID string) audit.DLQConfig {
    cfg := audit.DLQConfig{}
    if gameID != "" {
        cfg.Subject = fmt.Sprintf("internal.%s.audit.dlq", gameID)
    }
    return cfg
}
```

With an empty `gameID`, `cfg.Subject` stays `""`. Inside
`audit.ReplayDLQ` (`internal/eventbus/audit/replay.go:82`): `cfg =
cfg.Defaults()`, which (`internal/eventbus/audit/dlq.go:69-80`) resolves an
empty `Subject` to `defaultDLQSubject = "internal.main.audit.dlq"`
(`internal/eventbus/audit/dlq.go:24-37`, `defaultDLQGameID = "main"`).

So: server's real DLQ subject = `internal.<ULID>.audit.dlq`; CLI's default
expected prefix = `internal.main.audit.dlq`. These do not match in the
default case, confirmed precisely.

## (c) No bridging flag

`cmd/holomush/cmd_audit.go:107-119` (`newAuditDLQReplayCmd`) registers only
`--all`, `--msg-id`, `--limit`. Grepped the entire `cmd/holomush` package for
`"game-id"`: the **only** registration is `cmd/holomush/core.go:161`, local
to the `core` subcommand and bound to koanf section `"core"` (not
`"event_bus"`). Root persistent flags are only `--config` and `--log-level`
(`cmd/holomush/root.go:31-33`) — `cmd.Flags()` inside `loadEventBusConfig`
inherits those two and nothing else, so no `--game-id` value could reach
`event_bus.game_id` via flag overlay even indirectly. Confirmed: no bridging
flag exists anywhere in the CLI surface.

I also checked whether the CLI could resolve the true game id via the
already-open Postgres pool (`openAuditPool`, `cmd_audit.go:319`, which reads
the same `events_audit`/`system_info` database) — it does not; `ReplayDLQ` is
called with only `dlqConfigForGame(cfg.GameID)`, never consulting
`PostgresEventStore.GetSystemInfo(ctx, "game_id")`. That omission is exactly
the finding's own suggested fix, which I confirm is unimplemented.

## (d) Documented operator step to align them? No.

`site/src/content/docs/operating/how-to/external-nats-deployment.md` is the
**only** documented runbook that exercises `holomush audit dlq replay`
(confirmed by grep across `site/src/content/docs/`). Its Step 3 config
example (lines 110-123) sets `event_bus.mode/url/credentials/tls/provision/dlq`
but never `game_id`, in either the `event_bus:` or a `core:` block, and its
key-reference table (lines 125-133) doesn't mention `game_id` at all. Step 6
(lines 225-265, the DLQ operate/replay section) also never mentions
`game_id`. No other operating doc cross-references the two settings. This
confirms the claim's evidence line: the reference/documented path reproduces
the mismatch exactly as described, with nothing telling the operator to
reconcile the two keys.

## Where the original claim overstates: "zero-config default deployment"

This is the one point that does **not** hold up as literally stated, and
it's worth being precise about it because it changes who is actually
affected.

`cmd/holomush/cmd_audit.go:124-128` (`dialAuditJetStream`):

```go
if cfg.URL == "" {
    return nil, nil, oops.Code("AUDIT_DLQ_NATS_URL_MISSING").
        Errorf("event_bus.url is required (external NATS URL) for audit dlq commands")
}
```

Every `audit dlq` subcommand (`list`/`show`/`replay`) goes through this and
hard-fails if `event_bus.url` is empty. In the true zero-config default
(embedded NATS), `event_bus.url` is never set, and embedded mode itself does
not listen on any TCP endpoint at all —
`internal/eventbus/subsystem.go:153` (`connectEmbedded`): `DontListen:
true`, connected only via `nats.InProcessServer(s.server)`
(`subsystem.go:179`) — an in-process Go call, unreachable from a separate CLI
process invocation under any URL. So the `audit dlq` command family is
**architecturally inapplicable** to the zero-config embedded deployment; it
cannot even attempt to connect, let alone reach the game_id-mismatch code
path.

Confirmed by the docs themselves —
`site/src/content/docs/operating/how-to/external-nats-deployment.md:11-14`:
*"Embedded NATS stays the zero-config default — external mode is a
deliberate opt-in for horizontally-scaled multi-node deployments. If you run
a single core process, you do not need this guide."* The **only** documented
runbook for `audit dlq replay` is this external-NATS, explicitly-opt-in
guide — not "the documented zero-config default deployment."

So the correct scope statement is: **any deployment that has opted into
external NATS mode (a deliberate, documented choice) and follows the
external-NATS runbook's own Step 3 config example literally** will hit this
bug — not literally every zero-config HoloMUSH installation, most of which
never touch `audit dlq` at all. This narrows the blast radius considerably
(it only bites operators who've already taken the extra step of standing up
external NATS) but does not make the underlying defect any less real for
that population, and per this repo's own recent history (PR #4782,
"external/clustered NATS deployment... Phase 3", the branch tip at HEAD) that
population is exactly the audience the current work targets.

## Secondary correction: "fails loud" is closer to true than the claim implies

The claim's headline framing ("gets `replayed: 0`") reads as if the failure
is silent/opaque. In practice, per `internal/eventbus/audit/replay.go:120-166`,
`OrderedConsumer`/`Fetch` reads the **whole stream** with no subject filter
(`DeliverPolicy: jetstream.DeliverAllPolicy`, no `FilterSubject`), so
`result.Scanned` is non-zero and accurate (it reflects the real dead-letter
count, independent of the CLI's guessed `Subject`). Per-message,
`originalSubject` (`replay.go:249-260`) fails the prefix match and
`replayOne` (`replay.go:192-218`) increments `result.Failed` (not silently
dropped) and logs `slog.WarnContext(ctx, "audit DLQ replay: DLQ subject does
not carry the expected prefix; not persisted (replay game_id likely differs
from capture-time game_id)", ...)` — an operator who reads logs gets the
exact diagnosis. `renderReplayResult` (`cmd_audit.go:361-371`) prints
`scanned: N`, `replayed: 0`, `failed: N`, and "(failed dead letters are
retained in the DLQ for inspection)" to stdout. So the tool is honest about
failure (`failed: N` ≠ 0, not just a quiet `replayed: 0` with `scanned: 0`)
even though it gives no actionable next step (no `--game-id` flag to try).
The claim's own Evidence section already states this correctly ("the failure
is at least loud, not silent"); only the HIGH-1 headline/Claim sentence
slightly oversells the silence. Non-blocking nuance, not a refutation.

## Verdict

**CONFIRMED.** All four items I was asked to verify hold up under direct
code citation:

1. **(a)** Server DLQ subject's `game_id`: `cmd/holomush/core.go:300-304`
   (resolve) → `core.go:561-570`/`567` (`Subject: fmt.Sprintf("internal.%s.audit.dlq",
   gameID)`); falls back to `internal/store/postgres.go:96-113` (`InitGameID`,
   mints a ULID).
2. **(b)** CLI's `game_id`: `cmd/holomush/cmd_audit.go:143-149`
   (`loadEventBusConfig`, section `"event_bus"` only, no `.Defaults()` call)
   → `cmd_audit.go:325` → `cmd_audit.go:337-343` (`dlqConfigForGame`) → empty
   `Subject` → `internal/eventbus/audit/replay.go:82`
   (`cfg.Defaults()`) → `internal/eventbus/audit/dlq.go:24-37,69-80`
   (`defaultDLQSubject = "internal.main.audit.dlq"`).
3. **(c)** No bridging flag: `cmd/holomush/cmd_audit.go:107-119` (only
   `--all`/`--msg-id`/`--limit`); the only `--game-id` flag in the binary is
   `cmd/holomush/core.go:161`, scoped to koanf section `"core"`, unreachable
   from the audit command tree.
4. **(d)** No documented alignment step:
   `site/src/content/docs/operating/how-to/external-nats-deployment.md`
   Step 3 (lines 110-133) and Step 6 (lines 225-265) never mention `game_id`.

**Correction to the claim:** drop "zero-config default deployment" — the
`audit dlq` command family requires `event_bus.url`
(`cmd/holomush/cmd_audit.go:124-128`) and cannot reach the embedded,
`DontListen: true` NATS server at all (`internal/eventbus/subsystem.go:153`),
so it is inapplicable to the actual zero-config default. Replace with: "any
deployment that has opted into external NATS mode and follows the
documented `external-nats-deployment.md` runbook's own config example
literally." Severity stays **High** within that corrected scope — it breaks
the one tool that exists specifically to make external-NATS audit dead
letters recoverable, for every operator who follows the current docs as
written, and there is no test in the suite
(`internal/eventbus/audit/dlq_replay_integration_test.go:25-27`, hardcodes
`game "main"` on both capture and replay sides) that would have caught it.
