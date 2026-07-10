<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Documentation Guidelines

Guidelines for creating and maintaining documentation in the HoloMUSH project.

For RFC2119 keyword definitions, see the root `CLAUDE.md`.

## File Naming

| Requirement                  | Description                                             |
| ---------------------------- | ------------------------------------------------------- |
| **MUST** use kebab-case      | All documentation file names use lowercase with hyphens |
| **MUST NOT** use underscores | Use hyphens, not underscores (e.g., `my-doc.md`)        |
| **MUST NOT** use spaces      | Use hyphens instead of spaces                           |

Examples:

- `plugin-authoring.md` ✓
- `getting-started.md` ✓
- `plugin_authoring.md` ✗
- `Getting Started.md` ✗

## Markdown Standards

All markdown files MUST pass `rumdl` validation. The project uses `.rumdl.toml` for configuration (with a separate `site/.rumdl.toml` for documentation site files).

### Code Fences

| Requirement                | Description                                          |
| -------------------------- | ---------------------------------------------------- |
| **MUST** specify language  | All fenced code blocks require a language identifier |
| **MUST** use backticks     | Use triple backticks, not tildes                     |
| **MUST NOT** use hard tabs | Use spaces for indentation within code blocks        |

Valid languages include: `go`, `python`, `bash`, `json`, `yaml`, `sql`, `markdown`, `text`

Example:

````markdown
```go
func example() error {
    return nil
}
```
````

### Lists

| Requirement                       | Description                             |
| --------------------------------- | --------------------------------------- |
| **MUST** have blank lines         | Surround lists with blank lines         |
| **SHOULD** use consistent markers | Use `-` for unordered, `1.` for ordered |

### Headings

| Requirement              | Description                              |
| ------------------------ | ---------------------------------------- |
| **MUST** start with H1   | First line should be a top-level heading |
| **MUST NOT** skip levels | Don't jump from H1 to H3                 |
| **SHOULD** use ATX style | Use `#` prefix, not underlines           |

### Tables

| Requirement                     | Description                                      |
| ------------------------------- | ------------------------------------------------ |
| **MUST** align columns          | All pipes in a column must vertically align      |
| **MUST** have header separators | Include separator row with dashes after headers  |
| **MUST** have consistent style  | Use "aligned" style with spaces around cell text |
| **SHOULD** run `task fmt`       | rumdl auto-formats tables to correct alignment   |

Example of properly aligned table:

```markdown
| Column A | Column B | Column C |
| -------- | -------- | -------- |
| value 1  | value 2  | value 3  |
| longer   | short    | medium   |
```

Common table errors:

- **MD060**: Table column style - pipes not aligned or missing spaces
- **MD056**: Table column count - mismatched column counts between rows

Fix table alignment issues by running `task fmt` which uses rumdl to auto-format.

## Document Structure

### Plans (`docs/plans/`)

Plans document implementation strategies. They MUST include:

1. **Header** with goal, architecture, tech stack
2. **Tasks** with files, steps, and acceptance criteria
3. **Post-implementation** checklist

Naming: `YYYY-MM-DD-<feature-name>.md`

### Specs (`docs/specs/`)

Specs define requirements. They MUST include:

1. **Overview** describing the feature
2. **Requirements** using RFC2119 keywords
3. **Non-goals** to clarify scope

When a spec introduces or changes a **system-level invariant** (a durable
cross-cutting guarantee, not a local feature requirement), it MUST capture it in
the registry (`docs/architecture/invariants.yaml`) with a canonical
`INV-<SCOPE>-N` id, and MUST consult existing entries in the relevant scope so it
does not duplicate, contradict, or silently renumber one. See
`.claude/rules/invariants.md` for what rises to an invariant and the full
workflow. Do NOT mint ad-hoc invariant families in prose.

Naming: `YYYY-MM-DD-<feature-name>.md`

### Roadmap (`docs/roadmap.md`)

Single file (not per-theme docs) tracking strategic themes that span multiple epics. Paired with `theme:<slug>` GitHub issue labels for cross-epic queryability. The doc carries the **why** (substrate-and-uses framing, sequencing rationale, risks); GitHub Issues carry the **what** (individual work items).

Maintenance rules live in the root `CLAUDE.md` "Strategic Themes" section. Key directives:

- **MUST** keep current when adding/removing a `theme:*` label
- **MUST NOT** create per-theme markdown files (single roadmap, sectioned)
- **SHOULD** move completed themes to a "Completed themes" section with a date rather than deleting

When editing `docs/roadmap.md`, also capture or update an ADR in `docs/adr/` recording the framing.

## Nested Code Blocks

When documenting markdown within markdown (e.g., README templates), avoid nested code fences. Instead:

**Preferred approach** - Describe the content:

```markdown
Create a README.md with:

- Title section with project name
- Building instructions with `make build`
- Usage examples
```

**Avoid** - Nested fences break linting:

````markdown
```markdown
# Title

Content here
```
````

## Common Lint Errors

| Error                       | Fix                                         |
| --------------------------- | ------------------------------------------- |
| MD010: Hard tabs            | Replace tabs with spaces                    |
| MD031: Blanks around fences | Add blank line before/after code blocks     |
| MD032: Blanks around lists  | Add blank line before/after lists           |
| MD040: No language          | Add language to code fence                  |
| MD056: Table column count   | Ensure all rows have same number of columns |
| MD060: Table column style   | Run `task fmt` to fix pipe alignment        |

## Local quality checks

Run `task fmt` to format and apply license headers, and
`task pr-prep` to mirror the full CI gate before pushing. Documentation changes
MUST pass `task lint:markdown`.

To check a single file:

```bash
rumdl check path/to/file.md
```
