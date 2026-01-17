# Phase 1: Tracer Bullet Specification

**Status:** Draft
**Date:** 2026-01-17
**Version:** 0.1.0

## Overview

Phase 1 implements a thin vertical slice through the entire HoloMUSH architecture, validating core design decisions before expanding scope. The tracer bullet approach proves the event-oriented architecture works end-to-end.

## Goal

A functional telnet server where multiple users can connect, authenticate, see each other's messages, disconnect, reconnect, and see missed events replayed.

## Requirements

### MUST Have

1. **Telnet Server** - Accept telnet connections on configurable port
2. **Authentication** - Basic username/password authentication (hardcoded test users acceptable)
3. **Event System** - Events MUST be:
   - Identified by ULID
   - Persisted to PostgreSQL before acknowledgment
   - Ordered within streams
   - Replayable on reconnection
4. **Session Persistence** - Character sessions MUST persist across disconnections
5. **Event Replay** - On reconnect, client MUST receive missed events (configurable limit)
6. **Basic Commands**:
   - `connect <name> <password>` - Authenticate and enter game
   - `look` - Describe current location
   - `say <message>` - Send message to location
   - `pose <action>` - Send pose/emote to location
   - `quit` - Disconnect (session persists)
7. **WASM Plugin** - One proof-of-concept plugin that:
   - Receives events
   - Can emit responses
   - Proves wazero integration works

### SHOULD Have

1. **Graceful Shutdown** - Server SHOULD handle SIGTERM/SIGINT cleanly
2. **Configuration** - Server SHOULD read config from file/env
3. **Logging** - Structured logging with configurable level

### MUST NOT Have (Deferred)

- Web client, PWA, offline support
- Multiple locations, exits, movement
- Character creation (hardcoded test data)
- Full ABAC (simplified access control)
- TLS, HMAC signing
- Wiki, forum, character sheets
- Scenes

## Architecture

```
┌─────────────────┐
│  Telnet Client  │
└────────┬────────┘
         │ TCP (cleartext)
         ▼
┌─────────────────┐
│ Telnet Adapter  │ Translates lines ↔ events
└────────┬────────┘
         │ Internal events
         ▼
┌─────────────────┐
│   Core Engine   │ Sessions, commands, world
└────────┬────────┘
         │
    ┌────┴────┐
    ▼         ▼
┌───────┐ ┌──────────┐
│  PG   │ │  WASM    │
│ Store │ │  Plugin  │
└───────┘ └──────────┘
```

## Data Model (Phase 1)

### Tables

```sql
-- Players (accounts)
CREATE TABLE players (
    id TEXT PRIMARY KEY,  -- ULID
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Characters
CREATE TABLE characters (
    id TEXT PRIMARY KEY,  -- ULID
    player_id TEXT NOT NULL REFERENCES players(id),
    name TEXT NOT NULL,
    location_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Locations
CREATE TABLE locations (
    id TEXT PRIMARY KEY,  -- ULID
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Events
CREATE TABLE events (
    id TEXT PRIMARY KEY,  -- ULID
    stream TEXT NOT NULL,
    type TEXT NOT NULL,
    actor_kind SMALLINT NOT NULL,
    actor_id TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_events_stream_id ON events (stream, id);

-- Sessions
CREATE TABLE sessions (
    character_id TEXT PRIMARY KEY REFERENCES characters(id),
    last_event_id TEXT,  -- Per-stream tracking simplified to single cursor for Phase 1
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### Test Data

```sql
-- One test player
INSERT INTO players (id, username, password_hash)
VALUES ('01JTEST001', 'testuser', 'password');

-- One test character
INSERT INTO characters (id, player_id, name, location_id)
VALUES ('01JCHAR001', '01JTEST001', 'TestChar', '01JLOC001');

-- One test location
INSERT INTO locations (id, name, description)
VALUES ('01JLOC001', 'The Void', 'An empty expanse of nothing. This is where it all begins.');
```

## Success Criteria

1. ✓ Connect via telnet, authenticate
2. ✓ See events from other connected characters in same location
3. ✓ Disconnect and reconnect - see missed events replayed
4. ✓ WASM plugin receives events, can emit responses
5. ✓ All events persisted and queryable

## Open Questions

None - this is a minimal tracer bullet. Decisions deferred to later phases.
