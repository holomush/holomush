# Epic 5: Auth & Identity Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this
> plan task-by-task.

**Goal:** Implement secure player authentication with character selection across telnet and web
clients, including password hashing, rate limiting, and session management.

**Architecture:** Player accounts authenticate with argon2id passwords. Telnet uses connection-based
sessions; web uses signed tokens stored in database. Characters are bound to sessions after
authentication.

**Tech Stack:** Go 1.23+, PostgreSQL, pgx/v5, argon2id (golang.org/x/crypto/argon2), testify, oops
errors

**Epic:** holomush-dwk (Epic 5: Auth & Identity)

**Design Spec:** [docs/specs/2026-01-25-auth-identity-design.md](../specs/2026-01-25-auth-identity-design.md)

---

## Phase 5.1: Player Schema Updates

### Task 5.1.1: Create Auth Player Fields Migration

**Files:**

- Create: `internal/store/migrations/000009_auth_player_fields.up.sql`
- Create: `internal/store/migrations/000009_auth_player_fields.down.sql`

**Step 1: Create up migration**

Create `internal/store/migrations/000009_auth_player_fields.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Auth player fields for Epic 5
-- Adds email, rate limiting, preferences, and rostering support

-- Email and verification
ALTER TABLE players ADD COLUMN IF NOT EXISTS email TEXT;
ALTER TABLE players ADD COLUMN IF NOT EXISTS email_verified BOOLEAN NOT NULL DEFAULT FALSE;

-- Rate limiting fields
ALTER TABLE players ADD COLUMN IF NOT EXISTS failed_attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE players ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;

-- Default character preference (FK added after characters nullable player_id)
ALTER TABLE players ADD COLUMN IF NOT EXISTS default_character_id TEXT;

-- Extensible preferences
ALTER TABLE players ADD COLUMN IF NOT EXISTS preferences JSONB NOT NULL DEFAULT '{}';

-- Timestamps
ALTER TABLE players ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Unique index on email (partial - only when email is set)
CREATE UNIQUE INDEX IF NOT EXISTS idx_players_email ON players(email) WHERE email IS NOT NULL;

-- Allow rostered characters (player_id nullable for future holomush-gloh epic)
ALTER TABLE characters ALTER COLUMN player_id DROP NOT NULL;

-- Now add the FK constraint for default_character_id
ALTER TABLE players ADD CONSTRAINT fk_players_default_character
    FOREIGN KEY (default_character_id) REFERENCES characters(id) ON DELETE SET NULL;
```

**Step 2: Create down migration**

Create `internal/store/migrations/000009_auth_player_fields.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse 000009_auth_player_fields.up.sql

-- Remove FK constraint first
ALTER TABLE players DROP CONSTRAINT IF EXISTS fk_players_default_character;

-- Restore NOT NULL on characters.player_id
-- NOTE: This will fail if any characters have NULL player_id
ALTER TABLE characters ALTER COLUMN player_id SET NOT NULL;

-- Remove index
DROP INDEX IF EXISTS idx_players_email;

-- Remove columns
ALTER TABLE players DROP COLUMN IF EXISTS updated_at;
ALTER TABLE players DROP COLUMN IF EXISTS preferences;
ALTER TABLE players DROP COLUMN IF EXISTS default_character_id;
ALTER TABLE players DROP COLUMN IF EXISTS locked_until;
ALTER TABLE players DROP COLUMN IF EXISTS failed_attempts;
ALTER TABLE players DROP COLUMN IF EXISTS email_verified;
ALTER TABLE players DROP COLUMN IF EXISTS email;
```

**Step 3: Verify SQL syntax**

Run: `task lint:sql` (if available) or manual review

**Step 4: Commit**

```bash
git add internal/store/migrations/000009_auth_player_fields.*
git commit -m "feat(auth): add player auth fields migration"
```

---

### Task 5.1.2: Create Web Sessions Migration

**Files:**

- Create: `internal/store/migrations/000010_web_sessions.up.sql`
- Create: `internal/store/migrations/000010_web_sessions.down.sql`

**Step 1: Create up migration**

Create `internal/store/migrations/000010_web_sessions.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Web sessions for Epic 5
-- Database-backed sessions for web clients with signed tokens

CREATE TABLE IF NOT EXISTS web_sessions (
    id TEXT PRIMARY KEY,
    player_id TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    character_id TEXT NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    token_signature TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_web_sessions_player ON web_sessions(player_id);
CREATE INDEX IF NOT EXISTS idx_web_sessions_token ON web_sessions(token_signature);
CREATE INDEX IF NOT EXISTS idx_web_sessions_expires ON web_sessions(expires_at);
```

**Step 2: Create down migration**

Create `internal/store/migrations/000010_web_sessions.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse 000010_web_sessions.up.sql

DROP INDEX IF EXISTS idx_web_sessions_expires;
DROP INDEX IF EXISTS idx_web_sessions_token;
DROP INDEX IF EXISTS idx_web_sessions_player;
DROP TABLE IF EXISTS web_sessions;
```

**Step 3: Commit**

```bash
git add internal/store/migrations/000010_web_sessions.*
git commit -m "feat(auth): add web sessions table migration"
```

---

### Task 5.1.3: Create Password Resets Migration

**Files:**

- Create: `internal/store/migrations/000011_password_resets.up.sql`
- Create: `internal/store/migrations/000011_password_resets.down.sql`

**Step 1: Create up migration**

Create `internal/store/migrations/000011_password_resets.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Password reset tokens for Epic 5
-- Stores hashed tokens with expiry for secure password recovery

CREATE TABLE IF NOT EXISTS password_resets (
    id TEXT PRIMARY KEY,
    player_id TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_password_resets_player ON password_resets(player_id);
CREATE INDEX IF NOT EXISTS idx_password_resets_expires ON password_resets(expires_at);
```

**Step 2: Create down migration**

Create `internal/store/migrations/000011_password_resets.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse 000011_password_resets.up.sql

DROP INDEX IF EXISTS idx_password_resets_expires;
DROP INDEX IF EXISTS idx_password_resets_player;
DROP TABLE IF EXISTS password_resets;
```

**Step 3: Commit**

```bash
git add internal/store/migrations/000011_password_resets.*
git commit -m "feat(auth): add password resets table migration"
```

---

## Phase 5.2: Password Authentication

### Task 5.2.1: Implement Argon2id Password Hasher

**Files:**

- Create: `internal/auth/hasher.go`
- Test: `internal/auth/hasher_test.go`

**Step 1: Write the failing test**

