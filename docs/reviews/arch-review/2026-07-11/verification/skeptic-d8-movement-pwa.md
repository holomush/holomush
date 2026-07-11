<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Skeptic verification — D8b-HIGH-1 (movement) and D8a-HIGH-1 (PWA offline claim)

Adversarial re-derivation, independent of the review's own findings/spot-checks
docs (which were read only to locate the original citations, not trusted).
Working dir: `/Volumes/Code/github.com/holomush/.worktrees/arch-review`.

---

## CLAIM 1 (D8b-HIGH-1): No player-facing command/RPC moves a character

**Verdict: CONFIRMED**

### Hunt 1 — any registered command or dispatcher path that calls `MoveCharacter`?

`rg -n "MoveCharacter" --type go` across the whole repo returns exactly these
non-test/production hits:

- `internal/world/service.go:773` — `func (s *Service) MoveCharacter(...)` (the method itself)
- `internal/world/movement_hook.go:13` — doc comment referencing the method
- `internal/command/types.go:36-37` — the `WorldService` interface declares
  `MoveCharacter(...)` as a method players *could* call through — but declaring
  an interface method is not a call site
- `internal/testsupport/integrationtest/session.go:291` — a comment noting a
  test helper deliberately bypasses `MoveCharacter`

No file in `internal/command/`, `internal/command/handlers/`, `internal/grpc/`,
`plugins/*/*.go`, or `cmd/` calls `MoveCharacter`. `mcp__probe__search_code`
(chosen per the movement-substring display bug) corroborates: zero hits in
`internal/command` or `plugins` for the symbol, zero in `internal/grpc`.

### Hunt 2 — every registered command, across every in-tree plugin manifest

`internal/command/handlers/` contains exactly 6 compiled-in handlers: `output`,
`plugin_admin`, `quit`, `register`, `resetpassword`, `shutdown` — none
movement-related.

Every `plugins/*/plugin.yaml` `commands:` block was enumerated:

| Plugin | Commands |
|---|---|
| core-aliases | alias, unalias, aliases, sysalias, sysunsalias, sysaliases |
| core-building | dig, link |
| core-channels | channel |
| core-communication | say, pose, page, whisper, ooc, pemit, emit, wall |
| core-help | help |
| core-objects | describe, examine, create, set |
| core-scenes | scene, scenes |
| test-abac-widget | widget |
| echo-bot, setting-crossroads, setting-skeleton | (no `commands:` block) |

No `go`/`walk`/`move`/`north`/`south`/direction-named command exists anywhere.
`setting-crossroads/plugin.yaml` confirms `starting_location: "The Nexus"` —
matching the live-test evidence's starting room.

### Hunt 3 — dispatcher-level dynamic exit resolution

Read `internal/command/dispatcher.go` `Dispatch()` in full (lines 144-346).
Command resolution is a **flat registry lookup only**:

```go
// dispatcher.go:269-274
entry, ok := d.registry.Get(parsed.Name)
if !ok {
    metrics.SetStatus(StatusNotFound)
    err = ErrUnknownCommand(parsed.Name)
    return err
}
```

There is no fallback that resolves `parsed.Name` against `WorldService.GetExitsByLocation`
or any exit table. The only dynamic-rewrite mechanism in the dispatcher is
`maybeRedirectForFocus` (scene-focus verb routing for `pose`/`say`/`ooc`/`emit`
→ the `scene` command), which is unrelated to movement and does not consult
exits. `look` and `who` (top-level) are **not registered anywhere in
production code** — `rg 'Name:\s*"look"'` outside `_test.go` returns zero
hits; the only "look" `CommandEntry{}` registrations exist in
`internal/command/registry_test.go` / `alias_test.go` fixtures. The one `"who"`
hit in production code is `plugins/core-channels/commands.go:73`, a
**channel-scoped subcommand** (`channel who <name>`), not a global presence
command. This is exactly consistent with the live evidence: typing `look` or
`who` at the top level returns `ErrUnknownCommand` → "Unknown command."

### Hunt 4 — web exit-click / typed-direction path

`web/src/routes/(authed)/terminal/+page.svelte:664-666`:

```ts
function handleExitClick(direction: string) {
  sendCommand(direction);
}
```

`sendCommand` (line 623) does nothing but forward the raw string to
`client.sendCommand({ sessionId, text: command, connectionId })` (line 645) —
a straight pass-through to the gateway's command RPC, which lands in the same
`Dispatcher.Dispatch` registry lookup above. Since no command is registered
under the exit's direction/name, this **always** misses and returns "Unknown
command" — exactly the live-test symptom in the original finding
(`evidence/ui/06-after-exit-click.png`).

### Hunt 5 — scenes/join as an alternate mechanism

