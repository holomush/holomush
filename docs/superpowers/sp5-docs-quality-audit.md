<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# SP5 Documentation Quality Audit

Per-page editorial quality audit of the HoloMUSH docs site (`site/src/content/docs/`). Scored against the 8-dimension rubric in [`contributing/reference/docs-style-guide.md`](/contributing/reference/docs-style-guide.md). Covers exactly 78 content pages (excludes root `index.mdx` splash and auto-generated `reference/events/*` sub-pages). Date: 2026-05-28.

## Summary

| Section        | Pages | P1 | P2 | OK |
| -------------- | ----- | -- | -- | -- |
| guide          | 5     | 1  | 1  | 3  |
| operating      | 23    | 1  | 6  | 16 |
| extending      | 24    | 1  | 5  | 18 |
| contributing   | 22    | 1  | 3  | 18 |
| reference      | 5     | 1  | 2  | 2  |
| **Total**      | **79**| **5**  | **17** | **57** |

---

## guide (5 pages)

| Page (slug/path)                | Orient | Aud | Mode | Clarity | Ex | Term | Xlink | Concise | Total | Priority | Biggest issue |
| ------------------------------- | ------ | --- | ---- | ------- | -- | ---- | ----- | ------- | ----- | -------- | ------------- |
| `guide/index.mdx`               | 2      | 2   | 2    | 2       | 1  | 2    | 2     | 2       | 15    | OK       | None material; "build" section could link to the world-model explanation |
| `guide/explanation/the-world.md`| 1      | 2   | 1    | 2       | 2  | 1    | 1     | 1       | 11    | P2       | Uses "rooms" ("dig new rooms") — terminology violation; opener lacks audience cue |
| `guide/how-to/connecting.md`    | 2      | 2   | 2    | 2       | 2  | 2    | 1     | 2       | 15    | OK       | No link to Commands reference from the "Your First Connection" section |
| `guide/how-to/building.md`      | 1      | 2   | 0    | 2       | 1  | 1    | 1     | 1       | 9     | P1       | Mode: concept-lecture in a how-to with no actual steps; also uses "rooms" |
| `guide/reference/commands.md`   | 2      | 2   | 2    | 2       | 2  | 2    | 1     | 2       | 15    | OK       | Missing link to next how-to or explanation page from the command reference |

---

## operating (23 pages)

