---
title: "Commands"
---

Commands in HoloMUSH are plain words — no `@` symbols, no `+` prefixes, no sigils. Type the command followed by any arguments and hit enter. For example: `say Hello everyone` or just `look` on its own.

Type `help` in-game for a list of available commands and detailed usage.

## Communication

| Command | Usage | Description |
|---------|-------|-------------|
| say | `say Hello everyone` | Speak aloud to everyone in your location |
| pose | `pose waves cheerfully.` | Describe your character's action in third person |
| whisper | `whisper Alice=Something secret` | Send a private message to someone in the same location |
| page | `page Bob=Hey, are you free?` | Send a private message to anyone in the game |

## Navigation

| Command | Usage | Description |
|---------|-------|-------------|
| look | `look` | See the description of your current location, who's here, and available exits |

To move, type the name of an exit (or its alias). For example, if `look` shows a "north" exit, type `north` or `n` to go through it. Exit names are whatever the builder chose — cardinal directions are common but not required. The available exits depend on how the world was built.

## Information

| Command | Usage | Description |
|---------|-------|-------------|
| describe | `describe me=Tall with dark hair.` | Set a description on yourself or an object |
| who | `who` | See who's currently connected to the game |
| help | `help` | View available help topics |

## Session

| Command | Usage | Description |
|---------|-------|-------------|
| connect | `connect username password` | Log in with your account credentials |
| play | `play CharName` | Switch to one of your characters |
| create | `create CharName` | Create a new character on your account |
| quit | `quit` | Disconnect from the game |

## Scenes

| Command | Usage | Description |
|---------|-------|-------------|
| scene focus | `scene focus #<id>` | Focus your current connection on a specific scene; output from that scene appears in your terminal |
| scene grid | `scene grid` | Return your current connection to the grid (default view); clears any scene focus |
| scene list | `scene list` | List the scenes you are in. `[focused]` means at least one of your active connections is focused on that scene; `[background]` means no connection is. |

## Aliases

A few common commands have shorthand aliases so you can type faster:

| Alias | Expands to | Example |
|-------|-----------|---------|
| `"` | say | `"Hello!` becomes `say Hello!` |
| `:` | pose (with a space) | `:waves.` becomes `pose waves.` (shows as "Alice waves.") |
| `;` | pose (no space) | `;'s eyes widen.` becomes `pose 's eyes widen.` (shows as "Alice's eyes widen.") |

Use `;` when the action starts with punctuation that belongs directly after your character's name — possessives, contractions. `:` adds a space, `;` does not.