Create `internal/auth/hasher_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
    "strings"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/auth"
)

func TestHashPassword(t *testing.T) {
    hasher := auth.NewArgon2idHasher()

    t.Run("produces valid hash", func(t *testing.T) {
        hash, err := hasher.Hash("password123")
        require.NoError(t, err)
        assert.True(t, strings.HasPrefix(hash, "$argon2id$"))
    })

    t.Run("different passwords produce different hashes", func(t *testing.T) {
        hash1, err := hasher.Hash("password1")
        require.NoError(t, err)
        hash2, err := hasher.Hash("password2")
        require.NoError(t, err)
        assert.NotEqual(t, hash1, hash2)
    })

    t.Run("same password produces different hashes (salt)", func(t *testing.T) {
        hash1, err := hasher.Hash("samepassword")
        require.NoError(t, err)
        hash2, err := hasher.Hash("samepassword")
        require.NoError(t, err)
        assert.NotEqual(t, hash1, hash2)
    })

    t.Run("rejects empty password", func(t *testing.T) {
        _, err := hasher.Hash("")
        assert.Error(t, err)
    })
}

func TestVerifyPassword(t *testing.T) {
    hasher := auth.NewArgon2idHasher()

    t.Run("correct password verifies", func(t *testing.T) {
        hash, err := hasher.Hash("correctpassword")
        require.NoError(t, err)

        ok, err := hasher.Verify("correctpassword", hash)
        require.NoError(t, err)
        assert.True(t, ok)
    })

    t.Run("incorrect password fails", func(t *testing.T) {
        hash, err := hasher.Hash("correctpassword")
        require.NoError(t, err)

        ok, err := hasher.Verify("wrongpassword", hash)
        require.NoError(t, err)
        assert.False(t, ok)
    })

    t.Run("invalid hash format returns error", func(t *testing.T) {
        _, err := hasher.Verify("password", "not-a-valid-hash")
        assert.Error(t, err)
    })
}

func TestVerifyBcryptUpgrade(t *testing.T) {
    hasher := auth.NewArgon2idHasher()

    // This is the bcrypt hash from test data in 000001_initial.up.sql
    bcryptHash := "$2a$10$N9qo8uLOickgx2ZMRZoMye"

    t.Run("detects bcrypt hash needing upgrade", func(t *testing.T) {
        needsUpgrade := hasher.NeedsUpgrade(bcryptHash)
        assert.True(t, needsUpgrade)
    })

    t.Run("argon2id hash does not need upgrade", func(t *testing.T) {
        hash, err := hasher.Hash("password")
        require.NoError(t, err)
        assert.False(t, hasher.NeedsUpgrade(hash))
    })
}
```

**Step 2: Run test to verify it fails**

Run: `task test -- -run TestHashPassword ./internal/auth/...`

Expected: FAIL - package does not exist

**Step 3: Write minimal implementation**

Create `internal/auth/hasher.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package auth provides authentication primitives for HoloMUSH.
package auth

import (
    "crypto/rand"
    "crypto/subtle"
    "encoding/base64"
    "fmt"
    "strings"

    "github.com/samber/oops"
    "golang.org/x/crypto/argon2"
)

// OWASP-recommended argon2id parameters.
const (
    argon2Time    = 1         // iterations
    argon2Memory  = 64 * 1024 // 64 MB
    argon2Threads = 4         // parallelism
    argon2SaltLen = 16        // salt length in bytes
    argon2KeyLen  = 32        // output length in bytes
)

// ErrEmptyPassword is returned when attempting to hash an empty password.
var ErrEmptyPassword = oops.Code("AUTH_EMPTY_PASSWORD").Errorf("password cannot be empty")

// PasswordHasher provides password hashing and verification.
type PasswordHasher interface {
    // Hash produces an argon2id hash of the password.
    Hash(password string) (string, error)

    // Verify checks if the password matches the hash.
    // Returns (true, nil) on match, (false, nil) on mismatch, or error on invalid hash.
    Verify(password, hash string) (bool, error)

    // NeedsUpgrade returns true if the hash should be upgraded to argon2id.
    NeedsUpgrade(hash string) bool
}

// Argon2idHasher implements PasswordHasher using argon2id.
type Argon2idHasher struct{}

// NewArgon2idHasher creates a new Argon2idHasher.
func NewArgon2idHasher() *Argon2idHasher {
    return &Argon2idHasher{}
}

// Hash produces an argon2id hash of the password.
func (h *Argon2idHasher) Hash(password string) (string, error) {
    if password == "" {
        return "", ErrEmptyPassword
    }

    // Generate random salt
    salt := make([]byte, argon2SaltLen)
    if _, err := rand.Read(salt); err != nil {
        return "", oops.Code("AUTH_SALT_FAILED").Wrap(err)
    }

    // Compute hash
    hash := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)

    // Encode as PHC string format
    // $argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>
    encoded := fmt.Sprintf(
        "$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
        argon2.Version,
        argon2Memory,
        argon2Time,
        argon2Threads,
        base64.RawStdEncoding.EncodeToString(salt),
        base64.RawStdEncoding.EncodeToString(hash),
    )

    return encoded, nil
}

// Verify checks if the password matches the hash.
func (h *Argon2idHasher) Verify(password, encodedHash string) (bool, error) {
    // Parse the hash
    parts := strings.Split(encodedHash, "$")
    if len(parts) != 6 {
        return false, oops.Code("AUTH_INVALID_HASH").Errorf("invalid hash format")
    }

    if parts[1] != "argon2id" {
        return false, oops.Code("AUTH_INVALID_HASH").Errorf("unsupported hash algorithm: %s", parts[1])
    }

    var version int
    if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
        return false, oops.Code("AUTH_INVALID_HASH").Wrap(err)
    }

    var memory, time, threads uint32
    if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
        return false, oops.Code("AUTH_INVALID_HASH").Wrap(err)
    }

    salt, err := base64.RawStdEncoding.DecodeString(parts[4])
    if err != nil {
        return false, oops.Code("AUTH_INVALID_HASH").Wrap(err)
    }

    expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
    if err != nil {
        return false, oops.Code("AUTH_INVALID_HASH").Wrap(err)
    }

    // Compute hash with same parameters
    computedHash := argon2.IDKey([]byte(password), salt, time, memory, uint8(threads), uint32(len(expectedHash)))

    // Constant-time comparison
    if subtle.ConstantTimeCompare(computedHash, expectedHash) == 1 {
        return true, nil
    }

    return false, nil
}

// NeedsUpgrade returns true if the hash is not argon2id (e.g., bcrypt).
func (h *Argon2idHasher) NeedsUpgrade(hash string) bool {
    return !strings.HasPrefix(hash, "$argon2id$")
}
```