| Page (slug/path)                                        | Orient | Aud | Mode | Clarity | Ex | Term | Xlink | Concise | Total | Priority | Biggest issue |
| ------------------------------------------------------- | ------ | --- | ---- | ------- | -- | ---- | ----- | ------- | ----- | -------- | ------------- |
| `operating/index.mdx`                                   | 2      | 2   | 2    | 2       | 1  | 2    | 2     | 2       | 15    | OK       | None material |
| `operating/explanation/authentication.md`               | 2      | 2   | 2    | 2       | 1  | 2    | 2     | 2       | 15    | OK       | None material |
| `operating/explanation/plugin-security.md`              | 2      | 2   | 2    | 1       | 0  | 2    | 2     | 1       | 12    | P2       | No example of what a flagged timeout or registry-full looks like; prose is dense internal-tech |
| `operating/how-to/authentication-recovery.md`           | 2      | 2   | 2    | 2       | 1  | 2    | 2     | 2       | 15    | OK       | "Without email" section is a stub caution block; steps are absent |
| `operating/how-to/ca-rotation.md`                       | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `operating/how-to/crypto/crypto-monitoring.md`          | 2      | 1   | 1    | 1       | 2  | 2    | 2     | 1       | 12    | P2       | Mode mismatch: reference tables mixed with how-to alert-rule recipes in a single page; audience not oriented (who should read this?) |
| `operating/how-to/crypto/crypto-runbook.md`             | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 1       | 15    | OK       | Architecture overview section is substantial but appropriate for a runbook; minor conciseness issue with mermaid diagrams that belong in an explanation |
| `operating/how-to/crypto/crypto-setup.md`               | 1      | 2   | 1    | 1       | 1  | 2    | 1     | 1       | 10    | P2       | Opens with "currently a stub" which breaks orientation; in-game grant UX section is unresolved prose padding |
| `operating/how-to/database.md`                          | 2      | 2   | 2    | 2       | 2  | 2    | 1     | 2       | 15    | OK       | Missing link to contributing/database-migrations for the operator-facing create-migration topic |
| `operating/how-to/deploy/deployment.md`                 | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `operating/how-to/deploy/installation.md`               | 1      | 2   | 2    | 2       | 2  | 2    | 2     | 1       | 14    | OK       | Opener "This guide covers installing…" is flat; minor verbosity in the Custom Docker Compose section |
| `operating/how-to/deploy/verifying-releases.md`         | 2      | 2   | 2    | 2       | 2  | 2    | 1     | 2       | 15    | OK       | No link to installation or deployment pages as logical next steps |
| `operating/how-to/operations.md`                        | 2      | 2   | 2    | 1       | 2  | 2    | 2     | 1       | 14    | OK       | Sections on backup/restore duplicate content in database.md and deployment.md; minor redundancy |
| `operating/how-to/plugin-reloads.md`                    | 1      | 1   | 1    | 1       | 1  | 2    | 0     | 1       | 8     | P1       | No orientation opener; audience not stated; no links to any other page; mode unclear (reference table posing as how-to) |
| `operating/how-to/sandbox/sandbox-operations.md`        | 2      | 1   | 2    | 1       | 2  | 2    | 2     | 1       | 13    | P2       | Audience is narrowly the project's own sandbox operators but not stated; several bash snippets are dense without explanatory context |
| `operating/how-to/sandbox/sandbox-restore.md`           | 2      | 2   | 2    | 2       | 2  | 2    | 1     | 2       | 15    | OK       | Missing link back to sandbox-operations.md as prerequisite |
| `operating/how-to/sentry.md`                            | 2      | 2   | 2    | 1       | 2  | 2    | 1     | 1       | 13    | P2       | Long "Open follow-ups" section is internal bead tracking, not operator-facing documentation; "trial integration" framing creates ambiguity |
| `operating/how-to/telnet-security.md`                   | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `operating/how-to/tune-plugin-resource-limits.md`       | 2      | 2   | 2    | 2       | 1  | 2    | 2     | 2       | 15    | OK       | Could include one numeric example of a raised value in context |
| `operating/reference/authentication.md`                 | 2      | 2   | 2    | 2       | 1  | 2    | 2     | 2       | 15    | OK       | None material |
| `operating/reference/configuration.md`                  | 1      | 2   | 2    | 1       | 2  | 2    | 1     | 1       | 12    | P2       | Opener "This guide covers all configuration options" is flat and under-orients; no link to the plugin config reference |
| `operating/reference/monitoring.md`                     | 2      | 2   | 2    | 2       | 1  | 2    | 2     | 2       | 15    | OK       | None material |
| `operating/reference/plugin-metrics.md`                 | 2      | 2   | 2    | 2       | 0  | 2    | 2     | 2       | 14    | OK       | No example values or thresholds; the link back to tune-plugin-resource-limits partially covers this |

---

## extending (24 pages)

