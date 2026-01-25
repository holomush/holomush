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

All markdown files MUST pass `markdownlint-cli2` validation. The project uses `.markdownlint.yaml` for configuration.

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
| **SHOULD** run `task fmt`       | dprint auto-formats tables to correct alignment  |

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

Fix table alignment issues by running `task fmt` which uses dprint to auto-format.

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

Naming: `YYYY-MM-DD-<feature-name>.md`

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

## Pre-commit Validation

The project uses lefthook for pre-commit checks. Documentation changes MUST pass:

```bash
task lint:markdown
```

To check a single file:

```bash
markdownlint-cli2 path/to/file.md
```