**Step 4: Run test to verify it passes**

Run: `task test -- -run TestHashPassword ./internal/auth/... && task test -- -run TestVerify ./internal/auth/...`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/hasher.go internal/auth/hasher_test.go
git commit -m "feat(auth): implement argon2id password hasher"
```

---

### Task 5.2.2: Implement Rate Limiter

**Files:**

- Create: `internal/auth/ratelimit.go`
- Test: `internal/auth/ratelimit_test.go`

**Step 1: Write the failing test**

Create `internal/auth/ratelimit_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
    "testing"
    "time"

    "github.com/stretchr/testify/assert"

    "github.com/holomush/holomush/internal/auth"
)

func TestRateLimiter_CheckFailures(t *testing.T) {
    t.Run("no failures returns no delay", func(t *testing.T) {
        result := auth.CheckFailures(0, nil)
        assert.Zero(t, result.Delay)
        assert.False(t, result.RequiresCaptcha)
        assert.False(t, result.IsLockedOut)
    })

    t.Run("1-3 failures returns progressive delay", func(t *testing.T) {
        result1 := auth.CheckFailures(1, nil)
        assert.Equal(t, time.Second, result1.Delay)
        assert.False(t, result1.RequiresCaptcha)

        result2 := auth.CheckFailures(2, nil)
        assert.Equal(t, 2*time.Second, result2.Delay)

        result3 := auth.CheckFailures(3, nil)
        assert.Equal(t, 4*time.Second, result3.Delay)
    })

    t.Run("4-6 failures requires captcha (web)", func(t *testing.T) {
        result4 := auth.CheckFailures(4, nil)
        assert.True(t, result4.RequiresCaptcha)
        assert.Equal(t, 8*time.Second, result4.Delay)

        result6 := auth.CheckFailures(6, nil)
        assert.True(t, result6.RequiresCaptcha)
        assert.Equal(t, 32*time.Second, result6.Delay)
    })

    t.Run("7+ failures causes lockout", func(t *testing.T) {
        result := auth.CheckFailures(7, nil)
        assert.True(t, result.IsLockedOut)
        assert.Equal(t, auth.LockoutDuration, result.LockoutRemaining)
    })
}

func TestRateLimiter_IsLockedOut(t *testing.T) {
    now := time.Now()

    t.Run("nil locked_until means not locked", func(t *testing.T) {
        assert.False(t, auth.IsLockedOut(nil))
    })

    t.Run("past locked_until means not locked", func(t *testing.T) {
        past := now.Add(-time.Hour)
        assert.False(t, auth.IsLockedOut(&past))
    })

    t.Run("future locked_until means locked", func(t *testing.T) {
        future := now.Add(time.Hour)
        assert.True(t, auth.IsLockedOut(&future))
    })
}

func TestRateLimiter_ComputeLockoutTime(t *testing.T) {
    t.Run("7 failures returns lockout time", func(t *testing.T) {
        lockout := auth.ComputeLockoutTime(7)
        assert.NotNil(t, lockout)
        assert.True(t, lockout.After(time.Now()))
    })

    t.Run("less than 7 failures returns nil", func(t *testing.T) {
        assert.Nil(t, auth.ComputeLockoutTime(6))
    })
}
```

**Step 2: Run test to verify it fails**

Run: `task test -- -run TestRateLimiter ./internal/auth/...`

Expected: FAIL - functions not defined

**Step 3: Write minimal implementation**

Create `internal/auth/ratelimit.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
    "time"
)

// Rate limiting configuration.
const (
    // LockoutDuration is the time a user is locked out after too many failures.
    LockoutDuration = 15 * time.Minute

    // LockoutThreshold is the number of failures that triggers a lockout.
    LockoutThreshold = 7

    // CaptchaThreshold is the number of failures that triggers CAPTCHA requirement (web only).
    CaptchaThreshold = 4
)

// RateLimitResult contains the result of a rate limit check.
type RateLimitResult struct {
    // Delay is the time to wait before allowing another attempt.
    Delay time.Duration

    // RequiresCaptcha indicates the web client should require CAPTCHA.
    RequiresCaptcha bool

    // IsLockedOut indicates the account is temporarily locked.
    IsLockedOut bool

    // LockoutRemaining is the time until the lockout expires.
    LockoutRemaining time.Duration
}

// CheckFailures evaluates the rate limit state based on failure count.
// lockedUntil is the current lockout timestamp (nil if not locked).
func CheckFailures(failures int, lockedUntil *time.Time) RateLimitResult {
    result := RateLimitResult{}

    // Check existing lockout first
    if IsLockedOut(lockedUntil) {
        result.IsLockedOut = true
        result.LockoutRemaining = time.Until(*lockedUntil)
        return result
    }

    // Progressive delay: 2^(failures-1) seconds, max 32s before lockout
    if failures > 0 && failures < LockoutThreshold {
        result.Delay = time.Duration(1<<(failures-1)) * time.Second
        if result.Delay > 32*time.Second {
            result.Delay = 32 * time.Second
        }
    }

    // CAPTCHA required at 4+ failures (for web clients)
    if failures >= CaptchaThreshold && failures < LockoutThreshold {
        result.RequiresCaptcha = true
    }

    // Lockout at 7+ failures
    if failures >= LockoutThreshold {
        result.IsLockedOut = true
        result.LockoutRemaining = LockoutDuration
    }

    return result
}

// IsLockedOut returns true if the lockout time is in the future.
func IsLockedOut(lockedUntil *time.Time) bool {
    return lockedUntil != nil && lockedUntil.After(time.Now())
}

// ComputeLockoutTime returns the lockout timestamp for the given failure count.
// Returns nil if failures < LockoutThreshold.
func ComputeLockoutTime(failures int) *time.Time {
    if failures < LockoutThreshold {
        return nil
    }
    lockout := time.Now().Add(LockoutDuration)
    return &lockout
}

// ResetOnSuccess returns the values to set after a successful login.
// Returns 0 for failed_attempts and nil for locked_until.
func ResetOnSuccess() (int, *time.Time) {
    return 0, nil
}
```

**Step 4: Run test to verify it passes**

Run: `task test -- -run TestRateLimiter ./internal/auth/...`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/ratelimit.go internal/auth/ratelimit_test.go
git commit -m "feat(auth): implement rate limiting with progressive delays"
```

---

### Task 5.2.3: Implement Player Domain Type

**Files:**

