// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestNewPlayer(t *testing.T) {
	t.Run("creates valid player with email", func(t *testing.T) {
		email := "test@example.com"
		player, err := auth.NewPlayer("ValidUser", &email, "$argon2id$hash")
		require.NoError(t, err)
		require.NotNil(t, player)

		assert.NotEqual(t, ulid.ULID{}, player.ID)
		assert.Equal(t, "ValidUser", player.Username)
		assert.Equal(t, &email, player.Email)
		assert.Equal(t, "$argon2id$hash", player.PasswordHash)
		assert.False(t, player.CreatedAt.IsZero())
		assert.False(t, player.UpdatedAt.IsZero())
		assert.Equal(t, player.CreatedAt, player.UpdatedAt)
	})

	t.Run("creates valid player without email", func(t *testing.T) {
		player, err := auth.NewPlayer("ValidUser", nil, "$argon2id$hash")
		require.NoError(t, err)
		require.NotNil(t, player)
		assert.Nil(t, player.Email)
	})

	t.Run("rejects empty username", func(t *testing.T) {
		player, err := auth.NewPlayer("", nil, "$argon2id$hash")
		assert.Nil(t, player)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_USERNAME")
	})

	t.Run("rejects short username", func(t *testing.T) {
		player, err := auth.NewPlayer("ab", nil, "$argon2id$hash")
		assert.Nil(t, player)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_USERNAME")
	})

	t.Run("rejects empty password hash", func(t *testing.T) {
		player, err := auth.NewPlayer("ValidUser", nil, "")
		assert.Nil(t, player)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_PASSWORD")
	})

	t.Run("rejects whitespace-only password hash", func(t *testing.T) {
		player, err := auth.NewPlayer("ValidUser", nil, "   \t  ")
		assert.Nil(t, player)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_PASSWORD")
	})
}

func TestPlayer_IsLocked(t *testing.T) {
	t.Run("returns false when no lockout is set", func(t *testing.T) {
		p := &auth.Player{}
		assert.False(t, p.IsLocked())
	})

	t.Run("returns true when locked until is in the future", func(t *testing.T) {
		future := time.Now().Add(time.Hour)
		p := &auth.Player{LockedUntil: &future}
		assert.True(t, p.IsLocked())
	})

	t.Run("returns false when locked until is in the past", func(t *testing.T) {
		past := time.Now().Add(-time.Hour)
		p := &auth.Player{LockedUntil: &past}
		assert.False(t, p.IsLocked())
	})
}

func TestPlayer_RecordFailure(t *testing.T) {
	t.Run("increments the failed attempts counter", func(t *testing.T) {
		p := &auth.Player{FailedAttempts: 0}
		p.RecordFailure()
		assert.Equal(t, 1, p.FailedAttempts)
	})

	t.Run("does not set lockout when below threshold", func(t *testing.T) {
		p := &auth.Player{FailedAttempts: auth.LockoutThreshold - 2}
		p.RecordFailure()
		assert.Equal(t, auth.LockoutThreshold-1, p.FailedAttempts)
		assert.Nil(t, p.LockedUntil)
	})

	t.Run("sets lockout when threshold is reached", func(t *testing.T) {
		p := &auth.Player{FailedAttempts: auth.LockoutThreshold - 1}
		p.RecordFailure()
		assert.Equal(t, auth.LockoutThreshold, p.FailedAttempts)
		assert.NotNil(t, p.LockedUntil)
		assert.True(t, p.LockedUntil.After(time.Now()))
	})

	t.Run("updates the updated-at timestamp", func(t *testing.T) {
		p := &auth.Player{FailedAttempts: 0}
		before := time.Now().Add(-time.Millisecond)
		p.RecordFailure()
		assert.True(t, p.UpdatedAt.After(before))
	})
}

func TestPlayer_RecordSuccess(t *testing.T) {
	t.Run("resets failed attempts and clears lockout", func(t *testing.T) {
		future := time.Now().Add(time.Hour)
		p := &auth.Player{
			FailedAttempts: 5,
			LockedUntil:    &future,
		}
		p.RecordSuccess()
		assert.Equal(t, 0, p.FailedAttempts)
		assert.Nil(t, p.LockedUntil)
	})

	t.Run("updates the updated-at timestamp", func(t *testing.T) {
		p := &auth.Player{FailedAttempts: 3}
		before := time.Now().Add(-time.Millisecond)
		p.RecordSuccess()
		assert.True(t, p.UpdatedAt.After(before))
	})
}

