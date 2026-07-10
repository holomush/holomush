# Beads (bd) issue-tracker archive

HoloMUSH tracked issues in [Beads (bd)](https://github.com/steveyegge/beads) from
2026-01 through 2026-07. On 2026-07-09 the project moved to native GSD planning
(`.planning/`) plus GitHub Issues, and the bd database was exported and archived
here.

## Files

| File | Contents |
| --- | --- |
| `2026-07-09-beads-export-full.jsonl.gz` | Complete export: 5,894 issue records (5,386 closed, 497 open, 9 in_progress, 2 deferred). Gzipped JSONL, one record per line. |
| `2026-07-09-beads-live.jsonl` | The 508 non-closed records at export time, uncompressed for grepping. |
| `TRIAGE.md` | Migration triage: per-bead verdict (keep / already-done / stale / duplicate) and routing (GitHub issue / GSD backlog / archive-only). |

## Provenance conventions preserved elsewhere

- **Decision beads** (`-t decision`) were captured as ADRs in `docs/adr/<bd-id>-<slug>.md` — those files remain canonical.
- Migrated GitHub issues carry a `Migrated-from: <bead-id>` line in their body.
- Historic `bd github sync` mirror issues (bulk-closed as NOT_PLANNED) are referenced by `external_ref` in the export records.

## Recovery

```bash
gunzip -k 2026-07-09-beads-export-full.jsonl.gz
jq 'select(.id=="holomush-XXXX")' 2026-07-09-beads-export-full.jsonl
```
