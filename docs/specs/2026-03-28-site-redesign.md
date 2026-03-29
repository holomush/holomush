# Documentation Site Redesign

**Date:** 2026-03-28
**Status:** Approved
**Scope:** Complete restructure of holomush.dev documentation site

## Overview

Restructure holomush.dev from three audience sections (contributors, developers,
operators) to five (guide, operating, extending, contributing, reference). Add a
landing page with value proposition and feature cards. Create a new guide section
for players and game designers. Auto-generate the gRPC API reference from proto
files. Fix all dead links, stale technical content, and terminology violations.

## Problem Statement

The current documentation site has structural and content problems that undermine
its usefulness:

1. **Missing audience.** No content for players or game designers. The site
   assumes every visitor is a coder.
2. **Confusing taxonomy.** "Contributors" (core devs) vs "Developers" (plugin
   builders) is ambiguous. The naming is backwards for most visitors.
3. **23 dead links.** Index pages reference pages that do not exist. Visitors
   clicking these links get 404 errors.
4. **Stale technical content.** The gRPC API reference, EventStore interface, and
   command listings do not reflect the current codebase.
5. **No value proposition.** The landing page has a four-card feature grid with
   one-liners. There is nothing that explains what HoloMUSH is, why it exists, or
   why someone should choose it.
6. **Inconsistent diagrams.** Some pages use Mermaid, others use ASCII box art.
7. **Terminology violations.** Multiple pages use "room" instead of "location",
   violating the project terminology standard.
8. **Duplicate content.** Authentication is documented in both contributors/ and
   operators/ with heavy overlap.

## Goals

- Every visitor to holomush.dev MUST understand what HoloMUSH is within 10
  seconds of landing.
- Every audience (players, operators, plugin developers, core contributors) MUST
  have a clear path from the landing page.
- Every internal link MUST resolve to a real page.
- Technical reference content SHOULD be auto-generated where possible to prevent
  staleness.
- All diagrams MUST use Mermaid for consistency.
- All content MUST use the project terminology standard ("location", not "room").

## Non-Goals

- Custom site theme or CSS beyond what zensical provides.
- Comparison pages disparaging other MUSH platforms.
- Auto-generated documentation for Go package internals (godoc serves that role).

## Audience Priority

| Priority | Audience                  | Section         | Description                                    |
| -------- | ------------------------- | --------------- | ---------------------------------------------- |
| 1        | People evaluating          | Landing page    | "What is this? Why should I care?"             |
| 2        | Players and game designers | `guide/`        | "I want to play or build a world."             |
| 3        | Server operators           | `operating/`    | "I want to run a game."                        |
| 4        | Plugin developers          | `extending/`    | "I want to extend it."                         |
| 5        | Core contributors          | `contributing/` | "I want to hack on the codebase."              |

## Voice and Tone

The site voice MUST be warm, competent, and approachable. Think an experienced
game runner explaining things to a friend over coffee.

- NOT corporate-formal. No "leverage synergies" or "enterprise-grade solutions."
- NOT trying to sound young. No forced slang, no memes, no emoji-heavy copy.
- Direct and confident. State what the thing does, not what it "aims to" or
  "strives to" do.
- Respect the reader's time. Say it once, say it clearly.
- Technical content can be precise without being dry. Examples and context help.

Multiple revision passes SHOULD be made on new content to calibrate voice. When
in doubt, read the sentence aloud. If it sounds like a press release, rewrite it.

**Voice calibration examples:**

| Too formal                                                       | Too casual                              | About right                                                                |
| ---------------------------------------------------------------- | --------------------------------------- | -------------------------------------------------------------------------- |
| "HoloMUSH leverages event sourcing to provide robust auditability." | "Events are super cool and you'll love em!" | "Every game action is an immutable event. Replay history, audit what happened, debug issues." |
| "The session persistence subsystem facilitates seamless reconnection." | "Drop your wifi? No worries lol"       | "Sessions survive disconnects and network hiccups. Pick up where you left off." |
| "Operators are advised to configure monitoring endpoints."       | "You def wanna set up monitoring btw"   | "Both processes expose Prometheus metrics. Point your scraper at the metrics endpoints." |

## Site Structure

### New Navigation