func TestPlayerPreferences(t *testing.T) {
	t.Run("defaults to zero values with auto-login disabled", func(t *testing.T) {
		prefs := auth.PlayerPreferences{}
		assert.False(t, prefs.AutoLogin)
		assert.Equal(t, 0, prefs.MaxCharacters) // 0 means use default
	})

	t.Run("effective max characters uses default when zero", func(t *testing.T) {
		prefs := auth.PlayerPreferences{}
		assert.Equal(t, auth.DefaultMaxCharacters, prefs.EffectiveMaxCharacters())
	})

	t.Run("effective max characters uses custom when set", func(t *testing.T) {
		prefs := auth.PlayerPreferences{MaxCharacters: 10}
		assert.Equal(t, 10, prefs.EffectiveMaxCharacters())
	})

	t.Run("effective max characters uses default when negative", func(t *testing.T) {
		prefs := auth.PlayerPreferences{MaxCharacters: -1}
		assert.Equal(t, auth.DefaultMaxCharacters, prefs.EffectiveMaxCharacters())
	})
}

func TestScenePlayerPreferencesRoundTripsJSON(t *testing.T) {
	tail := 5
	prefs := auth.PlayerPreferences{
		Scenes: auth.ScenePlayerPreferences{FocusReplayTail: &tail},
	}
	data, err := json.Marshal(prefs)
	require.NoError(t, err)

	var decoded auth.PlayerPreferences
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.NotNil(t, decoded.Scenes.FocusReplayTail)
	assert.Equal(t, 5, *decoded.Scenes.FocusReplayTail)
}

func TestScenePlayerPreferencesOmitsNilTail(t *testing.T) {
	prefs := auth.PlayerPreferences{}
	data, err := json.Marshal(prefs)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "focus_replay_tail")
}

func TestScenePlayerPreferencesExplicitZeroIsPreserved(t *testing.T) {
	zero := 0
	prefs := auth.PlayerPreferences{
		Scenes: auth.ScenePlayerPreferences{FocusReplayTail: &zero},
	}
	data, err := json.Marshal(prefs)
	require.NoError(t, err)

	var decoded auth.PlayerPreferences
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.NotNil(t, decoded.Scenes.FocusReplayTail)
	assert.Equal(t, 0, *decoded.Scenes.FocusReplayTail)
}

// TestPlayerPreferencesPluginsBagRoundTrips asserts the opaque, owner-
// partitioned Plugins bag survives a whole-struct JSON marshal/unmarshal cycle
// (the players.preferences JSONB round-trip) without clobbering any typed
// preference field. This is the no-clobber invariant (plan §218).
func TestPlayerPreferencesPluginsBagRoundTrips(t *testing.T) {
	replayTail := 7
	orig := auth.PlayerPreferences{
		AutoLogin:     true,
		MaxCharacters: 3,
		Theme:         "x",
		Scenes:        auth.ScenePlayerPreferences{FocusReplayTail: &replayTail},
		Plugins: map[string]json.RawMessage{
			"core-scenes": json.RawMessage(`{"content.cw_block":["violence"]}`),
		},
	}

	data, err := json.Marshal(orig)
	require.NoError(t, err)

	var got auth.PlayerPreferences
	require.NoError(t, json.Unmarshal(data, &got))

	// Every typed field survives.
	assert.True(t, got.AutoLogin)
	assert.Equal(t, 3, got.MaxCharacters)
	assert.Equal(t, "x", got.Theme)
	require.NotNil(t, got.Scenes.FocusReplayTail)
	assert.Equal(t, 7, *got.Scenes.FocusReplayTail)

	// The opaque plugins bag survives (semantically).
	require.Contains(t, got.Plugins, "core-scenes")
	assert.JSONEq(t, `{"content.cw_block":["violence"]}`, string(got.Plugins["core-scenes"]))
}

