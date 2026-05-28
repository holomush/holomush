# Connecting

There are two ways to connect to a HoloMUSH game: a web browser or a telnet client. Both give you access to the same world — pick whichever feels more comfortable.

## Web Client

Navigate to the URL your game operator provides. The web client runs in any modern browser, including mobile. No installation needed.

## Telnet

If you prefer a traditional MU* client or a raw terminal, connect via telnet:

```text
telnet hostname 4201
```

Replace `hostname` with your game's address. The default port is 4201, but your game operator may use a different one. Dedicated MU* clients like [Mudlet](https://www.mudlet.org/), [TinTin++](https://tintin.mudhalla.net/), or [BeipMU](https://beipmu.com/) (Windows) work well too — just point them at the same host and port.

## Your First Connection

When you first connect, you'll need to create an account and a character. The game walks you through it:

1. **Create an account.** Choose a username and password. This is your login — it's separate from your character name.
2. **Create a character.** Pick a name for your in-game persona. You can have multiple characters on a single account.
3. **Enter the game.** Once your character is created, you'll land in the game's starting location.

## Example Session

### First Time

```text
> create account myuser mypassword
Account created. Welcome to HoloMUSH!
> create character Kael
Character "Kael" created.
> play Kael
Welcome, Kael!

The Nexus
A shimmering hub of interconnected pathways...
> say Hello, anyone around?
You say, "Hello, anyone around?"
```

### Returning Player

```text
> connect myuser mypassword
Welcome back!
> play Kael
Welcome, Kael!

The Nexus
A shimmering hub of interconnected pathways...
> say Hello, anyone around?
You say, "Hello, anyone around?"
> quit
Goodbye!
```

## If You Get Disconnected

Don't worry about it. HoloMUSH keeps your session alive in the background when you lose connection. You'll always need to log in again when you reconnect, but the important thing is you won't miss anything — events that happened while you were away are replayed when you come back. Your character stays in the same location, and the game catches you up.

Sessions stay alive for a configurable window after you disconnect (typically 30 minutes). After that, the session is cleaned up, but you can always start a new one.
