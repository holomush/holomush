---
name: Verify imports against code blocks
description: When a plan adds new package-level identifiers to a file (slog., timestamppb., status., codes.), check that the explicit "imports needed" directive lists every missing/new import not already in the target file
type: feedback
---

# Verify imports against code blocks

When reviewing a plan that ships an example code block plus an "imports needed" or "add imports" directive, the directive must enumerate every missing/new import the code block references — packages already imported in the target file don't need to be listed.

**Why:** Plans tend to under-list imports because the author writes the code first (compiler-checks would have caught it) and the import list second (manual). Two patterns I've now seen on the same review:

1. New `QueryHistory` body sprinkled with `slog.InfoContext(...)`; `import "log/slog"` not in the directive even though the file currently has no `slog` import.
2. `fakeAuditStore.queryLog` parameter list uses `*timestamppb.Timestamp`; `timestamppb` not in the test file's import block.

Per writing-plans, every code block must be the actual content the engineer needs. A missing import is a compile error the implementer hits on first run — fast to fix, but it's still a "plan failure" by the skill's checklist.

**How to apply:**

For every code block that references a package qualifier (`pkg.Symbol`), grep the existing target file's imports — confirm the package is already imported, or confirm the plan's "add imports" line lists it. Cross-reference both directions: imports listed but not used (dead) and uses without imports listed (compile error).