- Create: `internal/auth/player.go`
- Test: `internal/auth/player_test.go`

**Step 1: Write the failing test**

Create `internal/auth/player_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
    "testing"
    "time"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"

    "github.com/holomush/holomush/internal/auth"
)

func TestPlayer_IsLocked(t *testing.T) {
    t.Run("no lockout", func(t *testing.T) {
        p := &auth.Player{}
        assert.False(t, p.IsLocked())
    })

    t.Run("future lockout", func(t *testing.T) {
        future := time.Now().Add(time.Hour)
        p := &auth.Player{LockedUntil: &future}
        assert.True(t, p.IsLocked())
    })

    t.Run("past lockout", func(t *testing.T) {
        past := time.Now().Add(-time.Hour)
        p := &auth.Player{LockedUntil: &past}
        assert.False(t, p.IsLocked())
    })
}

func TestPlayer_RecordFailure(t *testing.T) {
    t.Run("increments counter", func(t *testing.T) {
        p := &auth.Player{FailedAttempts: 0}
        p.RecordFailure()
        assert.Equal(t, 1, p.FailedAttempts)
    })

    t.Run("sets lockout at threshold", func(t *testing.T) {
        p := &auth.Player{FailedAttempts: 6}
        p.RecordFailure()
        assert.Equal(t, 7, p.FailedAttempts)
        assert.NotNil(t, p.LockedUntil)
        assert.True(t, p.LockedUntil.After(time.Now()))
    })
}

func TestPlayer_RecordSuccess(t *testing.T) {
    t.Run("resets failures and lockout", func(t *testing.T) {
        future := time.Now().Add(time.Hour)
        p := &auth.Player{
            FailedAttempts: 5,
            LockedUntil:    &future,
        }
        p.RecordSuccess()
        assert.Equal(t, 0, p.FailedAttempts)
        assert.Nil(t, p.LockedUntil)
    })
}

func TestPlayerPreferences(t *testing.T) {
    t.Run("default values", func(t *testing.T) {
        prefs := auth.PlayerPreferences{}
        assert.False(t, prefs.AutoLogin)
        assert.Equal(t, 0, prefs.MaxCharacters) // 0 means use default
    })

    t.Run("effective max characters uses default when zero", func(t *testing.T) {
        prefs := auth.PlayerPreferences{}
        assert.Equal(t, auth.DefaultMaxCharacters, prefs.EffectiveMaxCharacters())

        prefs.MaxCharacters = 10
        assert.Equal(t, 10, prefs.EffectiveMaxCharacters())
    })
}

func TestValidateUsername(t *testing.T) {
    tests := []struct {
        name     string
        username string
        wantErr  bool
    }{
        {"valid", "testuser", false},
        {"valid with numbers", "user123", false},
        {"valid with underscore", "test_user", false},
        {"too short", "ab", true},
        {"too long", "abcdefghijklmnopqrstuvwxyz12345", true}, // 31 chars
        {"empty", "", true},
        {"spaces", "test user", true},
        {"special chars", "test@user", true},
        {"starts with number", "123user", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := auth.ValidateUsername(tt.username)
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

**Step 2: Run test to verify it fails**

Run: `task test -- -run TestPlayer ./internal/auth/...`

Expected: FAIL - Player type not defined

**Step 3: Write minimal implementation**

Create `internal/auth/player.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
    "regexp"
    "time"

    "github.com/oklog/ulid/v2"
    "github.com/samber/oops"
)

// DefaultMaxCharacters is the default character limit per player.
const DefaultMaxCharacters = 5

// Username validation constraints.
const (
    MinUsernameLength = 3
    MaxUsernameLength = 30
)

var usernameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// Player represents a player account.
type Player struct {
    ID                 ulid.ULID
    Username           string
    PasswordHash       string
    Email              *string
    EmailVerified      bool
    FailedAttempts     int
    LockedUntil        *time.Time
    DefaultCharacterID *ulid.ULID
    Preferences        PlayerPreferences
    CreatedAt          time.Time
    UpdatedAt          time.Time
}

// PlayerPreferences contains player-specific settings.
type PlayerPreferences struct {
    AutoLogin     bool   `json:"auto_login,omitempty"`
    MaxCharacters int    `json:"max_characters,omitempty"`
    Theme         string `json:"theme,omitempty"`
}

// EffectiveMaxCharacters returns the character limit, using default if not set.
func (p PlayerPreferences) EffectiveMaxCharacters() int {
    if p.MaxCharacters <= 0 {
        return DefaultMaxCharacters
    }
    return p.MaxCharacters
}

// IsLocked returns true if the player is currently locked out.
func (p *Player) IsLocked() bool {
    return IsLockedOut(p.LockedUntil)
}

// RecordFailure increments the failure counter and sets lockout if threshold reached.
func (p *Player) RecordFailure() {
    p.FailedAttempts++
    p.LockedUntil = ComputeLockoutTime(p.FailedAttempts)
    p.UpdatedAt = time.Now()
}

// RecordSuccess resets failure counter and lockout.
func (p *Player) RecordSuccess() {
    p.FailedAttempts = 0
    p.LockedUntil = nil
    p.UpdatedAt = time.Now()
}

// ValidateUsername validates a username against rules.
func ValidateUsername(username string) error {
    if username == "" {
        return oops.Code("AUTH_INVALID_USERNAME").Errorf("username cannot be empty")
    }
    if len(username) < MinUsernameLength {
        return oops.Code("AUTH_INVALID_USERNAME").
            With("min", MinUsernameLength).
            Errorf("username must be at least %d characters", MinUsernameLength)
    }
    if len(username) > MaxUsernameLength {
        return oops.Code("AUTH_INVALID_USERNAME").
            With("max", MaxUsernameLength).
            Errorf("username must be at most %d characters", MaxUsernameLength)
    }
    if !usernameRegex.MatchString(username) {
        return oops.Code("AUTH_INVALID_USERNAME").
            Errorf("username must start with a letter and contain only letters, numbers, and underscores")
    }
    return nil
}
```

**Step 4: Run test to verify it passes**

Run: `task test -- -run TestPlayer ./internal/auth/... && task test -- -run TestValidateUsername ./internal/auth/...`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/player.go internal/auth/player_test.go
git commit -m "feat(auth): implement Player domain type with preferences"
```

---

### Task 5.2.4: Implement Player Repository Interface and PostgreSQL Implementation

**Files:**

- Create: `internal/auth/repository.go`
- Create: `internal/auth/postgres/player_repo.go`
- Test: `internal/auth/postgres/player_repo_test.go`

**Step 1: Create repository interface**