| Page (slug/path)                                     | Orient | Aud | Mode | Clarity | Ex | Term | Xlink | Concise | Total | Priority | Biggest issue |
| ---------------------------------------------------- | ------ | --- | ---- | ------- | -- | ---- | ----- | ------- | ----- | -------- | ------------- |
| `extending/index.mdx`                                | 2      | 2   | 2    | 2       | 1  | 2    | 2     | 2       | 15    | OK       | None material |
| `extending/explanation/audit-chain.md`               | 1      | 1   | 2    | 1       | 2  | 2    | 2     | 1       | 12    | P2       | Audience is "developers adding new host-side chains" but stated mid-page not in the opener; prose assumes deep internals familiarity |
| `extending/explanation/substrate-invariants.md`      | 2      | 2   | 2    | 2       | 1  | 2    | 2     | 2       | 15    | OK       | None material |
| `extending/how-to/abac-attribute-resolver.md`        | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `extending/how-to/access-control.md`                 | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `extending/how-to/audit-events.md`                   | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `extending/how-to/event-sensitivity.md`              | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `extending/how-to/handle-plugin-errors.md`           | 2      | 2   | 2    | 2       | 2  | 2    | 1     | 2       | 15    | OK       | Missing link to the full Plugin API reference from within the procedure |
| `extending/how-to/lua-plugin-capabilities.md`        | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `extending/how-to/plugin-config.md`                  | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `extending/how-to/plugin-crypto-readback.md`         | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `extending/how-to/plugin-host-evaluate.md`           | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `extending/how-to/register-emit-types.md`            | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `extending/how-to/verb-registration.md`              | 1      | 2   | 2    | 1       | 2  | 2    | 1     | 1       | 12    | P2       | Orientation opener is absent — drops straight into "HoloMUSH uses a verb registry"; no link to Plugin Guide or Plugin API reference at end |
| `extending/reference/actor-kinds-claimable.md`       | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `extending/reference/api-guide.md`                   | 1      | 1   | 1    | 1       | 1  | 2    | 1     | 1       | 9     | P1       | Mode confusion: reference page titled "API Guide" but reads like a mix of explanation and tutorial; no orientation for who this page is for; missing runnable examples |
| `extending/reference/events.md`                      | 2      | 2   | 2    | 2       | 2  | 2    | 1     | 2       | 15    | OK       | Missing link to extending/how-to/event-sensitivity for the sensitivity subsection |
| `extending/reference/plugin-api.md`                  | 2      | 2   | 2    | 1       | 2  | 2    | 2     | 1       | 14    | OK       | Large API dump; World-query functions table shown only partially in what was read but the page is well-structured overall |
| `extending/reference/plugin-config.md`               | 2      | 2   | 2    | 2       | 1  | 2    | 2     | 2       | 15    | OK       | Lua config accessor section (read partially) appears well-formed; no worked examples in the reference itself |
| `extending/reference/substrate-contract.md`          | 2      | 2   | 2    | 2       | 1  | 2    | 2     | 2       | 15    | OK       | Future SDK table (eventkit/groupkit) could note it's not yet built more prominently |
| `extending/tutorials/binary-plugins.md`              | 1      | 2   | 1    | 2       | 2  | 2    | 1     | 1       | 12    | P2       | Orientation opener is flat; page starts with decision table but lacks narrative setup; misses link to Getting Started as prerequisite |
| `extending/tutorials/getting-started.md`             | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `extending/tutorials/lua-plugins.md`                 | 1      | 2   | 1    | 1       | 2  | 2    | 1     | 1       | 11    | P2       | Opener lacks narrative setup; drops straight into structure; reads more like reference than tutorial; no "what you'll learn" framing |
| `extending/tutorials/plugin-guide.mdx`               | 1      | 2   | 1    | 1       | 2  | 2    | 1     | 1       | 11    | P2       | Treated as both tutorial and reference simultaneously; lacks the narrative arc of a proper tutorial (no "what you'll build" opener, no closing "what you built" section) |

---

## contributing (22 pages)