```text
site/docs/
+-- index.md                        # Landing page (hero + features + paths)
|
+-- guide/                          # For players and game builders
|   +-- index.md                    # "Using HoloMUSH" overview
|   +-- connecting.md               # How to connect (telnet, web, future apps)
|   +-- commands.md                 # Command reference for players
|   +-- building.md                 # World-building for game designers
|
+-- operating/                      # For server operators
|   +-- index.md                    # Operations overview
|   +-- installation.md             # Install (Docker, binary, source)
|   +-- configuration.md            # All config options
|   +-- database.md                 # PostgreSQL setup + maintenance
|   +-- authentication.md           # Auth config (operator-focused only)
|   +-- operations.md               # Monitoring, metrics, troubleshooting
|   +-- verifying-releases.md       # Cosign/SBOM verification
|
+-- extending/                      # For plugin developers
|   +-- index.md                    # Plugin system overview
|   +-- getting-started.md          # Set up a running server, create first plugin
|   +-- plugin-guide.md             # Lua + Go plugin development
|   +-- api-guide.md                # Working with the gRPC API (narrative)
|   +-- events.md                   # Event types, streams, patterns
|
+-- contributing/                   # For core codebase contributors
|   +-- index.md                    # Contributing overview
|   +-- architecture.md             # System architecture
|   +-- coding-standards.md         # Go conventions
|   +-- authentication.md           # Auth internals (contributor-focused)
|   +-- event-delivery.md           # Event delivery deep-dive
|   +-- pr-guide.md                 # PR workflow
|
+-- reference/                      # Auto-generated + curated reference
    +-- index.md                    # Reference overview
    +-- grpc-api.md                 # Auto-generated proto reference
    +-- policy-dsl.ebnf             # Generated ABAC policy DSL grammar
    +-- policy-dsl-railroad.html    # Generated railroad diagram for policy DSL
    +-- events.md                   # Complete event type catalog
```

### Rationale for Section Names

| Old name       | New name        | Why                                                    |
| -------------- | --------------- | ------------------------------------------------------ |
| `contributors` | `contributing`  | Standard OSS convention (CONTRIBUTING.md)              |
| `developers`   | `extending`     | Avoids confusion with "contributor" / "developer"      |
| `operators`    | `operating`     | Consistent verb form across section names              |
| *(new)*        | `guide`         | Serves the missing player/designer audience            |
| *(new)*        | `reference`     | Separates auto-generated content from hand-written     |

## Landing Page Design

The landing page has five sections in order:

### 1. Hero

```markdown
# HoloMUSH

**Modern infrastructure for text-based virtual worlds.**

Build immersive games with a high-performance server, flexible plugin system,
and connectivity that works the way people actually use the internet today.

[Get Started ->] [View on GitHub ->]
```

"Get Started" links to `guide/index.md`. "View on GitHub" links to the repo.

### 2. Features (six cards)

| Card                  | Copy                                                                                                                                                                |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Play from anywhere    | Web browser, telnet client, phone -- your choice. Sessions survive disconnects and network hiccups. Pick up where you left off.                                     |
| Secure by design      | Argon2id passwords, mutual TLS between processes, attribute-based access control. Security decisions are baked in, not bolted on.                                    |
| Extend with Lua or Go | Write a quick Lua script to add a dice roller. Build a full Go plugin for a combat system. Both run sandboxed with policy-controlled access to the game world.      |
| Deploy on your terms  | Single binary plus PostgreSQL. Run it on a Raspberry Pi, a VPS, or a Kubernetes cluster. Docker Compose gets you running in minutes.                                |
| Events all the way down | Every game action is an immutable event. Replay history, audit what happened, debug issues. Per-stream ordering gives consistency where it matters.                |
| Open source, Apache-2.0 | Built in the open. Read the code, contribute improvements, fork it for your own purposes. No lock-in, no surprises.                                              |

### 3. Coming Soon

- Native iOS and desktop apps (Tauri)
- Discord and Slack integration -- bridge your game community where they already
  hang out
- Plugin packages for popular RP systems -- pick a genre, get a complete game
  framework

This section SHOULD be reviewed when the landing page is updated for any reason.
Items SHOULD be removed once shipped or replaced if plans change. The project
maintainer owns the accuracy of this list.

### 4. Choose Your Path (four cards)

| Card                | Description                                                      | Link               |
| ------------------- | ---------------------------------------------------------------- | ------------------ |
| Playing             | Connect to a game, learn the commands, start telling stories.    | `guide/index.md`   |
| Running a Server    | Install, configure, and operate your own HoloMUSH game.          | `operating/index.md` |
| Building Plugins    | Extend the game with Lua scripts or Go plugins.                  | `extending/index.md` |
| Contributing        | Help build HoloMUSH -- code, docs, testing, ideas.              | `contributing/index.md` |

