# Stack Research

**Domain:** Web portal — character identity surfaces + role-gated admin shell on an existing SvelteKit 2 / Svelte 5 / ConnectRPC client
**Researched:** 2026-07-31
**Confidence:** HIGH (versions verified against the npm registry directly on 2026-07-31 and cross-checked against Context7 docs; every repo claim cites a file read during this research)

> **Scope note.** This is a *subsequent* milestone on a mature brownfield stack. Nothing below proposes
> replacing Go 1.26.5, PostgreSQL 18, NATS JetStream, SvelteKit 2.69/Svelte 5, ConnectRPC, shadcn-svelte,
> Tailwind v4, or `task`. The recommendations are strictly the **delta** needed for the four new
> capabilities: character creation/management UI, per-field-private public profiles, the `/admin` shell
> with reserved room for six future sections, and a media-ready profile schema.

---

## Executive summary — the four decisions

| # | Question | Decision |
|---|----------|----------|
| 1 | Form handling + validation | **Add `zod` 4.4.3 + `sveltekit-superforms` 2.30.2 (SPA mode) + `formsnap` 2.0.1** via the shadcn-svelte `form` registry component. Superforms' `SPA: true` mode submits nothing to SvelteKit — `onUpdate` is the submit handler and calls the ConnectRPC client directly. Verified against superforms.rocks. |
| 2 | Admin data-grid | **Add `@tanstack/table-core` 8.21.3** + `shadcn-svelte add table data-table`. Do **not** add `@tanstack/svelte-table`. |
| 3 | Component testing | **Add nothing.** The repo already has a working Svelte-5 component-test project (`web/vite.config.ts` `client` project, 17 existing `*.svelte.test.ts` files using `mount`/`unmount` from `svelte`). `@testing-library/svelte` is unnecessary; `vitest-browser-svelte` is a migration, not an addition, and is out of scope. |
| 4 | "No migration later" headroom | **The constraint is a *database* constraint, not a proto one.** Additive proto fields are always backward-compatible, so proto needs no reservation trick — just ship the media fields now. The DB is where a later `ALTER TABLE` would be required, so absorb media + per-field privacy growth in **JSONB columns on a new `character_profiles` table**, following the repo's own precedent (`000001_baseline.sql:65`, `000006_session_focus.sql:12`). |

---

## Repo grounding (verified during this research)

Every claim below was read from a file in this worktree, not recalled.

| Claim | Evidence |
|---|---|
| No form/validation library exists today | `web/package.json` deps are `@bufbuild/protobuf`, `@connectrpc/connect{,-web}`, `ansi_up`, `clsx`, `dompurify`, `marked`, `tailwind-merge`, `tailwind-variants`. No zod/superforms/formsnap. |
| Current form pattern is hand-rolled runes | `web/src/lib/components/scenes/CreateSceneForm.svelte:20-48` — `let title = $state('')`, manual `submitting`/`errorMsg`, `if (!t \|\| submitting) return` as the only validation. |
| Validation logic is already split into headless "flow" modules | `web/src/lib/scenes/createFlow.ts` (`submitCreateScene`), imported at `CreateSceneForm.svelte:10`; unit-tested in the node project (`web/src/lib/scenes/createFlow.test.ts`). |
| 17 shadcn components installed; **no `table`, no `form`, no `select`, no `switch`, no `avatar`** | `ls web/src/lib/components/ui/` → badge, button, card, checkbox, command, dialog, dropdown-menu, input, input-group, label, popover, resizable, scroll-area, separator, sheet, textarea, tooltip |
| shadcn-svelte style is `nova`, baseColor `slate`, aliases `$lib/components/ui` | `web/components.json` |
| Component testing already works, without any testing-library | `web/vite.config.ts` defines a `client` vitest project with `resolve: { conditions: ['browser'] }` and `include: ['src/**/*.svelte.test.ts']`; `web/src/lib/components/scenes/CreateSceneForm.svelte.test.ts:5,16` uses `import { mount, unmount } from 'svelte'`. |
| A **section registry** pattern already exists for nav IA | `web/src/lib/nav/sections.ts:41-44` — `SECTIONS` is an `as const satisfies readonly WorkspaceSection[]` array with `id`/`label`/`href`/`match` and a `requiresPlayer` visibility gate; `SectionId` is derived so a section without an icon *fails to compile*. |
| Protovalidate is already used on the web BFF proto | `api/proto/holomush/web/v1/web.proto:8` imports `buf/validate/validate.proto`; lines 1446-1475 use `string.min_len`, `string.max_len`, `repeated.max_items = 32`. |
| **`WebCheckSessionResponse` carries no role field** | `api/proto/holomush/web/v1/web.proto:733-748` — `player_name`, `player_id`, `is_guest`, `characters`. `rg -n 'role\|Role' api/proto/holomush/web/v1/web.proto` returns only an unrelated scene-participant comment at :1097. |
| Roles are stored **per character**, not per player | `internal/access/role.go:6-12` (`RolePlayer`/`RoleBuilder`/`RoleAdmin`) + `internal/store/migrations/000001_baseline.sql:87-91` (`character_roles(character_id, role)`). |
| `characters` table has no profile/media/privacy columns | `internal/store/migrations/000001_baseline.sql:72-79` — `id, player_id, name, description, location_id, created_at`. |
| JSONB-for-evolving-shape is established repo precedent | `000001_baseline.sql:65` (`preferences JSONB NOT NULL DEFAULT '{}'`), `:322` (`metadata JSONB NOT NULL DEFAULT '{}'`), `000006_session_focus.sql:12` (`focus_memberships JSONB NOT NULL DEFAULT '[]'`), `000039_connection_focus_key.sql:11-12`. |
| Only `WebCreateCharacter` exists; no mutation RPCs | `web.proto:177-192` — `WebCreateCharacter`, `WebListCharacters`, `WebListAllCharacters` are the whole character surface. `api/proto/holomush/world/v1/*.proto` has read-only `GetCharacter`/`ListCharactersAtLocation`. |
| `pg`/`@types/pg` are E2E-fixture-only, not app code | Only importer is `web/e2e/helpers/db.ts:4`. Not a gateway-boundary violation. |
| Playwright E2E already has an `admin.spec.ts` (name is taken) | `web/e2e/admin.spec.ts` — it drives register→character→terminal flows, *not* a `/admin` portal. The new portal suite needs a distinct filename (e.g. `admin-portal.spec.ts`). |