Create `internal/auth/repository.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
    "context"

    "github.com/oklog/ulid/v2"
)

// PlayerRepository manages player persistence.
type PlayerRepository interface {
    // Get retrieves a player by ID.
    Get(ctx context.Context, id ulid.ULID) (*Player, error)

    // GetByUsername retrieves a player by username (case-insensitive).
    GetByUsername(ctx context.Context, username string) (*Player, error)

    // GetByEmail retrieves a player by email (case-insensitive).
    GetByEmail(ctx context.Context, email string) (*Player, error)

    // Create persists a new player.
    Create(ctx context.Context, player *Player) error

    // Update modifies an existing player.
    Update(ctx context.Context, player *Player) error

    // UpdatePassword changes a player's password hash.
    UpdatePassword(ctx context.Context, id ulid.ULID, passwordHash string) error

    // RecordLoginFailure increments failure count and sets lockout if needed.
    RecordLoginFailure(ctx context.Context, id ulid.ULID) error

    // ClearLoginFailures resets failure count and lockout on successful login.
    ClearLoginFailures(ctx context.Context, id ulid.ULID) error
}
```

**Step 2: Write the failing test**

Create `internal/auth/postgres/player_repo_test.go` with integration tests.

**Step 3: Write PostgreSQL implementation**

Create `internal/auth/postgres/player_repo.go` following the pattern from
`internal/world/postgres/location_repo.go`.

**Step 4: Run tests**

Run: `task test -- -run TestPlayerRepository ./internal/auth/postgres/...`

**Step 5: Commit**

```bash
git add internal/auth/repository.go internal/auth/postgres/
git commit -m "feat(auth): implement PostgreSQL PlayerRepository"
```

---

## Phase 5.3: Character Creation

### Task 5.3.1: Implement Character Name Validation

**Files:**

- Create: `internal/auth/character_validation.go`
- Test: `internal/auth/character_validation_test.go`

**Step 1: Write the failing test**

Create `internal/auth/character_validation_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
    "testing"

    "github.com/stretchr/testify/assert"

    "github.com/holomush/holomush/internal/auth"
)

func TestValidateCharacterName(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr bool
    }{
        {"valid simple", "Alaric", false},
        {"valid two words", "John Smith", false},
        {"valid lowercase normalizes", "alaric", false},
        {"too short", "A", true},
        {"too long", "Abcdefghijklmnopqrstuvwxyz1234567", true}, // 33 chars
        {"numbers not allowed", "Alaric123", true},
        {"special chars not allowed", "Alaric!", true},
        {"leading space", " Alaric", true},
        {"trailing space", "Alaric ", true},
        {"double space", "John  Smith", true},
        {"empty", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := auth.ValidateCharacterName(tt.input)
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}

func TestNormalizeCharacterName(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"already correct", "Alaric", "Alaric"},
        {"lowercase to title", "alaric", "Alaric"},
        {"all caps to title", "ALARIC", "Alaric"},
        {"two words", "john smith", "John Smith"},
        {"mixed case", "jOhN sMiTh", "John Smith"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.expected, auth.NormalizeCharacterName(tt.input))
        })
    }
}
```

**Step 2: Run test to verify it fails**

Run: `task test -- -run TestValidateCharacterName ./internal/auth/...`

Expected: FAIL - functions not defined

**Step 3: Write minimal implementation**

Create `internal/auth/character_validation.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
    "regexp"
    "strings"
    "unicode"

    "github.com/samber/oops"
)

// Character name validation constraints.
const (
    MinCharacterNameLength = 2
    MaxCharacterNameLength = 32
)

var (
    // Only letters and single spaces between words
    characterNameRegex = regexp.MustCompile(`^[a-zA-Z]+( [a-zA-Z]+)*$`)
)

// ValidateCharacterName validates a character name against rules.
func ValidateCharacterName(name string) error {
    if name == "" {
        return oops.Code("AUTH_INVALID_CHARACTER_NAME").Errorf("character name cannot be empty")
    }

    // Check for leading/trailing whitespace
    if name != strings.TrimSpace(name) {
        return oops.Code("AUTH_INVALID_CHARACTER_NAME").
            Errorf("character name cannot have leading or trailing spaces")
    }

    // Check for double spaces
    if strings.Contains(name, "  ") {
        return oops.Code("AUTH_INVALID_CHARACTER_NAME").
            Errorf("character name cannot have consecutive spaces")
    }

    if len(name) < MinCharacterNameLength {
        return oops.Code("AUTH_INVALID_CHARACTER_NAME").
            With("min", MinCharacterNameLength).
            Errorf("character name must be at least %d characters", MinCharacterNameLength)
    }

    if len(name) > MaxCharacterNameLength {
        return oops.Code("AUTH_INVALID_CHARACTER_NAME").
            With("max", MaxCharacterNameLength).
            Errorf("character name must be at most %d characters", MaxCharacterNameLength)
    }

    if !characterNameRegex.MatchString(name) {
        return oops.Code("AUTH_INVALID_CHARACTER_NAME").
            Errorf("character name must contain only letters and spaces")
    }

    return nil
}

// NormalizeCharacterName converts a character name to Initial Caps format.
// "alaric" -> "Alaric", "john smith" -> "John Smith"
func NormalizeCharacterName(name string) string {
    words := strings.Fields(name)
    for i, word := range words {
        if len(word) > 0 {
            runes := []rune(strings.ToLower(word))
            runes[0] = unicode.ToUpper(runes[0])
            words[i] = string(runes)
        }
    }
    return strings.Join(words, " ")
}
```

**Step 4: Run test to verify it passes**

Run: `task test -- -run TestValidateCharacterName ./internal/auth/... && task test -- -run TestNormalizeCharacterName ./internal/auth/...`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/character_validation.go internal/auth/character_validation_test.go
git commit -m "feat(auth): implement character name validation and normalization"
```

---

### Task 5.3.2: Implement Character Creation Service

**Files:**

- Create: `internal/auth/character_service.go`
- Test: `internal/auth/character_service_test.go`

**Step 1: Write failing tests for character creation**

Tests should cover:

- Name validation
- Name normalization
- Uniqueness check (case-insensitive)
- Character limit per player
- Initial placement in seeded first room

**Step 2: Implement CharacterService**

The service should:

- Validate and normalize the character name
- Check name uniqueness (using CharacterRepository)
- Check player's character limit
- Get the starting room (first seeded location)
- Create the character with proper containment

**Step 3: Commit**

```bash
git add internal/auth/character_service.go internal/auth/character_service_test.go
git commit -m "feat(auth): implement character creation service"
```

---

## Phase 5.4: Session Binding

### Task 5.4.1: Implement Web Session Token

**Files:**

- Create: `internal/auth/token.go`
- Test: `internal/auth/token_test.go`

**Step 1: Write the failing test**

Create `internal/auth/token_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
    "testing"
    "time"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/auth"
)

