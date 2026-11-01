# Planning archive (read-only)

This directory is the archive of the GSD workflow, which HoloMUSH used through
the v0.13 milestone. GSD is retired (#5069). Do not add or edit files here.

- New work starts as an OpenSpec change in `openspec/changes/<name>/`. Use
  `/opsx:explore`, `/opsx:propose` and `/opsx:apply`.
- Release planning lives in GitHub milestones and in the holomush organization
  project #1. Its Priority field stays in sync with the `priority::*` labels.
- Discrete work items, bugs and the former `## Backlog` entries are GitHub
  issues.

The files stay in place because code comments, `docs/architecture/invariants.yaml`
`origin_spec` entries, ADRs and old issues cite them. Completed milestone phases
are under `milestones/<version>-phases/`. The `phases/999.*` directories are
the old backlog, and GitHub issues #5070 to #5160 replaced them.