func TestPlayer_Fields(t *testing.T) {
	t.Run("all fields are settable", func(t *testing.T) {
		now := time.Now()
		playerID := ulid.Make()
		charID := ulid.Make()
		email := "test@example.com"

		p := &auth.Player{
			ID:                 playerID,
			Username:           "testuser",
			PasswordHash:       "$argon2id$v=19$...",
			Email:              &email,
			EmailVerified:      true,
			FailedAttempts:     2,
			LockedUntil:        nil,
			DefaultCharacterID: &charID,
			Preferences: auth.PlayerPreferences{
				AutoLogin:     true,
				MaxCharacters: 3,
				Theme:         "dark",
			},
			CreatedAt: now,
			UpdatedAt: now,
		}

		assert.Equal(t, playerID, p.ID)
		assert.Equal(t, "testuser", p.Username)
		assert.Equal(t, "$argon2id$v=19$...", p.PasswordHash)
		assert.Equal(t, &email, p.Email)
		assert.True(t, p.EmailVerified)
		assert.Equal(t, 2, p.FailedAttempts)
		assert.Nil(t, p.LockedUntil)
		assert.Equal(t, &charID, p.DefaultCharacterID)
		assert.True(t, p.Preferences.AutoLogin)
		assert.Equal(t, 3, p.Preferences.MaxCharacters)
		assert.Equal(t, "dark", p.Preferences.Theme)
		assert.Equal(t, now, p.CreatedAt)
		assert.Equal(t, now, p.UpdatedAt)
	})
}

func TestNewGuestPlayer(t *testing.T) {
	player, err := auth.NewGuestPlayer("guest_Sapphire_Diamond")
	require.NoError(t, err)
	assert.True(t, player.IsGuest)
	assert.Equal(t, "guest_Sapphire_Diamond", player.Username)
	assert.NotEqual(t, ulid.ULID{}, player.ID)
	assert.Empty(t, player.PasswordHash) // guests have no password
	assert.Nil(t, player.Email)
}

func TestNewGuestPlayerRejectsEmptyUsername(t *testing.T) {
	_, err := auth.NewGuestPlayer("")
	assert.Error(t, err)
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
		{"valid min length", "abc", false},
		{"valid max length", "abcdefghijklmnopqrstuvwxyz1234", false}, // 30 chars
		{"too short", "ab", true},
		{"too long", "abcdefghijklmnopqrstuvwxyz12345", true}, // 31 chars
		{"empty", "", true},
		{"spaces", "test user", true},
		{"special chars at", "test@user", true},
		{"special chars bang", "test!user", true},
		{"special chars hyphen", "test-user", true},
		{"starts with number", "123user", true},
		{"starts with underscore", "_user", true},
		{"uppercase valid", "TestUser", false},
		{"mixed case valid", "Test_User_123", false},
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

func TestValidateUsername_ErrorCodes(t *testing.T) {
	t.Run("empty username has correct error code", func(t *testing.T) {
		err := auth.ValidateUsername("")
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_USERNAME")
		assert.Contains(t, err.Error(), "empty")
	})

	t.Run("too short has correct error code", func(t *testing.T) {
		err := auth.ValidateUsername("ab")
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_USERNAME")
		assert.Contains(t, err.Error(), "at least")
	})

	t.Run("too long has correct error code", func(t *testing.T) {
		err := auth.ValidateUsername("abcdefghijklmnopqrstuvwxyz12345")
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_USERNAME")
		assert.Contains(t, err.Error(), "at most")
	})

	t.Run("invalid chars has correct error code", func(t *testing.T) {
		err := auth.ValidateUsername("test@user")
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_USERNAME")
		assert.Contains(t, err.Error(), "letter")
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// IDENT-08 — the player-username regression pin and the two-policy separation
// guards.
//
// 01-SPEC.md §6.2 is explicit that v0.13 writes NO new username validation:
// IDENT-08 is discharged by pinning the rule that already exists
// (`^[a-zA-Z][a-zA-Z0-9_]*$`, 3..30) so that a future change to the
// CHARACTER-name pipeline cannot quietly reach the USERNAME path. Nothing
// below validates anything; it all asserts that what is already there stays
// there.
// ─────────────────────────────────────────────────────────────────────────────

// acceptedUsernameControl is the paired positive control every rejection row
// below runs on the same fixture. Without it `err != nil` cannot distinguish
// "rejected as non-ASCII" from "rejected for length" — or from a validator
// that has started rejecting everything.
const acceptedUsernameControl = "alaric_01"

func TestValidateUsernameStillRejectsEveryNonASCIIAndLeadingNonLetterShape(t *testing.T) {
	// Non-ASCII fixtures are spelled as \uXXXX escapes: a literal fullwidth a
	// and a literal ASCII a are indistinguishable in a diff, and this suite
	// exists precisely to keep them distinguishable.
	tests := []struct {
		name     string
		username string
		wantErr  bool
	}{
		{name: "a name carrying a non-ASCII accented letter", username: "Jos\u00E9", wantErr: true},
		{name: "a name written in Cyrillic", username: "\u0418\u0432\u0430\u043D", wantErr: true},
		{name: "a name carrying a fullwidth Latin letter", username: "\uFF41laric", wantErr: true},
		{name: "a name carrying a format codepoint", username: "alaric\u200Db", wantErr: true},
		{name: "a name starting with a digit", username: "1alaric", wantErr: true},
		{name: "a name starting with an underscore", username: "_alaric", wantErr: true},
		{name: "a name below the minimum length", username: "ab", wantErr: true},
		{name: "a name above the maximum length", username: strings.Repeat("a", auth.MaxUsernameLength+1), wantErr: true},

		{name: "a name at the minimum length", username: "abc"},
		{name: "a name at the maximum length", username: strings.Repeat("a", auth.MaxUsernameLength)},
		{name: "an ASCII name with a digit and an underscore", username: acceptedUsernameControl},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := auth.ValidateUsername(tt.username)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "AUTH_INVALID_USERNAME")

			// The pairing, on the same fixture and in the same subtest.
			require.NoError(t, auth.ValidateUsername(acceptedUsernameControl),
				"the accepted control proves the rejection above is attributable to this input")
		})
	}
}

const (
	authPkgPath     = "github.com/holomush/holomush/internal/auth"
	charnamePkgPath = "github.com/holomush/holomush/internal/charname"
)

// importsWithPrefix returns the import paths declared by a single Go file that
// start with prefix.
//
// It parses the file rather than grepping it, so a path mentioned in a comment
// or a string literal cannot satisfy or defeat the guard — only a real import
// declaration counts.
func importsWithPrefix(t *testing.T, path, prefix string) []string {
	t.Helper()

	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	require.NoError(t, err, "parsing %s", path)

	var found []string
	for _, spec := range parsed.Imports {
		unquoted, err := strconv.Unquote(spec.Path.Value)
		require.NoError(t, err)
		if strings.HasPrefix(unquoted, prefix) {
			found = append(found, unquoted)
		}
	}
	return found
}

// nonTestFilesImportingPrefix returns the names of the non-test .go files under
// root — sub-packages included — that import a path starting with prefix.
func nonTestFilesImportingPrefix(t *testing.T, root, prefix string) []string {
	t.Helper()

	var offenders []string
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if len(importsWithPrefix(t, path, prefix)) > 0 {
			offenders = append(offenders, filepath.Base(path))
		}
		return nil
	}))
	sort.Strings(offenders)
	return offenders
}