func TestToken_SignAndVerify(t *testing.T) {
    secret := []byte("test-secret-key-32-bytes-long!!!")
    signer := auth.NewTokenSigner(secret)

    playerID := ulid.Make()
    characterID := ulid.Make()

    t.Run("creates valid token", func(t *testing.T) {
        token, err := signer.Sign(playerID, characterID, 24*time.Hour)
        require.NoError(t, err)
        assert.NotEmpty(t, token)
        assert.Contains(t, token, ".")
    })

    t.Run("verifies valid token", func(t *testing.T) {
        token, err := signer.Sign(playerID, characterID, 24*time.Hour)
        require.NoError(t, err)

        claims, err := signer.Verify(token)
        require.NoError(t, err)
        assert.Equal(t, playerID, claims.PlayerID)
        assert.Equal(t, characterID, claims.CharacterID)
    })

    t.Run("rejects expired token", func(t *testing.T) {
        token, err := signer.Sign(playerID, characterID, -time.Hour) // Already expired
        require.NoError(t, err)

        _, err = signer.Verify(token)
        assert.Error(t, err)
    })

    t.Run("rejects tampered token", func(t *testing.T) {
        token, err := signer.Sign(playerID, characterID, 24*time.Hour)
        require.NoError(t, err)

        // Tamper with the token
        tampered := token[:len(token)-5] + "XXXXX"

        _, err = signer.Verify(tampered)
        assert.Error(t, err)
    })

    t.Run("rejects token signed with different secret", func(t *testing.T) {
        otherSecret := []byte("other-secret-key-32-bytes-long!!")
        otherSigner := auth.NewTokenSigner(otherSecret)

        token, err := otherSigner.Sign(playerID, characterID, 24*time.Hour)
        require.NoError(t, err)

        _, err = signer.Verify(token)
        assert.Error(t, err)
    })
}

func TestToken_Signature(t *testing.T) {
    secret := []byte("test-secret-key-32-bytes-long!!!")
    signer := auth.NewTokenSigner(secret)

    playerID := ulid.Make()
    characterID := ulid.Make()

    t.Run("extracts signature from token", func(t *testing.T) {
        token, err := signer.Sign(playerID, characterID, 24*time.Hour)
        require.NoError(t, err)

        sig := auth.ExtractSignature(token)
        assert.NotEmpty(t, sig)
    })
}
```

**Step 2: Run test to verify it fails**

Run: `task test -- -run TestToken ./internal/auth/...`

Expected: FAIL - TokenSigner not defined

**Step 3: Write minimal implementation**

Create `internal/auth/token.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "strings"
    "time"

    "github.com/oklog/ulid/v2"
    "github.com/samber/oops"
)

// Token expiry defaults.
const (
    DefaultTokenExpiry = 30 * 24 * time.Hour // 30 days
)

// TokenClaims contains the data embedded in a session token.
type TokenClaims struct {
    PlayerID    ulid.ULID `json:"pid"`
    CharacterID ulid.ULID `json:"cid"`
    IssuedAt    time.Time `json:"iat"`
    ExpiresAt   time.Time `json:"exp"`
}

// TokenSigner creates and verifies HMAC-SHA256 signed tokens.
type TokenSigner struct {
    secret []byte
}

// NewTokenSigner creates a new TokenSigner with the given secret.
// The secret should be at least 32 bytes.
func NewTokenSigner(secret []byte) *TokenSigner {
    return &TokenSigner{secret: secret}
}

// Sign creates a signed token with the given claims.
func (s *TokenSigner) Sign(playerID, characterID ulid.ULID, expiry time.Duration) (string, error) {
    now := time.Now()
    claims := TokenClaims{
        PlayerID:    playerID,
        CharacterID: characterID,
        IssuedAt:    now,
        ExpiresAt:   now.Add(expiry),
    }

    payload, err := json.Marshal(claims)
    if err != nil {
        return "", oops.Code("TOKEN_MARSHAL_FAILED").Wrap(err)
    }

    payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
    signature := s.computeSignature(payloadB64)

    return payloadB64 + "." + signature, nil
}

// Verify validates a token and returns the claims.
func (s *TokenSigner) Verify(token string) (*TokenClaims, error) {
    parts := strings.Split(token, ".")
    if len(parts) != 2 {
        return nil, oops.Code("TOKEN_INVALID_FORMAT").Errorf("invalid token format")
    }

    payloadB64, providedSig := parts[0], parts[1]

    // Verify signature
    expectedSig := s.computeSignature(payloadB64)
    if !hmac.Equal([]byte(providedSig), []byte(expectedSig)) {
        return nil, oops.Code("TOKEN_INVALID_SIGNATURE").Errorf("invalid token signature")
    }

    // Decode payload
    payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
    if err != nil {
        return nil, oops.Code("TOKEN_DECODE_FAILED").Wrap(err)
    }

    var claims TokenClaims
    if err := json.Unmarshal(payload, &claims); err != nil {
        return nil, oops.Code("TOKEN_UNMARSHAL_FAILED").Wrap(err)
    }

    // Check expiry
    if time.Now().After(claims.ExpiresAt) {
        return nil, oops.Code("TOKEN_EXPIRED").Errorf("token has expired")
    }

    return &claims, nil
}