| Page (slug/path)                                           | Orient | Aud | Mode | Clarity | Ex | Term | Xlink | Concise | Total | Priority | Biggest issue |
| ---------------------------------------------------------- | ------ | --- | ---- | ------- | -- | ---- | ----- | ------- | ----- | -------- | ------------- |
| `contributing/index.mdx`                                   | 2      | 2   | 2    | 2       | 1  | 2    | 2     | 2       | 15    | OK       | None material |
| `contributing/explanation/architecture.md`                 | 1      | 1   | 2    | 1       | 1  | 2    | 1     | 1       | 10    | P2       | No orientation opener; audience assumed; diagram-heavy but prose light; missing link to event-store or gateway-boundary explanation |
| `contributing/explanation/authentication.md`               | 1      | 1   | 1    | 1       | 1  | 2    | 2     | 1       | 10    | P2       | Mode: this reads as an internal spec dump (source file line references like `auth_service.go:81`) rather than an explanation; audience is undeclared |
| `contributing/explanation/event-emit-pipeline.md`          | 2      | 1   | 2    | 2       | 2  | 2    | 2     | 2       | 15    | OK       | Assumes contributor audience without saying so; otherwise well-formed explanation |
| `contributing/explanation/event-store.md`                  | 2      | 1   | 2    | 2       | 2  | 2    | 2     | 2       | 15    | OK       | `---` separator mid-page is noise; audience not stated in opener |
| `contributing/explanation/gateway-boundary.md`             | 2      | 2   | 2    | 2       | 1  | 2    | 2     | 2       | 15    | OK       | Missing a concrete "wrong" example to pair with the "how to apply" guidance |
| `contributing/explanation/hostfunc-context-audit.md`       | 2      | 1   | 2    | 2       | 1  | 2    | 2     | 2       | 14    | OK       | Audience (hostfunc contributors) not declared in opener |
| `contributing/explanation/integration-test-harness.md`     | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `contributing/explanation/lifecycle-and-health.md`         | 2      | 2   | 2    | 2       | 1  | 2    | 2     | 2       | 15    | OK       | None material; the diagram-heavy approach works well for this topic |
| `contributing/how-to/add-a-host-function.md`               | 0      | 1   | 1    | 1       | 1  | 2    | 2     | 1       | 9     | P1       | Three-sentence stub with no orientation and no worked example; orientation is completely absent; steps are skeletal |
| `contributing/how-to/database-migrations.md`               | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `contributing/how-to/integration-tests.md`                 | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `contributing/how-to/migrate-world-querier.md`             | 2      | 2   | 2    | 2       | 2  | 2    | 1     | 2       | 15    | OK       | Missing link to contributing/explanation/architecture as context for why the boundary matters |
| `contributing/how-to/pr-guide.md`                          | 1      | 2   | 2    | 2       | 1  | 2    | 2     | 2       | 14    | OK       | Opener does not state what a PR achieves; it immediately lists "this ensures…" items without a grounding sentence |
| `contributing/how-to/pr-prep.md`                           | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `contributing/how-to/proto-doc-comments.md`               | 2      | 2   | 2    | 2       | 2  | 2    | 1     | 2       | 15    | OK       | None material; could cross-link to coding standards or a proto reference |
| `contributing/how-to/quarantine.md`                        | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `contributing/how-to/sessions.md`                          | 2      | 2   | 2    | 2       | 2  | 2    | 1     | 2       | 15    | OK       | Missing link to CLAUDE.md or contributing/index.md as entry context |
| `contributing/reference/coding-standards.md`               | 1      | 2   | 2    | 1       | 1  | 2    | 1     | 1       | 11    | P2       | Opener "This guide covers coding conventions" is flat; page is quite long with RFC2119 and Go sections but examples are sparse |
| `contributing/reference/docs-style-guide.md`               | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `contributing/reference/hostfunc-audit-table.md`           | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 2       | 16    | OK       | None material |
| `contributing/reference/integration-test-harness.md`       | 2      | 2   | 2    | 2       | 1  | 2    | 2     | 2       | 15    | OK       | Example-sparse in the helper catalog; tables are well-formed |

---

## reference (5 pages)

| Page (slug/path)              | Orient | Aud | Mode | Clarity | Ex | Term | Xlink | Concise | Total | Priority | Biggest issue |
| ----------------------------- | ------ | --- | ---- | ------- | -- | ---- | ----- | ------- | ----- | -------- | ------------- |
| `reference/index.mdx`         | 2      | 2   | 2    | 2       | 0  | 2    | 2     | 2       | 14    | OK       | No examples; index pages don't require them but the Policy DSL Grammar entry is listed without a link |
| `reference/access-control.md` | 2      | 2   | 2    | 2       | 2  | 2    | 2     | 1       | 15    | OK       | None material; minor verbosity in the DSL section |
| `reference/audit-subjects.md` | 1      | 1   | 2    | 1       | 1  | 2    | 1     | 1       | 10    | P2       | Opener is a mid-sentence statement about ABAC; no audience orientation; links only to a design spec, not to related operator/extending pages |
| `reference/events.md`         | 1      | 1   | 2    | 1       | 0  | 2    | 0     | 1       | 8     | P1       | Stub index page: three list items and no prose; no orientation; no links to explanation pages; no examples |
| `reference/grpc-api.md`       | 0      | 2   | 2    | 1       | 1  | 2    | 1     | 1       | 10    | P2       | Auto-generated: opener is a bare table of contents with no orientation sentence; no link back to extending/reference/api-guide |
