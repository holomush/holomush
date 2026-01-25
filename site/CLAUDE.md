# Documentation Site Guidelines

Instructions for working with the HoloMUSH documentation site (zensical).

## Site Structure

| Path                      | Purpose                      |
| ------------------------- | ---------------------------- |
| `site/docs/`              | Documentation content        |
| `site/zensical.toml`      | Site configuration           |
| `site/.markdownlint.yaml` | Markdown lint rules for site |

## Audience Directories

Documentation is organized by audience in `site/docs/`:

| Directory       | Audience              |
| --------------- | --------------------- |
| `contributors/` | Codebase contributors |
| `developers/`   | Plugin developers     |
| `operators/`    | Server operators      |

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

## Markdown Standards

Site uses same markdown standards as `docs/CLAUDE.md`:

- All code fences MUST specify language
- Tables MUST have aligned columns (run `task fmt`)
- Headings MUST NOT skip levels
