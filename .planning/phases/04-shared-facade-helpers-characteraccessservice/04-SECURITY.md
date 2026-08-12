---
phase: 04
slug: shared-facade-helpers-characteraccessservice
status: verified
# threats_open = count of OPEN threats at or above workflow.security_block_on severity (the blocking gate)
threats_open: 0
asvs_level: 1
created: 2026-08-11
---

# Phase 04 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

Verification depth was raised to L2/L3 for every `high` row despite `asvs_level: 1`. The
ASVS-L1 short-circuit (`threats_open: 0` + register authored at plan time) would have closed this
phase on the executors' own `## Threat Flags` self-reports. That was declined deliberately: this
phase's code review found **two guards that passed identically under the bug they claimed to pin**,
so a self-report of "mitigated" is weak evidence here. Every CLOSED row below cites `path:line` in
implementation or asserting-test code.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Anonymous → public facade | `GetCharacterProfile`, `ListCharacterDirectory` serve unauthenticated callers; no `viewer:` principal is constructed and `resolveAndGate` is never reached | Public character identity (name, pronouns, public `profile.*` rows, in-world description) |
| Session → owner facade | `ListMyCharacters`, `GetMyCharacter`, `UpdateCharacterProfile`, `UpdateCharacterDescription`; identity from the server-side session, never from a client-supplied id | A player's own characters and their full property slice |
| Web BFF → core gRPC | `internal/web` proxies translate only; all game state flows through core RPCs (`.claude/rules/gateway-boundary.md`) | Session token in header; typed request/response messages |
| Facade → world domain | `CharacterAccessService` → `world.Service.UpdateCharacterProfileAttributes` / `UpdateCharacterDescription`, gated by `ownedCharacterForMutation` | Character aggregate mutations under optimistic concurrency (CAS on `characters.version`) |
| Alt-linkage boundary | `entity_properties.owner` is a SCALAR, so an "all alts" rule collapses to "owner is that player's only character". The grid side is character-keyed; the web side must not disclose that two characters share a player | Character↔player association (MUST NOT cross) |

---

## Threat Register

