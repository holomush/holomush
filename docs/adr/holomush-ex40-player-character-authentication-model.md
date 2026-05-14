<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Player-Character Authentication Model

**Date:** 2026-02-02
**Status:** Accepted
**Decision:** holomush-ex40
**Originally:** ADR 0003
**Deciders:** HoloMUSH Contributors

## Context

MUSH platforms must decide where authentication credentials live in the data model. Two
approaches exist:

**Option A: Classic MUSH (character-level passwords)**

```text
Character "Alaric" → password: "sword123"
Character "Beatrix" → password: "magic456"
```

- Each character has its own password
- Players remember multiple passwords
- No concept of "player account" spanning characters
- Switching characters requires re-authentication

**Option B: Modern approach (player-level authentication)**

```text
Player "alice" → password: "SecurePass!2026"
  ├─ Character "Alaric"
  ├─ Character "Beatrix"
  └─ Character "Cirdan"
```

- Players authenticate once at account level
- Characters are owned by players, selected post-authentication
- One password secures all characters
- Switching characters requires no re-authentication

## Decision

We chose **player-level authentication** with post-login character selection.

Players authenticate with username and password at the account level. After successful
authentication, the session is created without a character binding. The player then
selects from their owned characters, which binds the session to that character.

**Key characteristics:**

- One player can own multiple characters (default limit: 5)
- Password exists at player level, not character level
- Sessions start with `character_id = NULL`
- Character selection updates the session in place

## Rationale

### 1. Seamless character switching in web client

The web client enables character switching without re-authentication. A player can switch
from Alaric to Beatrix by calling `SelectCharacter`—no password required. With classic
MUSH authentication, each switch would require entering a new password, degrading the
modern web experience.

### 2. Better audit trails

All actions tie back to a single player identity. When investigating abuse or enforcing
policy, admins see a unified view: "Player alice (characters: Alaric, Beatrix) did X."
With character-level auth, correlating actions across characters requires IP analysis or
other forensics.

### 3. Stronger security posture

One well-chosen password is more secure than five weak ones. Players managing multiple
character passwords often resort to patterns (`char1pass`, `char2pass`) or reuse. With
player-level auth, we can enforce password strength once and consider account-level
features like MFA in the future.

### 4. Foundation for account-level features

Player-level auth enables:

- Session management ("logout all sessions")
- Password reset via email (one email per player)
- Account lockout (affects all characters)
- Future MFA/2FA at account level

### 5. Preparation for character rostering

The future epic (holomush-gloh) will support character rostering—characters that exist
without a player owner, available for claiming. By making `player_id` nullable on
characters now, we prepare for this feature without migration complexity later.

## Alternatives Considered

_No formal alternatives section in the original ADR. See Decision and Rationale sections for the chosen approach._

## Consequences

### Positive

- Seamless character switching in web client
- Unified audit trail per player identity
- Stronger password policy enforcement
- Foundation for MFA, session management, email recovery
- Prepared for character rostering feature

### Negative

- Single password compromise affects all characters
- More complex data model (players own characters)
- Migration required from classic MUSH databases

### Neutral

- Different from traditional MUSH authentication expectations
- Telnet users see similar flow (connect → select → play)
- Character limits enforced at player level (default: 5)

## References

- Implementation: `internal/auth/player.go`, `internal/auth/character_service.go`,
  `internal/auth/auth_service.go`
- Design spec: `docs/specs/2026-01-25-auth-identity-design.md`
- Related ADR: [ADR 0001: Opaque Session Tokens](0001-opaque-session-tokens.md)
- Future epic: holomush-gloh (character rostering)