### 5. Community Footer

Link to GitHub repository. Copyright notice. Apache-2.0 license mention.

## New Pages

### guide/index.md

Overview of what a MUSH is, what HoloMUSH offers players and game designers.
Links to connecting, commands, and building. Warm and inviting tone -- this is
where non-technical visitors land first.

### guide/connecting.md

How to connect via web browser or telnet client. What to expect on first
connection. Account creation and character creation flow. Screenshots or
terminal output examples where helpful.

### guide/commands.md

Player command reference organized by intent:

- Communication: say, pose, whisper, page
- Navigation: look, move, exits, direction shortcuts
- Information: describe, who, help
- Session: connect, play, create, quit

Each command with usage example and brief explanation.

### guide/building.md

For game designers who are not coders. Creating locations, exits, and
descriptions. The scene system. How the world model works conceptually without
Go internals. This page bridges between "I play here" and "I want to run a
game."

**Scope boundary with extending/:** `guide/building.md` covers world-building
through in-game commands (creating locations, writing descriptions, linking
exits). `extending/getting-started.md` covers programmatic extension through
plugins (Lua scripts, Go binaries, event handlers). A game designer reads
`guide/building.md`; a programmer reads `extending/`.

### extending/events.md

Extracted from the current plugin-guide.md event types section. Stream patterns,
event lifecycle, subscription model from a plugin developer's perspective.
Communication events, world events, and stream types.

### reference/index.md

Brief overview of reference material. Explains which sections are auto-generated
(gRPC API, event catalog) vs hand-curated. Links to each reference page.

### reference/grpc-api.md

Auto-generated from protobuf definitions using `protoc-gen-doc`. Covers all
services: Core (core.proto), Web (web.proto), Control (control.proto), Plugin
(plugin.proto, hostfunc.proto). Field tables, type definitions, enums.

### reference/events.md

Complete event type catalog with payload schemas. This page is hand-written
(not auto-generated). Content is sourced from the existing event type tables in
`plugin-guide.md` and expanded with full payload field descriptions for each
event type. The page SHOULD be verified against the proto definitions during
writing but does not require a generation pipeline.

## Content Migration

### Pages That Move (with modifications)

| Current path                         | New path                           | Changes                                    |
| ------------------------------------ | ---------------------------------- | ------------------------------------------ |
| `index.md`                           | `index.md`                         | Complete rewrite (new landing page)        |
| `contributors/index.md`              | `contributing/index.md`            | Rewrite, remove dead links                 |
| `contributors/architecture.md`       | `contributing/architecture.md`     | Terminology fix ("room" -> "location")     |
| `contributors/coding-standards.md`   | `contributing/coding-standards.md` | Update stale EventStore interface          |
| `contributors/authentication.md`     | `contributing/authentication.md`   | ASCII -> Mermaid, scope to internals       |
| `contributors/event-delivery.md`     | `contributing/event-delivery.md`   | Terminology pass, otherwise good           |
| `contributors/pr-guide.md`           | `contributing/pr-guide.md`         | Good as-is                                 |
| `developers/index.md`                | `extending/index.md`               | Rewrite, scope to plugins only             |
| `developers/getting-started.md`      | `extending/getting-started.md`     | Rewrite for plugin devs (not core devs)    |
| `developers/plugin-guide.md`         | `extending/plugin-guide.md`        | Terminology fix, extract event types       |
| `developers/grpc-api.md`             | `extending/api-guide.md`           | Rewrite as narrative guide (see below)     |
| `developers/policy-dsl.ebnf`          | `reference/policy-dsl.ebnf`          | Move (update Taskfile output path)         |
| `developers/policy-dsl-railroad.html` | `reference/policy-dsl-railroad.html` | Move (update Taskfile output path)         |
| `operators/index.md`                 | `operating/index.md`               | Rewrite, remove dead links                 |
| `operators/installation.md`          | `operating/installation.md`        | Good as-is                                 |
| `operators/configuration.md`         | `operating/configuration.md`       | Good as-is                                 |
| `operators/database.md`              | `operating/database.md`            | Good as-is                                 |
| `operators/authentication.md`        | `operating/authentication.md`      | Scope to operational concerns only         |
| `operators/operations.md`            | `operating/operations.md`          | Terminology fix ("room:123" -> "location") |
| `operators/verifying-releases.md`    | `operating/verifying-releases.md`  | Good as-is                                 |

### Pages That Are New