65 threats across nine plans. Threat IDs collide across plans (`T-04-13` names two distinct
threats, `T-04-25` three); disambiguated as `ID/pNN`. Full per-threat evidence table with
`path:line` citations is in the audit record below — this table summarises by group.

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-04-01/p01 | Information Disclosure | `GetCharacterProfile` not-found | high | mitigate | One literal on every arm (`characteraccess_service.go:391,401,409,551,554`); differential over status + message + **marshaled bytes** with a non-vacuity control (`character_profile_read_test.go:363-389`) | closed |
| T-04-07/p01 | Information Disclosure | `read_description` permits | high | mitigate | Narrow new action (`seed.go:937,953`); gate `world/service.go:866`; two-field return type `world/character.go:51-57` | closed |
| T-04-09/p01, /p04 | Elevation of Privilege | viewer principal construction | high | mitigate | Exhaustive switch with denying default (`characteraccess_service.go:319-331`) + positive control | closed |
| T-04-27/p01 | Spoofing | `resolveViewerIdentity` | high | mitigate | Server-side session + `player.IsGuest`; unresolvable ⇒ `anonymous` (least-privileged) (`:287-305`) | closed |
| T-04-10/p01, /p04 | Information Disclosure | response message shape | high | mitigate | Descriptor carries no `player_id` / `location_id` / visibility hint (`characteraccess.proto:99-120,158-167,202-233`) | closed |
| T-04-08/p02, /p08 | Spoofing | shared guest gate | high | mitigate | Single definition `player_gate.go:108-126`, embedded at both servers; set-equality census `characteraccess_routing_census_test.go:487-501` | closed |
| T-04-11/p02, /p05 | Elevation of Privilege | ownership resolution | high | mitigate | Resolves against `charRepo.ListByPlayer` (`player_gate.go:92-102`) — the client-supplied id is never trusted | closed |
| T-04-02/p04 | Information Disclosure | `projectPublic` marshaled response | high | mitigate | Omission **by construction** (`characteraccess_projection.go:79-94`); byte scan + paired positive control | closed |
| T-04-15/p04 | Information Disclosure | term-A/term-B collapse | high | mitigate | Both terms always evaluated and ANDed (`profilevis.go:169-179`) | closed |
| T-04-17/p04 | Information Disclosure | admissibility↔value join | high | mitigate | `byID` built from the SAME term-B-filtered slice; divergence ⇒ Internal (`:484-511`) | closed |
| T-04-03/p05 | Information Disclosure | derived owner peer / alt linkage | high | mitigate | No `viewer:` principal on the owner path; two-alt regression + both controls (`viewer_alt_linkage_test.go:126,152,162`); INV-ACCESS-15 bound | closed |
| T-04-17/p05 | Information Disclosure | self-detection branch | high | mitigate | Handler has no identity/owner branch — one code path for every viewer (`characteraccess_service.go:385-425`) | closed |
| T-04-05/p06 | Elevation of Privilege | `update_mask` | high | mitigate | Twelve-path exact-string map; container prefix rejected; empty-mask short-circuit AFTER ownership (`characteraccess_write.go:118-155,276-288`) | closed |
| T-04-19/p06, T-04-24/p09 | Tampering | absent/zero `expected_version` | high | mitigate | Rejected before any read (`characteraccess_write.go:189-199`; `world/service.go:1062-1067`) | closed |
| T-04-20/p06, T-04-25/p09 | Tampering | concurrent profile mutation | high | mitigate | Caller's version threaded to `AND version = $4` (`character_repo.go:155`); non-vacuity — mock armed only for the caller's version | closed |
| T-04-29/p06 | Tampering | concurrent DESCRIPTION mutation | high | **accept** | Facade compare narrows the window (`characteraccess_write.go:475-485`); limit pinned by a documenting test. **See Accepted Risks** | closed (accepted) |
| T-04-33/p06 | Tampering | oversized `characters.description` | high | mitigate | #4954 fixed: `world/service.go:937-939` → `character.go:120-126`; W3/W4 assert the row UNCHANGED | closed |
| T-04-23/p07 | Information Disclosure | directory response message | high | mitigate | Two-field descriptor; byte assertion + control (`character_directory_test.go:194-199`) | closed |
| T-04-26/p07 | Elevation of Privilege | bulk enumeration | high | mitigate | Gate evaluated BEFORE `ListAll`; `listCalls == 0` on deny. Residual scope: **#4957** | closed |
| T-04-29/p07 | Tampering | `evaluateGate` infra branch | high | mitigate | Fail-closed `infra:` prefix, nil error (`characteraccess_directory.go:201-212`) | closed |
| T-04-32/p08 | Elevation of Privilege | ownership indirection | high | mitigate | Wrapper integrity asserted BEFORE the set comparison (`:517-542`) | closed |
| T-04-26/p08 | Information Disclosure | uninventoried character RPC | high | mitigate | Descriptor census, both directions (`character_rpc_census_test.go`) | closed |
| T-04-34/p08 | Elevation of Privilege | RPC referencing neither gate | high | mitigate | Audience partition + derived-public counterpart, closing the park-in-the-literal bypass (`:607-683`) | closed |
| T-04-36/p08 | Elevation of Privilege | shape outside unary filter | high | mitigate | Fail-closed classifier; non-handler set proven non-RPC; promotion guard (`:614-689`) | closed |
| T-04-23/p09 | Elevation of Privilege | widening `propertyRepo` | high | mitigate | Stays `PropertyReader` (`world/service.go:106`), pinned by `fence_test.go:34,39` | closed |
| T-04-04, -06, -12, -13, -14, -16, -18, -21, -22, -24, -25, -27, -28, -30, -31, -35 (medium/low, all plans) | Tampering · Repudiation · Info Disclosure · EoP · DoS | infra branches, denial uniformity, envelope emission, census non-vacuity, ordering, scope | medium / low | mitigate | Each cited `path:line` in the audit record | closed |
| T-04-SC ×9 (p01–p09) | Tampering | package installs | low | **accept** | Verified, not asserted: `git diff main...HEAD` over `go.mod`, `go.sum`, `web/package.json`, `package.json` is **empty** | closed (accepted) |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above workflow.security_block_on count toward threats_open*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

### Execution-record gap — plan 04-02

`04-02-SUMMARY.md` carries **no `## Threat Flags` section**. Its five threats (T-04-08, T-04-11,
T-04-12, T-04-13, T-04-SC) were therefore verified **against code only**; no mitigation was
credited to the absent record. Absence of a record is not evidence of mitigation. All five close on
`internal/grpc/player_gate.go` and `player_gate_test.go`.