`core-scenes` commands are `scene`/`scenes` (join/leave/invite/etc scoped to
roleplay scenes, a structured encounter construct — see
`.claude/rules/terminology.md`), not general grid location traversal. Scenes
operate independently of `location_id` grid movement; joining a scene does not
call `world.Service.MoveCharacter`. No alternate movement path exists.

### Secondary observation (does not weaken the claim, refines its wording)

The original finding's phrase "integration-tested" (citing
`test/integration/world/movement_test.go`) is **imprecise**: that Ginkgo file
(`Describe("Character Movement", ...)`) exercises only `env.Exits.Create` /
`FindByName` / `FindBySimilarity` / `IsVisibleTo` — exit-repository CRUD and
visibility rules. It **never calls `svc.MoveCharacter`** (`rg -n
"MoveCharacter" test/integration/world/movement_test.go` → zero hits). The
actual `MoveCharacter` coverage is at the **unit** level only:
`internal/world/service_test.go` (`TestWorldService_MoveCharacter`,
`TestWorldService_MoveCharacter_VerifiesAccessRequest`, plus fail-closed cases)
and `internal/world/movement_hook_test.go`
(`TestMoveCharacter_FiresMovementHook`) — both plain `package world_test`
files with no `//go:build integration` tag, using mocked repos. This makes the
production-caller gap slightly worse than "tested at the service level,
untested at the command level" — it's "tested with mocks only, no
integration coverage of the real DB/event path either." Non-blocking for the
verdict; recommend tightening the finding's wording on this point.

**Conclusion:** every avenue checked (compiled-in handlers, all plugin
manifests, dispatcher registry/redirect logic, web exit-click, scenes-as-
alternate-mechanism) confirms `MoveCharacter` has zero production callers.
CLAIM 1 is CONFIRMED, not refuted.

---

## CLAIM 2 (D8a-HIGH-1): No service worker / manifest / PWA infra despite "offline-capable PWA" docs

**Verdict: CONFIRMED**

### Hunt 1 — SvelteKit service worker (`src/service-worker.{js,ts}` convention)

```text
$ find web/src -iname "*service-worker*"
(no results)
$ ls web/src/
app.css  app.html  lib  routes  test-setup.ts
```

No `service-worker.js`/`.ts` at the SvelteKit-conventional path, and no such
file anywhere else in `web/src`. `rg -in "serviceWorker|service-worker"
web/package.json web/svelte.config.js` → zero hits.

### Hunt 2 — Web app manifest

```text
$ find web -iname "manifest.webmanifest" -o -iname "manifest.json"
web/.svelte-kit/output/server/.vite/manifest.json   (Vite build artifact, not a web manifest)
web/.svelte-kit/output/client/.vite/manifest.json   (Vite build artifact, not a web manifest)
web/node_modules/.../playwright-core/.../manifest.webmanifest  (Playwright's own trace-viewer asset, unrelated)
```

None of these is an app web-manifest. `web/static/` — the conventional
location for a hand-authored `manifest.webmanifest` — **does not exist**
(`test -d web/static` → false). `web/src/app.html` was read in full:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    %sveltekit.head%
  </head>
  <body data-sveltekit-preload-data="hover">
    <div style="display: contents">%sveltekit.body%</div>
  </body>
</html>
```

No `<link rel="manifest">` tag.

### Hunt 3 — PWA dependency (`vite-plugin-pwa`, `@vite-pwa/sveltekit`, `workbox-*`)

`rg -in "pwa|workbox|service.?worker|manifest" web/package.json` → zero hits.
`rg -in "pwa|workbox" web/pnpm-lock.yaml` → zero hits (checked the lockfile,
not just the manifest, to rule out an installed-but-undeclared transitive).
`web/svelte.config.js` uses `@sveltejs/adapter-static` only — a plain static
SPA export adapter with a CSP config; no PWA plugin registered.

### Hunt 4 — the doc claims, read verbatim

`site/src/content/docs/contributing/explanation/architecture.md`:

- line 19 (mermaid diagram node): `WC[SvelteKit PWA]`
- line 298 (capability table row): `| **Web Client**    | SvelteKit PWA         | Modern, offline-capable               |`

`site/src/content/docs/operating/index.mdx`:

- line 25: `- **WebSocket** (port 8080) — Modern web client with PWA support`

Both citations from the original finding are verified verbatim at the stated
locations (line 298 exact match; operating/index.mdx line 25 exact match).

**Conclusion:** zero service-worker file, zero manifest file/tag, zero PWA
build dependency (declared or locked) anywhere in `web/`. The "offline-capable
PWA" / "PWA support" doc language is unsupported by the shipped web client.
CLAIM 2 is CONFIRMED, not refuted.
