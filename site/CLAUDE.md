# Documentation Site Guidelines

Instructions for working with the HoloMUSH documentation site (Astro + Starlight).

## Site Structure

| Path                              | Purpose                                    |
| --------------------------------- | ------------------------------------------ |
| `site/src/content/docs/`          | Documentation content (Astro collection)   |
| `site/src/assets/`                | Image and static asset sources             |
| `site/public/`                    | Static files served at the root            |
| `site/astro.config.mjs`           | Site configuration (navigation, sidebars)  |
| `site/.rumdl.toml`                | Markdown lint rules for site               |

## Audience Directories

Documentation is organized by audience in `site/src/content/docs/`:

| Directory        | Audience                   |
| ---------------- | -------------------------- |
| `guide/`         | Players and game designers |
| `operating/`     | Server operators           |
| `extending/`     | Plugin developers          |
| `contributing/`  | Codebase contributors      |
| `reference/`     | Auto-generated references  |

## Commands

```bash
task docs:setup   # Install dependencies (bun install)
task docs:serve   # Start local dev server (bunx astro dev)
task docs:build   # Build static site (bunx astro build)
```

## Adding Pages

1. Create `.md` or `.mdx` file in the appropriate audience directory under `site/src/content/docs/`
2. Add the page to the sidebar in `site/astro.config.mjs` (navigation is explicit, not auto-generated)
3. Use kebab-case for filenames: `getting-started.md`
4. Include required frontmatter: `title:` and optionally `description:`

## Configuration

Site settings in `site/astro.config.mjs`:

- `title`, `description` - Basic metadata
- `starlight.sidebar` - Navigation structure (explicit, ordered)
- `starlight.social` - Social links
- Theme, search, and other Starlight options

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