### Unregistered flags (non-blocking, filed)

Both are new attack surface with **no mapping to any register row**. All nine SUMMARYs report
"None" under Threat Flags, which is inaccurate for these two:

| Flag | Why unregistered | Filed |
|------|------------------|-------|
| Anonymous `ListCharacterDirectory` enumerates every row unfiltered/unbounded (no status filter, no guest filter, no LIMIT; one ABAC eval per row) | T-04-26/p07 covers only whether a controlling decision exists — it does, at `characteraccess_directory.go:102`. The **floor value** (`anonymous`, `seed.go:610`) and the absent row filtering/bound fall outside every register row | #4957 |
| Owner clear→re-set of a profile field resets that row's visibility to `public`, erasing operator remediation | No register row covers the visibility column on re-create | #4958 |

Also confirmed accurate (tracked in **#4959**): the domain-side
`checkAccess(caller, "write", character:<id>)` on the profile write **is** self-referential — the
facade mints the subject from the target id (`characteraccess_write.go:322`) and
`seed:player-self-access` matches on `resource.character.id == principal.character.id`
(`seed.go:41`), so it always permits. The load-bearing control is `ownedCharacterForMutation`
(`characteraccess_write.go:171-180`), verified present. This does not open T-04-11 or T-04-26/p09,
but plan 04-09's stated trust boundary ("a repository call from outside would bypass ABAC")
overstates what the domain gate contributes on this path.

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-04-01 | T-04-29/p06 | `world.Service.UpdateCharacterDescription` takes no `expectedVersion`; it re-reads and guards on its own freshly-read version. The facade compare (`characteraccess_write.go:475-485`) catches the common stale-edit-form case but cannot close the read-to-re-read window — a writer landing inside it still last-write-wins. Closing it means extending a domain signature that is load-bearing across the command-layer interface and the plugin capability ABI. The limit is pinned by a documenting test (`characteraccess_write_test.go:771-784`), not left to be discovered. **Re-open when:** #4956 lands (extend the domain signature), or a lost description update is observed in production. The documenting test must be REPLACED, not deleted, when that happens. | plan 04-06 threat model (`disposition: accept`, set at plan time and carried through plan review) | 2026-08-11 |
| AR-04-02 | T-04-SC (all nine plans) | Phase 4 installs no external packages. Verified rather than asserted: `git diff main...HEAD` over `go.mod`, `go.sum`, `web/package.json` and `package.json` is empty. **Re-open when:** any future plan in this phase adds a dependency. | plan threat models, all nine (`disposition: accept`, set at plan time) | 2026-08-11 |
| AR-04-03 | INV-PRIVACY-10 (registry, not a register row) | Deliberately `binding: pending` (`docs/architecture/invariants.yaml:2191`). 01-SPEC §8.8's clause — that configuration cannot raise `name` or `pronouns` above the profile's reachability floor — is unprovable in v0.13: `name` is structurally immune (projected from the character row, never floor-evaluated) but `pronouns` is a floor-evaluated `entity_properties` row, and the engine is deny-overrides, so an admin `forbid` beats the seeded floor. Recorded here so the pending binding is not later mistaken for an oversight. **Re-open when:** v0.14 introduces a mechanism that can express the config clause. | **maintainer ruling** — `/gsd-verify-work 4`, UAT test 1, ruled `pass` 2026-08-11 | 2026-08-11 |

> **Provenance note.** AR-04-01 and AR-04-02 record dispositions their plans' `<threat_model>`
> blocks already set to `accept` before execution; they are not fresh acceptances made during this
> audit. AR-04-03 is the only entry backed by an explicit maintainer ruling in this session.

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-08-11 | 65 | 65 | 0 | gsd-security-auditor (L2/L3 depth on every `high` row) |

Prior domain gates on the same change set:

| Date | Gate | Result |
|------|------|--------|
| 2026-08-11 | `abac-reviewer` | READY — 6 findings, 0 blocking |
| 2026-08-11 | `/gsd-code-review --depth=deep --fix --auto` | 3 passes, 14 findings, 14 fixed, 0 skipped |
| 2026-08-11 | `crypto-reviewer` | Not applicable — phase diff touches zero files on the crypto surface (evidenced, not assumed) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-08-11