// identsReachableFrom returns every identifier name appearing in the body of
// root, following calls to functions declared in the SAME file.
//
// Same-file only is the honest scope: this walks one file's AST, so a call into
// another package is opaque to it. That is sufficient here because the claim
// being pinned is about what player.go itself does.
func identsReachableFrom(t *testing.T, path, root string) map[string]bool {
	t.Helper()

	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	require.NoError(t, err, "parsing %s", path)

	decls := map[string]*ast.FuncDecl{}
	for _, decl := range parsed.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			decls[fn.Name.Name] = fn
		}
	}
	require.Contains(t, decls, root, "%s declares no %s", path, root)

	seen := map[string]bool{}
	idents := map[string]bool{}

	var walk func(name string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true

		fn, ok := decls[name]
		if !ok {
			return // declared elsewhere; opaque to a single-file walk
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok {
				idents[ident.Name] = true
				if _, isLocal := decls[ident.Name]; isLocal {
					walk(ident.Name)
				}
			}
			return true
		})
	}
	walk(root)

	return idents
}

// TestTheCharacterNameAndUsernamePoliciesCannotReachEachOther is IDENT-08's
// separation guard, and it is DIRECTIONAL and FILE-SCOPED on purpose.
//
// The obvious form — "no file under internal/auth imports internal/charname" —
// would be wrong, and expensively so. Plan 02-06 makes
// internal/auth/character_service.go and internal/auth/guest_service.go call
// Gate.Admit, deliberately, because character-name admission is exactly those
// services' job. A package-wide ban would therefore be RED BY DESIGN one wave
// later, and the cheap repair would be deleting this guard — losing the
// protection entirely.
//
// What §6.2's two-policy separation actually needs is these two claims, both of
// which stay true forever:
//
//  1. internal/charname MUST NOT import internal/auth. This is the direction
//     that matters most: it makes it impossible for the character-name pipeline
//     to start consulting the credential validator.
//  2. internal/auth/player.go — the file that OWNS the username rule — MUST NOT
//     reach charname.
//
// Each claim carries a synthetic-fixture control, because a guard nobody has
// watched fail is a guard nobody knows works.
func TestTheCharacterNameAndUsernamePoliciesCannotReachEachOther(t *testing.T) {
	t.Run("no non-test file under internal/charname imports internal/auth", func(t *testing.T) {
		charnameDir := filepath.Join("..", "charname")

		// Non-vacuity first. An Empty assertion over a walk that reached no
		// files, or that cannot see imports at all, passes for the wrong
		// reason forever. Probing for a package internal/charname demonstrably
		// DOES import proves the walker is looking at real code.
		require.NotEmpty(t, nonTestFilesImportingPrefix(t, charnameDir, "github.com/samber/oops"),
			"the walk reaches real internal/charname files and can see their imports")

		assert.Empty(t, nonTestFilesImportingPrefix(t, charnameDir, authPkgPath),
			"the character-name pipeline must not be able to reach the credential validator")
	})

	t.Run("the charname-to-auth guard flags a planted import in a synthetic package", func(t *testing.T) {
		// The RED demonstration, run against a fixture rather than against the
		// real tree: planting an import in real internal/charname code and
		// reverting it would prove the guard fires, but only by briefly
		// committing the very edge the guard forbids.
		dir := t.TempDir()
		writeGoFixture(t, dir, "clean.go", "package charname\n")
		writeGoFixture(t, dir, "planted.go", "package charname\n\nimport _ \""+authPkgPath+"\"\n")

		assert.Equal(t, []string{"planted.go"}, nonTestFilesImportingPrefix(t, dir, authPkgPath))
	})

	t.Run("player.go imports no internal/charname path", func(t *testing.T) {
		assert.Empty(t, importsWithPrefix(t, "player.go", charnamePkgPath),
			"the username policy is implemented by its own regex and nothing else")
	})

	t.Run("no identifier named charname is reachable from ValidateUsername within player.go", func(t *testing.T) {
		reachable := identsReachableFrom(t, "player.go", "ValidateUsername")

		// Non-vacuity: the walk really does reach into the rule. usernameRegex
		// is the identifier the whole policy turns on, so its presence proves
		// the NotContains below is a statement about a populated set.
		require.Contains(t, reachable, "usernameRegex",
			"the call-graph walk reaches the regex the username policy is implemented by")

		assert.NotContains(t, reachable, "charname")
	})

	t.Run("the call-graph walk flags a planted charname call in a synthetic player.go", func(t *testing.T) {
		// The RED demonstration for the second guard, against a fixture: a
		// helper that never detects anything would satisfy the assertion above
		// no matter what player.go grew.
		dir := t.TempDir()
		writeGoFixture(t, dir, "player.go", "package auth\n\n"+
			"import \""+charnamePkgPath+"\"\n\n"+
			"func ValidateUsername(s string) error { return normalizeIt(s) }\n\n"+
			"func normalizeIt(s string) error { _, err := charname.Normalize(s); return err }\n")

		reachable := identsReachableFrom(t, filepath.Join(dir, "player.go"), "ValidateUsername")
		assert.Contains(t, reachable, "charname",
			"a charname call reached through a same-file helper is detected")
	})

	t.Run("a file in internal/auth that is not player.go may import internal/charname", func(t *testing.T) {
		// The control that proves this guard is file-scoped rather than
		// package-wide — the criterion that keeps it green after plan 02-06
		// lands Gate.Admit in character_service.go and guest_service.go.
		dir := t.TempDir()
		writeGoFixture(t, dir, "player.go", "package auth\n")
		writeGoFixture(t, dir, "character_service.go", "package auth\n\nimport _ \""+charnamePkgPath+"\"\n")

		assert.Empty(t, importsWithPrefix(t, filepath.Join(dir, "player.go"), charnamePkgPath),
			"the file-scoped guard passes: player.go is clean")
		assert.Equal(t, []string{"character_service.go"}, nonTestFilesImportingPrefix(t, dir, charnamePkgPath),
			"and the package-wide form that plan 02-06 would make RED does flag it \u2014 "+
				"asserted so the difference between the two guards is demonstrated, not merely described")
	})
}

func writeGoFixture(t *testing.T, dir, name, src string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600))
}