func (s *TokenSigner) computeSignature(payload string) string {
    h := hmac.New(sha256.New, s.secret)
    h.Write([]byte(payload))
    return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// ExtractSignature extracts the signature portion from a token.
// This is stored in the database to allow session invalidation.
func ExtractSignature(token string) string {
    parts := strings.Split(token, ".")
    if len(parts) != 2 {
        return ""
    }
    return parts[1]
}
```

**Step 4: Run test to verify it passes**

Run: `task test -- -run TestToken ./internal/auth/...`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/token.go internal/auth/token_test.go
git commit -m "feat(auth): implement HMAC-SHA256 session tokens"
```

---

### Task 5.4.2: Implement Web Session Repository

**Files:**

- Create: `internal/auth/session.go`
- Create: `internal/auth/postgres/session_repo.go`
- Test: `internal/auth/postgres/session_repo_test.go`

**Step 1: Define WebSession domain type and repository interface**

Create `internal/auth/session.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
    "context"
    "time"

    "github.com/oklog/ulid/v2"
)

// WebSession represents a web client session.
type WebSession struct {
    ID             ulid.ULID
    PlayerID       ulid.ULID
    CharacterID    ulid.ULID
    TokenSignature string
    CreatedAt      time.Time
    ExpiresAt      time.Time
    LastActiveAt   time.Time
}

// IsExpired returns true if the session has expired.
func (s *WebSession) IsExpired() bool {
    return time.Now().After(s.ExpiresAt)
}

// WebSessionRepository manages web session persistence.
type WebSessionRepository interface {
    // Get retrieves a session by ID.
    Get(ctx context.Context, id ulid.ULID) (*WebSession, error)

    // GetBySignature retrieves a session by token signature.
    GetBySignature(ctx context.Context, signature string) (*WebSession, error)

    // Create persists a new session.
    Create(ctx context.Context, session *WebSession) error

    // UpdateLastActive updates the last_active_at timestamp.
    UpdateLastActive(ctx context.Context, id ulid.ULID) error

    // Delete removes a session.
    Delete(ctx context.Context, id ulid.ULID) error

    // DeleteByPlayer removes all sessions for a player.
    DeleteByPlayer(ctx context.Context, playerID ulid.ULID) error

    // DeleteExpired removes all expired sessions.
    DeleteExpired(ctx context.Context) (int64, error)
}
```

**Step 2: Implement PostgreSQL session repository**

**Step 3: Commit**

```bash
git add internal/auth/session.go internal/auth/postgres/session_repo.go internal/auth/postgres/session_repo_test.go
git commit -m "feat(auth): implement web session repository"
```

---

### Task 5.4.3: Implement Authentication Service

**Files:**

- Create: `internal/auth/service.go`
- Test: `internal/auth/service_test.go`

**Step 1: Define AuthService**

The AuthService orchestrates:

- Player authentication (username + password)
- Password verification with bcrypt upgrade path
- Rate limiting enforcement
- Session creation (web) / connection binding (telnet)
- Logout / session invalidation

**Step 2: Write tests**

Tests should cover:

- Successful login (single character, auto-enter)
- Successful login (multiple characters, show list)
- Login with default character preference
- Failed login (wrong password, increments counter)
- Login blocked by rate limit
- Login blocked by lockout
- Bcrypt to argon2id upgrade on successful login
- Logout invalidates session

**Step 3: Implement AuthService**

**Step 4: Commit**

```bash
git add internal/auth/service.go internal/auth/service_test.go
git commit -m "feat(auth): implement authentication service"
```

---

### Task 5.4.4: Implement Telnet Auth Handler

**Files:**

- Modify: `internal/telnet/handler.go`
- Test: `internal/telnet/handler_test.go`

**Step 1: Add connect command handler**

Add handler for `connect <username> <password>` command that:

- Parses username and password from input
- Calls AuthService.Authenticate
- Handles rate limiting (shows delay message)
- Shows character list or auto-enters world
- Updates connection state to authenticated

**Step 2: Add create command handler**

Add handler for `CREATE <name>` command that:

- Requires authenticated player
- Calls CharacterService.Create
- Enters world as new character

**Step 3: Add play command handler**

Add handler for `PLAY <name|number>` command that:

- Requires authenticated player
- Resolves character by name or index
- Binds character to connection
- Enters world

**Step 4: Commit**

```bash
git add internal/telnet/handler.go internal/telnet/handler_test.go
git commit -m "feat(telnet): implement auth commands (connect, create, play)"
```

---

## Phase 5.5: Password Reset

### Task 5.5.1: Implement Password Reset Token

**Files:**

- Create: `internal/auth/reset.go`
- Test: `internal/auth/reset_test.go`

**Step 1: Write the failing test**

Create `internal/auth/reset_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
    "testing"
    "time"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/auth"
)

func TestPasswordResetToken(t *testing.T) {
    t.Run("generates secure token", func(t *testing.T) {
        token, hash, err := auth.GenerateResetToken()
        require.NoError(t, err)
        assert.Len(t, token, 64) // 32 bytes hex-encoded
        assert.NotEmpty(t, hash)
        assert.NotEqual(t, token, hash)
    })

    t.Run("verifies correct token", func(t *testing.T) {
        token, hash, err := auth.GenerateResetToken()
        require.NoError(t, err)

        assert.True(t, auth.VerifyResetToken(token, hash))
    })

    t.Run("rejects incorrect token", func(t *testing.T) {
        _, hash, err := auth.GenerateResetToken()
        require.NoError(t, err)

        assert.False(t, auth.VerifyResetToken("wrongtoken", hash))
    })
}

func TestPasswordReset_IsExpired(t *testing.T) {
    playerID := ulid.Make()

    t.Run("not expired", func(t *testing.T) {
        reset := &auth.PasswordReset{
            PlayerID:  playerID,
            ExpiresAt: time.Now().Add(time.Hour),
        }
        assert.False(t, reset.IsExpired())
    })

    t.Run("expired", func(t *testing.T) {
        reset := &auth.PasswordReset{
            PlayerID:  playerID,
            ExpiresAt: time.Now().Add(-time.Hour),
        }
        assert.True(t, reset.IsExpired())
    })
}
```

**Step 2: Run test to verify it fails**

Run: `task test -- -run TestPasswordReset ./internal/auth/...`

Expected: FAIL - functions not defined

**Step 3: Write minimal implementation**

Create `internal/auth/reset.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
    "context"
    "crypto/rand"
    "crypto/sha256"
    "encoding/hex"
    "time"

    "github.com/oklog/ulid/v2"
    "github.com/samber/oops"
)

// Reset token configuration.
const (
    ResetTokenBytes  = 32          // 32 bytes = 64 hex chars
    ResetTokenExpiry = time.Hour   // 1 hour expiry
)

// PasswordReset represents a password reset request.
type PasswordReset struct {
    ID        ulid.ULID
    PlayerID  ulid.ULID
    TokenHash string
    ExpiresAt time.Time
    CreatedAt time.Time
}

// IsExpired returns true if the reset token has expired.
func (r *PasswordReset) IsExpired() bool {
    return time.Now().After(r.ExpiresAt)
}

// GenerateResetToken creates a secure random token and its hash.
// Returns (plaintext_token, sha256_hash, error).
// The plaintext token is sent to the user; the hash is stored in the database.
func GenerateResetToken() (string, string, error) {
    tokenBytes := make([]byte, ResetTokenBytes)
    if _, err := rand.Read(tokenBytes); err != nil {
        return "", "", oops.Code("RESET_TOKEN_GENERATE_FAILED").Wrap(err)
    }

    token := hex.EncodeToString(tokenBytes)
    hash := hashResetToken(token)

    return token, hash, nil
}

// VerifyResetToken checks if the plaintext token matches the stored hash.
func VerifyResetToken(token, hash string) bool {
    computed := hashResetToken(token)
    return computed == hash
}

func hashResetToken(token string) string {
    h := sha256.Sum256([]byte(token))
    return hex.EncodeToString(h[:])
}

// PasswordResetRepository manages password reset persistence.
type PasswordResetRepository interface {
    // Create stores a new password reset request.
    Create(ctx context.Context, reset *PasswordReset) error

    // GetByPlayer retrieves the latest reset request for a player.
    GetByPlayer(ctx context.Context, playerID ulid.ULID) (*PasswordReset, error)

    // Delete removes a password reset request.
    Delete(ctx context.Context, id ulid.ULID) error

    // DeleteByPlayer removes all reset requests for a player.
    DeleteByPlayer(ctx context.Context, playerID ulid.ULID) error

    // DeleteExpired removes all expired reset requests.
    DeleteExpired(ctx context.Context) (int64, error)
}
```

**Step 4: Run test to verify it passes**

Run: `task test -- -run TestPasswordReset ./internal/auth/...`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/reset.go internal/auth/reset_test.go
git commit -m "feat(auth): implement password reset tokens"
```

---

### Task 5.5.2: Implement Password Reset Repository

**Files:**

- Create: `internal/auth/postgres/reset_repo.go`
- Test: `internal/auth/postgres/reset_repo_test.go`

**Step 1: Implement PostgreSQL PasswordResetRepository**

**Step 2: Write integration tests**

**Step 3: Commit**

```bash
git add internal/auth/postgres/reset_repo.go internal/auth/postgres/reset_repo_test.go
git commit -m "feat(auth): implement password reset repository"
```

---

### Task 5.5.3: Implement Password Reset Service

**Files:**

- Create: `internal/auth/reset_service.go`
- Test: `internal/auth/reset_service_test.go`

**Step 1: Define PasswordResetService**

The service should:

- `RequestReset(email)` - Generate token, store hash, return token (caller sends email)
- `ConfirmReset(token, newPassword)` - Verify token, update password, invalidate sessions
- Handle email not found (security: don't reveal if email exists)

**Step 2: Write tests**

**Step 3: Implement service**

**Step 4: Commit**

```bash
git add internal/auth/reset_service.go internal/auth/reset_service_test.go
git commit -m "feat(auth): implement password reset service"
```

---

## Phase 5.6: Test Helpers and Mocks

### Task 5.6.1: Generate Auth Mocks

**Files:**

- Modify: `.mockery.yaml`
- Create: `internal/auth/authtest/` directory

**Step 1: Add mockery configuration**

Add to `.mockery.yaml`:

```yaml
packages:
  github.com/holomush/holomush/internal/auth:
    interfaces:
      PasswordHasher:
        config:
          outpkg: authtest
          dir: internal/auth/authtest
      PlayerRepository:
        config:
          outpkg: authtest
          dir: internal/auth/authtest
      WebSessionRepository:
        config:
          outpkg: authtest
          dir: internal/auth/authtest
      PasswordResetRepository:
        config:
          outpkg: authtest
          dir: internal/auth/authtest
```

**Step 2: Generate mocks**

Run: `mockery`

**Step 3: Commit**

```bash
git add .mockery.yaml internal/auth/authtest/
git commit -m "feat(auth): add test mocks for auth repositories"
```

---

## Summary

| Phase | Tasks       | Description                                         |
| ----- | ----------- | --------------------------------------------------- |
| 5.1   | 5.1.1-5.1.3 | Database migrations (players, sessions, resets)     |
| 5.2   | 5.2.1-5.2.4 | Password auth (hasher, rate limit, player, repo)    |
| 5.3   | 5.3.1-5.3.2 | Character creation (validation, service)            |
| 5.4   | 5.4.1-5.4.4 | Session binding (tokens, sessions, service, telnet) |
| 5.5   | 5.5.1-5.5.3 | Password reset (tokens, repo, service)              |
| 5.6   | 5.6.1       | Test helpers and mocks                              |

## Dependencies

```text
5.1.1 (player fields migration)
  └── 5.1.2 (web sessions migration)
      └── 5.1.3 (password resets migration)

5.2.1 (hasher) ─────────────────┐
5.2.2 (rate limit) ─────────────┤
5.2.3 (player type) ────────────┼── 5.2.4 (player repo)
                                │
5.1.1 (migration) ──────────────┘

5.2.4 (player repo) ────────────┐
5.3.1 (name validation) ────────┼── 5.3.2 (character service)
                                │
(existing CharacterRepository) ─┘

5.4.1 (token) ──────────────────┐
5.1.2 (session migration) ──────┼── 5.4.2 (session repo)
                                │
5.4.2 (session repo) ───────────┤
5.2.4 (player repo) ────────────┼── 5.4.3 (auth service)
5.2.1 (hasher) ─────────────────┤
5.2.2 (rate limit) ─────────────┘

5.4.3 (auth service) ───────────┬── 5.4.4 (telnet handler)
5.3.2 (character service) ──────┘

5.5.1 (reset token) ────────────┐
5.1.3 (reset migration) ────────┼── 5.5.2 (reset repo)
                                │
5.5.2 (reset repo) ─────────────┤
5.2.4 (player repo) ────────────┼── 5.5.3 (reset service)
5.4.2 (session repo) ───────────┘
```

## Test Strategy

- **Unit tests:** All domain types, validation logic, token signing, rate limiting
- **Integration tests:** Repository implementations against real PostgreSQL
- **Table-driven tests:** Cover edge cases systematically
- **Mocks:** Generated for all repository interfaces
- **Coverage target:** >80% per package

## Acceptance Criteria

From the design spec:

- [ ] Player accounts with username/password work
- [ ] Optional email field for password recovery
- [ ] argon2id hashing with OWASP parameters
- [ ] Telnet: `connect <user> <pass>` → character select → play
- [ ] Web: login form → character select → WebSocket game connection (future epic)
- [ ] Signed session tokens for web clients
- [ ] Character creation with name validation (Init Caps, no numbers)
- [ ] New characters placed in seeded first room
- [ ] Rate limiting: progressive delay → Turnstile (web) → lockout
- [ ] Password reset via email (when configured)
- [ ] Player preferences with default character support
- [ ] player_id nullable on characters (rostering prep)
