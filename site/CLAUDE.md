# Documentation Site Guidelines

Instructions for working with the HoloMUSH documentation site (zensical).

## Site Structure

| Path                      | Purpose                      |
| ------------------------- | ---------------------------- |
| `site/docs/`              | Documentation content        |
| `site/zensical.toml`      | Site configuration           |
| `site/.rumdl.toml`        | Markdown lint rules for site |

## Audience Directories

Documentation is organized by audience in `site/docs/`:

| Directory        | Audience                   |
| ---------------- | -------------------------- |
| `guide/`         | Players and game designers |
| `operating/`     | Server operators           |
| `extending/`     | Plugin developers          |
| `contributing/`  | Codebase contributors      |
| `reference/`     | Auto-generated references  |

## Commands

```bash
task docs:setup   # Install dependencies (uv)
task docs:serve   # Start local dev server
task docs:build   # Build static site
```

## Adding Pages

1. Create `.md` file in appropriate audience directory
2. Navigation auto-generates from directory structure
3. Use kebab-case for filenames: `getting-started.md`

## Configuration

Site settings in `zensical.toml`:

- `site_name`, `site_url`, `site_description` - Basic metadata
- `docs_dir`, `site_dir` - Directory paths
- `theme.variant`, `theme.palette` - Visual appearance
- `theme.features` - Navigation and search options

## Voice and Tone

The docs are written for people, not robots. Match this voice:

- **Conversational and direct.** Use "you" naturally. Write like you're
  explaining something to a friend who's smart but new to this.
- **Grounded, not breathless.** Don't oversell. State what something does
  and why it matters. Let the reader decide if it's exciting.
- **Vivid when it counts.** Game-world descriptions should paint a picture
  ("a hall of freestanding doorways, each opening onto a different world's
  bazaar"), but technical explanations should be plain and precise.
- **No filler.** Cut "simply," "just," "easily," "of course," "Note that."
  If something is simple, the explanation will show it.
- **Acknowledge the MU\* tradition.** Many readers come from classic MUSHes.
  Reference that background when it helps ("Unlike traditional MU\*s..."),
  but don't assume everyone does.

## Markdown Standards

Site uses same markdown standards as `docs/CLAUDE.md`:

- All code fences MUST specify language
- Tables MUST have aligned columns (run `task fmt`)
- Headings MUST NOT skip levels
- Ordered lists MUST use ascending numbers (1, 2, 3), not repeated `1.` markers