---

## Recommended Stack

### Core Technologies (additions only)

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| `zod` | **4.4.3** | Single client-side validation contract for character + profile forms | The BFF already enforces `buf.validate` constraints (`web.proto:1446-1475`). A Zod schema is the *mirror* of those constraints, giving inline field errors before a round-trip while server-side protovalidate stays authoritative. Zod 4 is the version superforms declares first-class (`zod4`/`zod4Client` adapter exports, peer `zod: ^3.25.0 \|\| ^4.0.0`). |
| `sveltekit-superforms` | **2.30.2** | Form state, per-field error stores, `$constraints` a11y attributes, tainted-field tracking | **SPA mode (`SPA: true`) is the load-bearing feature.** Per superforms.rocks/concepts/spa, SPA mode intercepts submission so nothing is posted to a SvelteKit form action; `onUpdate({ form })` is the submit handler and is documented as the place to "call an external API with `form.data`". That is exactly the ConnectRPC seam. `defaults(zod4(schema))` initializes a form with **no `+page.ts` load at all** — critical because this app uses `@sveltejs/adapter-static`. Peer deps verified: `svelte >=5.0.0-next.51`, `@sveltejs/kit 1.x \|\| 2.x`. |
| `formsnap` | **2.0.1** | Accessible field/label/description/error wiring over superforms | This is what shadcn-svelte's `form` registry component is built on — adopting shadcn `form` pulls it in regardless. Peers verified: `svelte ^5.0.0`, `sveltekit-superforms ^2.19.0` (2.30.2 satisfies). Its only runtime dep is `svelte-toolbelt ^0.5.0`. Matters most for the profile sheet, where every field needs a label + description + error + an adjacent privacy control, all correctly `aria-describedby`-linked. |
| `@tanstack/table-core` | **8.21.3** | Headless sorting / filtering / pagination / column-visibility for admin character list-search | Required peer of the shadcn-svelte `data-table` registry component. Framework-agnostic core with **zero peer dependencies** (verified: `peerDependencies: null`), so it adds no Svelte-version risk. Client-side row models are the right call for the admin character list — the milestone's list-search is over a bounded roster, not a paginated firehose. |

### shadcn-svelte registry components to add (code, not dependencies)

These are **vendored source files**, not npm packages — they land in `$lib/components/ui/` and are yours to edit. All are `nova`-style and consume the existing `--color-*` tokens.

