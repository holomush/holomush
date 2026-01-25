# Website Landing Page Design

**Date:** 2026-01-24
**Status:** Draft

## Overview

Transform `site/docs` from a documentation-only site (`docs.holomush.dev`) into the
full HoloMUSH website (`holomush.dev`) with a proper landing page while keeping the
documentation-focused navigation.

## Goals

- Provide a welcoming entry point that explains what HoloMUSH is
- Highlight key features and differentiators
- Maintain existing documentation structure and navigation
- Update outdated content (WASM → Lua & Go plugins)
- Remove broken links to nonexistent pages

## Non-Goals

- Full marketing website with multiple landing pages
- Pricing, testimonials, or sales-focused content
- Demo or screenshot integration (future consideration)

## Design

### Site Configuration

**URL:** `holomush.dev` (not `docs.holomush.dev`)

**Navigation bar:**

```text
[Logo] HoloMUSH     Developers | Operators | Contributors     [GitHub]
```

Navigation MUST remain documentation-focused with the three audience sections.

### Landing Page Structure

The `index.md` MUST include these sections in order:

1. **Hero section** — Tagline, description, CTA button
2. **Feature cards** — 4 key differentiators
3. **Project status** — Active development notice
4. **Audience paths** — Links to Developers/Operators/Contributors
5. **Community** — GitHub link (Discord placeholder for future)

### Hero Content

```markdown
# HoloMUSH

**A modern platform for text-based virtual worlds**

Build immersive MUSHes with a high-performance Go server, flexible plugin
system, and modern connectivity.

[Get Started →](operators/installation.md)
```

### Feature Highlights

| Feature                | Description                                            |
| ---------------------- | ------------------------------------------------------ |
| **Go Core**            | High-performance server with event-driven architecture |
| **Lua & Go Plugins**   | Lightweight Lua scripts or powerful Go extensions      |
| **Dual Protocol**      | Classic telnet + modern web client                     |
| **PostgreSQL Backend** | Reliable, scalable data storage                        |

### Project Status

```markdown
🚧 **Active Development** — HoloMUSH is being built in the open.
Star us on GitHub to follow progress.
```

### Community Section

- GitHub: [holomush/holomush](https://github.com/holomush/holomush)
- Discord: Coming soon (placeholder, do not include link yet)

## Migration

### Configuration Changes

**`site/zensical.toml`:**

| Setting     | Current                     | New                    |
| ----------- | --------------------------- | ---------------------- |
| `site_url`  | `https://docs.holomush.dev` | `https://holomush.dev` |
| `site_name` | `HoloMUSH Documentation`    | `HoloMUSH`             |

### Content Updates

**WASM → Lua & Go plugins:**

Files requiring updates:

- `site/docs/index.md`
- `site/docs/contributors/coding-standards.md`

### Broken Links to Remove

Current `index.md` references pages that do not exist:

| Broken Link                      | Action                                                |
| -------------------------------- | ----------------------------------------------------- |
| `developers/architecture.md`     | Remove (content is in `contributors/architecture.md`) |
| `operators/quickstart.md`        | Remove                                                |
| `developers/plugins/tutorial.md` | Remove                                                |
| `contributors/roadmap.md`        | Remove                                                |
| `changelog.md`                   | Remove                                                |

These MAY be added later when the content exists.

## Testing

After implementation:

1. Run `task docs:build` — MUST complete without errors
2. Run `task docs:serve` — MUST render landing page correctly
3. Verify all navigation links work
4. Verify no broken internal links remain

## Future Considerations

- Add Discord link when community channel is created
- Add demo/screenshot section when visual assets are available
- Create roadmap page when project milestones are defined
- Create changelog when release process is established