| Path                    | Source                                             |
| ----------------------- | -------------------------------------------------- |
| `guide/index.md`        | New content                                        |
| `guide/connecting.md`   | New content                                        |
| `guide/commands.md`     | New content (draws from getting-started.md examples)|
| `guide/building.md`     | New content                                        |
| `extending/events.md`   | Extracted from plugin-guide.md event sections       |
| `reference/index.md`    | New content                                        |
| `reference/grpc-api.md` | Auto-generated from proto files                    |
| `reference/events.md`   | New content (partially generated)                  |

### Content Split: extending/api-guide.md vs reference/grpc-api.md

The current `developers/grpc-api.md` is mostly field tables. The new structure
splits this into two files with different purposes:

**`extending/api-guide.md`** (hand-written narrative):

- Connection lifecycle (startup, reconnection, disconnect)
- mTLS requirements and certificate setup
- How to use Subscribe with the oneof frame pattern
- Code examples in Go showing common patterns
- Error handling strategy (gRPC status codes vs application errors)
- When to use each RPC and how they relate

**`reference/grpc-api.md`** (auto-generated):

- Service definitions with all RPCs
- Request/response message field tables
- Enum values and nested types
- Type cross-references

The api-guide links to the reference for field details. The reference is
standalone and requires no narrative context.

### Content Split: Authentication Pages

The current contributors and operators authentication pages overlap
significantly. The new pages have distinct scopes:

**`contributing/authentication.md`** (internals):

- Three-layer architecture diagram (protocol -> service -> repository)
- Service responsibilities (AuthService, CharacterService, PasswordResetService)
- Timing attack prevention implementation details
- Argon2id parameter choices and rationale
- Constant-time token comparison implementation
- Code references (file paths, line numbers)
- Repository interfaces

**`operating/authentication.md`** (operations):

