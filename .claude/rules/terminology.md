---
paths:
  - "**/*.go"
  - "**/*.md"
  - "**/*.proto"
  - "**/*.lua"
  - "**/*.svelte"
  - "**/*.ts"
---

# HoloMUSH Terminology

Consistent terminology prevents confusion. Use these terms exactly.

| Correct term     | Incorrect / ambiguous | Notes |
|------------------|-----------------------|-------|
| **location**     | room, area, zone      | A place in the world model. Event type: `location_state`. |
| **exit**         | door, path, passage   | A connection between locations. |
| **character**    | player, user, avatar  | An in-game entity controlled by a player. |
| **player**       | user, account         | The human behind one or more characters. |
| **session**      | connection            | Server-side state for a character's ongoing presence. |
| **connection**   | socket, client        | A single client attachment to a session (terminal/telnet/etc). |
| **presence**     | who's here, occupants | Active sessions at a location. Derived from session store. Queryable via `CoreService.ListFocusPresence`. |
| **grid present** | online, visible       | Character is visible on the grid (has terminal/telnet conn). |
| **scene**        | RP scene              | A structured roleplay encounter with participants. |

**MUST NOT** mix terms. `room` is never used in code, comments, types, events, or variable names. The spatial concept is always `location`.