| Component | CLI | Needed for | Notes |
|---|---|---|---|
| `table` + `data-table` | `pnpm dlx shadcn-svelte@latest add table data-table` | Admin character list/search (cap. 3) | Ships its own `createSvelteTable` + `FlexRender` Svelte-5 wrapper over `@tanstack/table-core`. |
| `form` | `... add form` | All forms (cap. 1, 2) | Pulls `formsnap` + `sveltekit-superforms`. |
| `select` | `... add select` | "Create as" alt picker, privacy dropdowns | Replaces the raw `<select>` currently hand-styled at `CreateSceneForm.svelte:56-65`. |
| `switch` | `... add switch` | Per-field public/private toggle (cap. 2) | Semantically correct for a binary visibility control; `checkbox` already exists but reads as a form-value, not a mode toggle. |
| `avatar` | `... add avatar` | Profile header placeholder (cap. 4) | Renders initials today; swaps to `primary_image` later with **no layout rework** — this is the component-layer half of the "reserve room" requirement. |
| `alert-dialog` | `... add alert-dialog` | Retire/delete/disable confirmation (cap. 1, 3) | Destructive actions must not use plain `dialog`. |
| `pagination` | `... add pagination` | Admin list paging | Pairs with `getPaginationRowModel()`. |
| `tabs` | `... add tabs` | Profile sheet sections; admin section landing | |
| `breadcrumb` | `... add breadcrumb` | `/admin/**` depth navigation | Directly serves the six-future-section IA. |
| `skeleton` | `... add skeleton` | Loading states for RPC-backed lists | |
| `sonner` | `... add sonner` | Optimistic-update success/rollback toasts (cap. 1) | Adds `svelte-sonner`. Optional — see "Alternatives Considered". |

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| `task` | Mandatory build/test/lint entry point | Unchanged. Never run `pnpm`/`go`/`golangci-lint` directly. `task proto && task web:generate` after any proto change, and **commit** `pkg/proto/**/*.pb.go` + `web/src/lib/connect/**/*_pb.ts` in the same change or CI fails the stale-diff check. |
| `buf` + `protovalidate` | Proto lint/breaking + server-side field constraints | Already wired. New messages must satisfy `task lint:proto` — note `.claude/rules/proto-doc-comments.md`: **every** new message/field/RPC needs a Go-grounded leading comment with no name-echo. This is a real, non-trivial authoring cost for a ~15-field profile message; budget for it in the SPEC phase. |
| Vitest (existing, `client` + `server` projects) | Component + flow-module tests | No config change needed. |
| Playwright 1.61.1 (existing) | E2E for the admin gate and profile privacy | The *authorization* assertions (non-admin cannot reach `/admin`; a private field is absent from a stranger's profile response) belong here, not in component tests. |

---

## Installation

```bash
# All web work runs from web/ with pnpm@11.13.1 (per web/package.json "packageManager").
# Prefer `task` wrappers where they exist; these are the underlying installs.

cd web

# Runtime dependencies (3 packages)
pnpm add zod@4.4.3 sveltekit-superforms@2.30.2 @tanstack/table-core@8.21.3
# formsnap arrives transitively via the shadcn `form` component; pin it explicitly:
pnpm add formsnap@2.0.1

# Vendored component source (writes into src/lib/components/ui/, edits components.json-tracked files)
pnpm dlx shadcn-svelte@latest add table data-table form select switch avatar \
                                    alert-dialog pagination tabs breadcrumb skeleton

# Optional (toast for optimistic-update feedback — see Alternatives Considered)
pnpm dlx shadcn-svelte@latest add sonner

# NO new dev dependencies. Component testing is already configured.
```

---

## How each addition serves each capability

### Capability 1 — Character creation + management UI

- **What's missing in the repo:** only `WebCreateCharacter` exists (`web.proto:177`). Rename / set-description / retire RPCs do **not** exist at any layer (`world/v1` has read-only character RPCs). Per the locked decision in `PROJECT.md` §Key Decisions #5 and `.claude/rules/gateway-boundary.md`, these are GUI-driven **structural writes** → they MUST be new typed RPCs on the BFF facade, never `sendCommand`.
- **superforms role:** each mutation is a small form. `superForm(defaults(zod4(renameSchema)), { SPA: true, validators: zod4Client(renameSchema), onUpdate })` gives client validation, `$errors.characterName` for inline display, and `$constraints` (auto `maxlength`/`required`) mirrored from the Zod schema — which itself mirrors the `buf.validate` constraints on the new request message.
- **Optimistic updates:** superforms handles the *form*; the *list* is a store concern. Follow the existing pattern (`$lib/scenes/workspaceStore.ts`, `$lib/scenes/membershipFlow.ts`) — mutate the local `$state` array, fire the ConnectRPC call in `onUpdate`, roll back in the `catch`. Do **not** introduce a query cache for this (see "What NOT to Use").
- **Testability:** `web/CLAUDE.md` §Form Requirements already mandates `name` attributes on all inputs and `type="submit"` on submit buttons — superforms/formsnap emit both by default, so this gets *easier*, not harder.

### Capability 2 — Public profiles with per-field privacy

- **superforms + formsnap is the highest-value addition here.** A profile sheet is ~10-20 fields where each field carries a *paired* visibility control. Hand-rolled runes (the `CreateSceneForm.svelte` pattern) scale to 3 fields; at 20 fields × 2 controls it becomes ~300 lines of duplicated `$state`/`errorMsg`/`aria-describedby` wiring. formsnap's `Field`/`Control`/`Description`/`FieldErrors` composition collapses that to one snippet reused per field.
- **Model the privacy map as part of the Zod schema**, e.g. `z.object({ fields: z.record(profileFieldKey, z.enum(['public','private'])) })`. Superforms handles nested objects, so the visibility map round-trips as one form.
- **Server-side is the real gate.** Per-field privacy MUST be enforced by projection/redaction in the read RPC under the default-deny ABAC engine — a private field must be **absent from the response**, not hidden by CSS. This is a spec/architecture point, not a library one, but it determines the proto shape (see below). No client library can satisfy it.
- **`avatar` component** renders initials now and an image later — the component-layer half of the media-headroom requirement.

### Capability 3 — Admin shell with reserved room for six sections

- **No new library is needed for the IA.** `web/src/lib/nav/sections.ts:41-47` is already the exact pattern the requirement describes: an ordered `as const satisfies` registry with a derived `SectionId` union, a visibility predicate (`requiresPlayer`), and a single `visibleSections(viewer)` gate that both the rail and the command palette flow through (per ADR `holomush-stds8`). **Mirror it as `web/src/lib/admin/sections.ts`** with a `requiresRole: 'admin'` gate and a `status: 'available' | 'planned'` discriminant.
  - This satisfies "declared, currently-empty room… without rework" *structurally*: the six deferred sections are registered entries with `status: 'planned'`, rendered as disabled nav items. Filling one later is a one-line status flip plus a route file — no nav refactor, no IA change. Because `SectionId` is derived from the registry, a section without an icon **fails to compile** rather than crashing at runtime.
- **`@tanstack/table-core` + `data-table`** is the one genuine addition — the character admin list needs sort/filter/paginate/column-visibility, and hand-rolling that is exactly the kind of accidental complexity a headless table exists to prevent. It also front-runs five of the six deferred sections (player management, moderation/bans, audit-log viewer, plugin management are all list-search surfaces) — so the investment amortizes.
- **The role gate needs a proto change, and there is no way around it.** `WebCheckSessionResponse` (`web.proto:733-748`) has no role field, and roles live on `character_roles` **per character** (`000001_baseline.sql:87-91`), not per player. The SPEC phase must decide whether `/admin` is gated on *"the acting character has RoleAdmin"* or *"any character owned by this player has RoleAdmin"* — these give materially different UX with an alt switcher. Either way the client needs a signal it cannot currently obtain. Client-side gating is UX only; the authoritative gate is default-deny ABAC on every admin RPC.
- **Naming collision:** `web/e2e/admin.spec.ts` already exists and is unrelated (register→character→terminal flows). Use a new filename for the portal suite.

### Capability 4 — Media-ready profile schema

**The proto half is free. The database half is the whole problem.** Treat these separately.

**Proto layer — no reservation trick required.**
Adding a field with a fresh number is always wire- and API-backward-compatible; `reserved` exists to stop *reuse of deleted* numbers, not to book space for future ones. So there is no "reserve capacity" pattern to apply, and a plan that spends effort on one is solving a non-problem. What *is* worth doing:

```protobuf
// ProfileImage is one stored image reference on a character profile. Media
// storage is deferred (999.16); this message ships shaped-but-unpopulated so
// the wire contract does not change when uploads land.
message ProfileImage {
  // media_id is the ULID of the stored blob once an upload path exists.
  string media_id = 1;
  // alt_text is the accessible description shown when the image cannot render.
  string alt_text = 2;
  // content_warning flags the image for the viewer-side CW gate.
  string content_warning = 3;
}

message CharacterProfile {
  // ... typed profile fields 1..N ...

  // primary_image is the profile's single hero image. Unset in v0.13 — no
  // upload path is built in this milestone.
  ProfileImage primary_image = 40;

  // gallery holds up to 10 supplementary images. Empty in v0.13.
  repeated ProfileImage gallery = 41 [(buf.validate.field).repeated.max_items = 10];
}
```

- Ship `ProfileImage` and both fields **now, empty**. Cost: near-zero. Benefit: `buf breaking` never sees a change when uploads land, TS types (`web/src/lib/connect/holomush/**`) are already correct, and the `max_items = 10` cap is enforced by protovalidate on day one rather than being retrofitted.
- Use a **numbered block** (e.g. 40-49 media, 50-59 admin/moderation metadata) purely as a legibility convention. It is documentation, not a compatibility mechanism — do not oversell it in the SPEC.
- **Do not** add a `reserved 100 to 199;` block for the deferred admin sections. `reserved` on numbers that were never used blocks *you* from using them and buys nothing.

**Database layer — this is where "no migration later" actually costs something.**
`characters` (`000001_baseline.sql:72-79`) has none of these columns, and adding one later **is** an `ALTER TABLE`, i.e. exactly the migration the requirement forbids. Recommendation:

```sql
-- Ship ONE migration now; none later.
CREATE TABLE IF NOT EXISTS character_profiles (
    character_id     TEXT PRIMARY KEY REFERENCES characters(id) ON DELETE CASCADE,
    -- Typed columns for fields the milestone actually renders.
    -- ...
    -- Growth absorbers: shape evolves inside JSONB with zero DDL.
    field_visibility JSONB NOT NULL DEFAULT '{}',
    media            JSONB NOT NULL DEFAULT '{"primary": null, "gallery": []}',
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

- `media JSONB` means the upload epic (999.16) adds keys inside the document — **no `ALTER TABLE`, constraint satisfied literally.**
- `field_visibility JSONB` means a *new profile field* added in a later milestone gets its privacy flag with no DDL either.
- This is not a novel pattern here: `preferences JSONB NOT NULL DEFAULT '{}'` (`:65`), `metadata JSONB NOT NULL DEFAULT '{}'` (`:322`), `focus_memberships JSONB NOT NULL DEFAULT '[]'` (`000006_session_focus.sql:12`), and the `connection_focus_key` JSONB encoding (`000039:11-12`) are all the same call, and `000039` explicitly cites `focus_memberships` as its precedent. Per `.claude/rules/database-migrations.md`, the migration must be idempotent (`IF NOT EXISTS`), a single `NNNNNN_name.sql` file carrying `-- +goose Up` and a `-- +goose Down` section, and carry no triggers/functions.
- **Trade-off, stated honestly:** JSONB gives up column-level type checking and cheap indexing. That is the correct trade for `media` (write-rarely, read-whole, never filtered) and for `field_visibility` (a small map read with its row). It would be the *wrong* trade for anything the admin list-search filters or sorts on — those stay typed columns.

---

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| `sveltekit-superforms` 2.30.2 | **Plain runes + `zod`, extending the existing `createFlow.ts` "flow module" pattern** | Genuinely viable and ~40 kB lighter. Choose it if the SPEC phase concludes the profile sheet is **under ~8 fields** and per-field privacy is a single global toggle rather than per-field. The repo's flow pattern (`$lib/scenes/createFlow.ts` + thin component) is clean and already test-covered. **This is the one recommendation the SPEC phase should be allowed to overturn** — the deciding variable is final profile field count. Superforms wins at scale; below ~8 fields it is ceremony. |
| `zod` 4.4.3 | `valibot` 1.4.2 | Smaller bundle, also has a superforms adapter (`valibot`/`valibotClient`). Choose only if web bundle size becomes a measured PWA constraint. Zod's ecosystem gravity and error-message ergonomics win by default. |
| `@tanstack/table-core` (client-side row models) | Server-side pagination via new list RPCs with `page_token` | Switch when any admin list exceeds a few thousand rows. Not this milestone's character roster. Design the list RPC with an optional `page_size`/`page_token` so the switch is additive, not breaking. |
| Mirror `nav/sections.ts` for admin IA | shadcn-svelte `sidebar` block / `dashboard-01` | A full sidebar block would install a large surface fighting the existing `nova` style, the `--color-*` runtime theme system (`web/CLAUDE.md` §Theme System), and the existing `SectionRail`/`ShellFooter` shell. Cherry-pick primitives instead. |
| `sonner` toast for optimistic feedback | Inline `$message` from superforms | If the SPEC prefers in-form success/error text over toasts, skip `sonner` entirely — superforms' `$message` store covers it and adds no dependency. |
| Ship media fields empty in proto now | Add them in the upload milestone | The proto addition is genuinely free at any time. The *only* reason to do it now is the milestone's explicit "shaped for media without later rework" REQ. Doing it now also lets the `avatar` component and profile layout be built against the real type. |

---

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| **`@tanstack/svelte-table`** (8.21.3 exists) | The Svelte *adapter* package is a Svelte-4-era store wrapper. shadcn-svelte's `data-table` deliberately does **not** use it — it ships its own `createSvelteTable` built on runes-compatible getters over `@tanstack/table-core` (confirmed in the shadcn-svelte data-table docs). Installing both yields two competing table abstractions. | `@tanstack/table-core` 8.21.3 + `shadcn-svelte add data-table` |
| **`@testing-library/svelte`** 5.4.2 | Solves a problem this repo already solved. `web/vite.config.ts` has a working `client` vitest project and 17 `*.svelte.test.ts` files use `mount`/`unmount` directly (`CreateSceneForm.svelte.test.ts:5,16`). Adding it creates two component-test idioms with no coverage gain. | The existing `mount` + `document.body` pattern. Copy `CreateSceneForm.svelte.test.ts` as the template for profile/admin component tests. |
| **`vitest-browser-svelte`** 3.0.0 | Real improvement (real browsers, retry-ability, better rune support) but it is a **migration**: requires Vitest Browser Mode (`@vitest/browser-playwright`), removing `jsdom` + the `localStorage` polyfill at `web/src/test-setup.ts`, and rewriting all 17 existing tests from `querySelector` to locators. That is its own project, and doing it inside a feature milestone risks the milestone. | Keep jsdom + `mount` now. File a separate issue for the migration; revisit when the whole suite can move at once. |
| **`@tanstack/svelte-query` / any query-cache layer** | Tempting for "optimistic updates over ConnectRPC", but `@connectrpc/connect-query` is React-only, and the repo has an established store idiom (`$lib/scenes/workspaceStore.ts`, `publishStore.ts`, `$lib/presence/store.ts`, `$lib/stores/*`). Adding a cache creates a **second source of truth** competing with those stores and with the live `StreamEvents` push feed — a class of bug that is expensive to find. | Extend the existing store pattern; do optimistic mutation + rollback in the store, exactly as `membershipFlow.ts`/`lifecycleFlow.ts` do today. |
| **`felte`, `svelte-forms-lib`, `sveltekit-flash-message`-style form helpers** | Svelte-4-era store APIs; no first-class runes support; small/stalled maintenance. | superforms 2.30.2 (or plain runes) |
| **`yup` / `joi` / `superstruct` / `class-validator`** | Superforms has adapters for all of them, but none mirrors the `buf.validate` constraint vocabulary as cleanly as Zod, and each drags a distinct type-inference story. | `zod` 4.4.3 |
| **`zod` v3** | Superseded; superforms ships a dedicated `zod4` adapter and declares `zod ^3.25.0 \|\| ^4.0.0`. Starting on v3 buys a migration. | `zod` 4.4.3 with the `zod4`/`zod4Client` adapters |
| **`drizzle-orm`, `prisma`, `postgres.js`, or any DB client in `web/src/`** | Direct violation of `.claude/rules/gateway-boundary.md` and `PROJECT.md` §Constraints — `internal/web/` and the SvelteKit client are protocol translation only. Note `pg` + `@types/pg` are *already* devDeps but are used **only** by `web/e2e/helpers/db.ts:4` (Playwright fixture). Do not let that precedent leak into `src/`. | New typed RPCs on the BFF facade; `internal/web` proxies to core. |
| **`sendCommand` / `HandleCommand` for any admin or profile mutation** | Locked decision (`PROJECT.md` §Key Decisions #5, ADR `holomush-v4qmu`, `.claude/rules/gateway-boundary.md`). GUI buttons/forms are machine-initiated structural writes and MUST NOT route through the human text-command parser. | Typed RPCs on `WebService`. If the facade lacks one, **add the RPC** — do not string-build a command. |
| **Upload libraries (`uppy`, `filepond`, `svelte-file-dropzone`), image processing, S3/blob SDKs** | No upload path is in scope (`PROJECT.md`: "Schema shaped for 1 primary image + up to 10 gallery images; **no upload path built in this milestone**"; avatar/blob storage deferred to 999.16). Installing them invites scope creep into a deferred epic. | Ship the schema/type headroom only. `avatar` renders initials. |
| **`reserved` blocks on never-used proto field numbers**, "capacity reservation" messages, or placeholder `oneof` slots | Additive proto fields are already backward-compatible — this solves nothing and permanently blocks the numbers from the author's own use. `reserved` is for numbers of **deleted** fields. | Ship the real fields now, empty. Use numbered *ranges* as a documentation convention only. |
| **A separate admin SPA, different framework, or second design system** | Fragments auth/session handling, the `--color-*` theme system, and the ConnectRPC transport. The `/admin` route belongs inside the existing `(authed)` shell. | A route group under the existing shell with an `admin/sections.ts` registry. |
| **Client-side-only role gating as the security boundary** | `RoleAdmin` in the browser is a UX affordance. Every admin RPC must be independently default-deny ABAC-gated server-side. | ABAC evaluation per admin RPC; client role signal only hides dead-end nav (the same posture `sections.ts:19-23` documents for `requiresPlayer`). |

---

## Stack Patterns by Variant

**If the profile sheet lands under ~8 fields with a single global public/private toggle:**
- Skip `sveltekit-superforms` and `formsnap`; keep `zod` for the shared validation contract.
- Extend the existing flow-module pattern (`$lib/characters/profileFlow.ts` + thin component), unit-tested in the `server` vitest project.
- Because superforms is SPA-mode-only here, adopting it later is a per-form refactor — not a global one. This decision is reversible at low cost.

**If the profile sheet lands at 10+ fields with per-field privacy (the stated requirement):**
- Adopt superforms + formsnap + shadcn `form` as recommended. The nested-object handling and the per-field `Field`/`Control`/`FieldErrors` composition are the entire justification.

**If any admin list is expected to exceed a few thousand rows:**
- Keep `@tanstack/table-core` but design the list RPC with `page_size` / `page_token` from day one and use manual pagination (`manualPagination: true`) instead of `getPaginationRowModel()`. Adding pagination params to a request message later is additive and non-breaking, so this is safe to defer — but the *RPC shape* is cheaper to get right at SPEC time.

**If the SPEC gates `/admin` on the acting character rather than the player:**
- The role signal belongs on the character-selection response path, and the admin shell must react to alt switches (`web/e2e/character-switcher.spec.ts` is the existing behavioral reference). If it gates on the player, a single additive field on `WebCheckSessionResponse` suffices. **Decide this before writing the proto** — it changes the message shape.

---

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| `sveltekit-superforms@2.30.2` | `svelte@5.56.8` ✓ | Peer: `svelte 3.x \|\| 4.x \|\| >=5.0.0-next.51` (verified on npm) |
| `sveltekit-superforms@2.30.2` | `@sveltejs/kit@2.69.3` ✓ | Peer: `@sveltejs/kit 1.x \|\| 2.x` |
| `sveltekit-superforms@2.30.2` | `zod@4.4.3` ✓ | Peer: `zod ^3.25.0 \|\| ^4.0.0`; use the `zod4` / `zod4Client` adapter exports (present in `dist/adapters/index.d.ts`), **not** `zod`/`zodClient` |
| `sveltekit-superforms@2.30.2` | `@sveltejs/adapter-static@3.0.10` ✓ | SPA mode requires **no** server form action and **no** `+page.ts` load — use `defaults(zod4(schema))` for initialization |
| `formsnap@2.0.1` | `svelte@5.56.8`, `sveltekit-superforms@2.30.2` ✓ | Peers: `svelte ^5.0.0`, `sveltekit-superforms ^2.19.0`. Adds one transitive dep: `svelte-toolbelt ^0.5.0` |
| `formsnap@2.0.1` | `bits-ui@2.18.1` ✓ | Not a declared peer; both target Svelte 5 + `svelte-toolbelt`. shadcn-svelte `form` composes them. |
| `@tanstack/table-core@8.21.3` | anything | `peerDependencies: null`, `engines.node >=12`. Framework-agnostic — zero Svelte-version risk. |
| `shadcn-svelte@1.4.2` CLI | `web/components.json` (`style: nova`, `baseColor: slate`) ✓ | CLI writes into `$lib/components/ui` per the existing `aliases` block. Run `task fmt` afterward — generated files need SPDX headers per `.licenserc.yaml`, and uncommitted `fmt` output is a known cause of red CI. |
| `zod@4.4.3` | `typescript@6.0.3` ✓ | Zod 4 requires TS 5.5+ |
| `vitest@4.1.10` + `jsdom@29.1.1` (existing) | unchanged | No test-tooling change. The `localStorage` polyfill at `web/src/test-setup.ts` stays. |
| `@bufbuild/protobuf@^2.12.0` (repo) / 2.13.0 (latest) | `@bufbuild/protoc-gen-es@2.12.1` | Generator and runtime should stay in lockstep. Bumping the runtime without the generator is a separate, unrelated change — **not** part of this milestone. |

---

## Open questions for the SPEC phase

These are genuine gaps this research could not close from the repo alone. Each changes a proto or DB shape, so each is cheaper to decide before Phase 1 writes code.

1. **Is `/admin` gated per-character or per-player?** Roles are stored per character (`character_roles`), the session response carries neither. This determines whether the new role field goes on `WebCheckSessionResponse` or on the character-selection path.
2. **Final profile field count.** This is the single input that decides superforms vs. plain runes (see "Stack Patterns by Variant").
3. **Redaction shape for private fields.** Absent-from-response (recommended, fail-safe) vs. a `visibility` enum returned alongside. The former requires `optional` proto fields or a presence witness; note `.claude/rules/abac-providers.md`'s omit-don't-sentinel invariant is the directly analogous prior decision on the server side.
4. **Does the admin character list need server-side search**, or is client-side filtering over the full roster acceptable at current scale? Determines `manualFiltering` vs. `getFilteredRowModel()`.

---

## Sources

- **npm registry** (`registry.npmjs.org`, queried 2026-07-31) — authoritative `latest` versions and `peerDependencies` for `sveltekit-superforms` (2.30.2), `formsnap` (2.0.1), `zod` (4.4.3), `valibot` (1.4.2), `@tanstack/table-core` (8.21.3), `@tanstack/svelte-table` (8.21.3), `@testing-library/svelte` (5.4.2), `vitest-browser-svelte` (3.0.0), `@vitest/browser` (4.1.10), `shadcn-svelte` (1.4.2), `bits-ui` (2.18.1), `@bufbuild/protobuf` (2.13.0). **HIGH**
- **`unpkg.com/sveltekit-superforms@2.30.2/dist/adapters/index.d.ts`** — verified `zod4` / `zod4Client` adapter exports exist. **HIGH**
- **Context7 `/websites/superforms_rocks`** — SPA mode without form actions, `onUpdate` as external-API submit handler, `defaults(zod(schema))` without `+page.ts`, v2 adapter model. **HIGH** (cross-verified against npm peer deps)
- **Context7 `/websites/shadcn-svelte`** — `add table data-table` + `pnpm i @tanstack/table-core`, `createSvelteTable`/`FlexRender` Svelte-5 wrapper, sorting/filtering/pagination/column-visibility wiring. **HIGH**
- **Exa web search** — `svelte.dev/docs/svelte/testing` (mount-based baseline, testing-library as optional layer); `vitest.dev/api/browser/svelte` + `github.com/vitest-community/vitest-browser-svelte` (v3.0.0, released 2026-07-09, requires vitest ≥4 + Browser Mode); migration-cost accounts from scottspence.com and sveltest.dev. **MEDIUM**
- **Repository files read during this research** — `.planning/PROJECT.md`, `.planning/codebase/STACK.md`, `web/CLAUDE.md`, `web/package.json`, `web/components.json`, `web/vite.config.ts`, `web/src/test-setup.ts`, `web/src/lib/nav/sections.ts`, `web/src/lib/components/scenes/CreateSceneForm.svelte`, `web/src/lib/components/scenes/CreateSceneForm.svelte.test.ts`, `api/proto/holomush/web/v1/web.proto`, `api/proto/holomush/world/v1/*.proto`, `internal/access/role.go`, `internal/store/migrations/000001_baseline.up.sql`, `internal/store/migrations/000006_session_focus.up.sql`, `internal/store/migrations/000039_connection_focus_key.up.sql`, `web/e2e/` listing. **HIGH**

---
*Stack research for: web portal identity + admin foundations (HoloMUSH v0.13)*
*Researched: 2026-07-31*