- Security properties (what's protected, not how)
- Rate limiting behavior table (what operators observe)
- Lockout recovery procedures
- Session management (expiry, invalidation triggers)
- Password reset flow (operator perspective)
- Monitoring: what to alert on, key log events
- Database requirements (tables needed)

Rule of thumb: if the content helps you *understand the code*, it goes in
contributing. If it helps you *run the server*, it goes in operating.

## Cross-Cutting Fixes

### Terminology

All pages MUST use project terminology. The following replacements MUST be
applied globally:

| Find                       | Replace                          | Context              |
| -------------------------- | -------------------------------- | -------------------- |
| `room` (as spatial concept)| `location`                       | Prose, examples, SQL |
| `query_room`               | `query_location`                 | Host function names  |
| `query_room_characters`    | `query_location_characters`      | Host function names  |
| `location:room1`           | `location:<id>`                  | Stream examples      |
| `room:123`                 | `location:123`                   | SQL examples         |

### Diagrams

All diagrams MUST use Mermaid. The ASCII box art diagram in
`contributing/authentication.md` (three-layer architecture) MUST be converted to
a Mermaid flowchart.

### Stale Technical Content

| Page                              | Issue                                         | Fix                                              |
| --------------------------------- | --------------------------------------------- | ------------------------------------------------ |
| `extending/api-guide.md`          | Subscribe signature outdated                  | Update to `SubscribeResponse` with oneof frame   |
| `extending/api-guide.md`          | Supported commands incomplete                 | Add describe, page, whisper                      |
| `contributing/coding-standards.md`| EventStore interface outdated                 | Update Subscribe signature to match current code |
| `extending/plugin-guide.md`       | Host function names use "room"                | Update to "location" terminology                 |

### Dead Link Elimination

Index pages MUST only link to pages that exist. The following dead links exist in
the current site and MUST be removed or replaced during migration:

**contributors/index.md** (10 dead links):

- `development.md`, `building.md`, `testing.md`
- `pull-requests.md`, `code-review.md`, `standards.md`
- `roadmap.md`, `decisions.md`, `../changelog.md`
- `code-of-conduct.md`

**developers/index.md** (3 dead links):

- `plugins/host-functions.md`, `abac.md`, `plugins/testing.md`

**operators/index.md** (9 dead links):

- `quickstart.md`, `deployment.md`, `docker.md`
- `tls.md`, `monitoring.md`, `backup.md`
- `scaling.md`, `security.md`, `access-control.md`

**Other pages** (1 dead link):

- `plugin-guide.md` links to `events.md` (will exist after migration)

Since all index pages are being rewritten, these dead links are eliminated by
writing new index pages that only reference pages in the new structure.

## Auto-Generation Pipeline

### Proto Documentation

A new Taskfile target `docs:proto` MUST generate markdown API reference from
protobuf definitions.

**Tool:** `protoc-gen-doc`, installed via:

```bash
go install github.com/pseudomuto/protoc-gen-doc/cmd/protoc-gen-doc@latest
```

This MUST be added to `task setup` alongside existing tool installs.

**Invocation:**

```yaml
# Taskfile.yaml addition
docs:proto:
  desc: Generate gRPC API reference from proto files
  cmds:
    - >-
      protoc
      --doc_out=site/docs/reference
      --doc_opt=markdown,grpc-api.md
      --proto_path=api/proto
      --proto_path={{.PROTOC_INCLUDE}}
      api/proto/holomush/core/v1/core.proto
      api/proto/holomush/web/v1/web.proto
      api/proto/holomush/control/v1/control.proto
      api/proto/holomush/plugin/v1/plugin.proto
      api/proto/holomush/plugin/v1/hostfunc.proto
  sources:
    - api/proto/holomush/*/v1/*.proto
  generates:
    - site/docs/reference/grpc-api.md
```

**Build integration:** `task docs:build` MUST declare `docs:proto` as a
dependency so the reference is always regenerated before the site builds.

**Output:** The generated file is committed to the repository (not gitignored).
This ensures the site builds without requiring protoc in the docs-only build
path. The `task docs:proto` target is run manually or in CI when protos change.

The generated output covers:

- Service definitions with all RPCs
- Message types with field descriptions
- Enum values
- Nested types

The `extending/api-guide.md` page links to the generated reference for field
details and focuses on narrative: connection lifecycle, mTLS setup, reconnection
behavior, code examples.

### Policy DSL Reference

The existing `task generate:ebnf` target generates `policy-dsl.ebnf` and
`policy-dsl-railroad.html`. The Taskfile MUST be updated to output these to
`site/docs/reference/` instead of `site/docs/developers/`.

### Future: Config Reference

A config reference page (`reference/config.md`) is NOT included in the initial
redesign. It MAY be added later, generated from cobra command definitions. The
`reference/index.md` page SHOULD NOT link to it until it exists.

## Old Directory Cleanup

After all content is migrated to the new structure, the old directories MUST be
deleted:

- `site/docs/contributors/` (replaced by `contributing/`)
- `site/docs/developers/` (replaced by `extending/` and `reference/`)
- `site/docs/operators/` (replaced by `operating/`)

This prevents stale content from being served at old URLs and avoids confusion
if someone browses the repository.

## zensical.toml Updates

Zensical auto-generates navigation from the directory structure. The current
config uses `navigation.tabs = true` and `navigation.sections = true`. Section
ordering in zensical follows alphabetical directory order by default.

To achieve the desired nav order (Guide, Operating, Extending, Contributing,
Reference), directory names MAY be prefixed with a sort index if zensical does
not support explicit ordering in config. However, the current names happen to
sort reasonably:

1. `contributing/`
2. `extending/`
3. `guide/`
4. `operating/`
5. `reference/`

This puts Guide in the middle rather than first. The implementation plan MUST
resolve nav ordering as its first task, before any content migration begins.
The resolution options in priority order:

1. Explicit `nav` configuration in `zensical.toml` (preferred if supported)
2. Numeric directory prefixes (e.g., `01-guide/`, `02-operating/`)
3. Accept alphabetical order if the above are impractical

The choice affects all internal cross-links and the migration table, so it MUST
be decided before writing any content.

## Acceptance Criteria

- [ ] Landing page renders with hero, features, coming soon, choose your path
- [ ] All five sections (guide, operating, extending, contributing, reference)
      have index pages with working links
- [ ] Zero dead links across the entire site
- [ ] Zero instances of "room" used as a spatial concept in documentation
- [ ] All diagrams render as Mermaid (no ASCII art)
- [ ] `task docs:proto` generates reference/grpc-api.md from proto files
- [ ] `task docs:build` succeeds with no warnings
- [ ] Authentication content is deduplicated (internals vs operations)
- [ ] extending/getting-started.md targets plugin developers, not core contributors
- [ ] Voice is consistent: warm, competent, approachable (not corporate, not juvenile)
- [ ] Old directories (`contributors/`, `developers/`, `operators/`) deleted
- [ ] Taskfile `generate:ebnf` outputs to `site/docs/reference/` (not `developers/`)
- [ ] `protoc-gen-doc` added to `task setup` toolchain
- [ ] `guide/building.md` covers in-game building, not programmatic extension
