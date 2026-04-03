// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/auth"
	authmocks "github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
)

// mockCharLister is a test implementation of CharacterLister.
type mockCharLister struct {
	mock.Mock
}

func (m *mockCharLister) ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error) {
	args := m.Called(ctx, playerID)
	chars, _ := args.Get(0).([]*world.Character)
	return chars, args.Error(1)
}

// resetTestSetup holds common test fixtures for reset password tests.
type resetTestSetup struct {
	playerRepo     *authmocks.MockPlayerRepository
	hasher         *authmocks.MockPasswordHasher
	playerSessions *authmocks.MockPlayerSessionRepository
	resetRepo      *authmocks.MockPasswordResetRepository
	charLister     *mockCharLister
	sessionMgr     *session.MemStore
	buf            *bytes.Buffer
	charID         ulid.ULID
	playerID       ulid.ULID
	targetID       ulid.ULID
}

func newResetTestSetup(t *testing.T) *resetTestSetup {
	t.Helper()
	return &resetTestSetup{
		playerRepo:     authmocks.NewMockPlayerRepository(t),
		hasher:         authmocks.NewMockPasswordHasher(t),
		playerSessions: authmocks.NewMockPlayerSessionRepository(t),
		resetRepo:      authmocks.NewMockPasswordResetRepository(t),
		charLister:     &mockCharLister{},
		sessionMgr:     session.NewMemStore(),
		buf:            &bytes.Buffer{},
		charID:         ulid.Make(),
		playerID:       ulid.Make(),
		targetID:       ulid.Make(),
	}
}

func (s *resetTestSetup) deps() AdminDeps {
	return AdminDeps{
		PlayerRepo:     s.playerRepo,
		Hasher:         s.hasher,
		PlayerSessions: s.playerSessions,
		ResetRepo:      s.resetRepo,
		CharLister:     s.charLister,
	}
}

func (s *resetTestSetup) execAllowAll(t *testing.T, args string) *command.CommandExecution {
	t.Helper()
	return s.execWithMockEngine(t, args, policytest.AllowAllEngine())
}

func (s *resetTestSetup) execWithMockEngine(t *testing.T, args string, engine *policytest.MockAccessPolicyEngine) *command.CommandExecution {
	t.Helper()
	svc := command.NewTestServices(command.ServicesConfig{
		Engine:  engine,
		Session: s.sessionMgr,
	})
	return command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   s.charID,
		CharacterName: "Admin",
		PlayerID:      s.playerID,
		Args:          args,
		Output:        s.buf,
		Services:      svc,
	})
}

func (s *resetTestSetup) execWithGrantEngine(t *testing.T, args string, engine *policytest.GrantEngine) *command.CommandExecution {
	t.Helper()
	svc := command.NewTestServices(command.ServicesConfig{
		Engine:  engine,
		Session: s.sessionMgr,
	})
	return command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   s.charID,
		CharacterName: "Admin",
		PlayerID:      s.playerID,
		Args:          args,
		Output:        s.buf,
		Services:      svc,
	})
}

func (s *resetTestSetup) targetPlayer() *auth.Player {
	return &auth.Player{
		ID:       s.targetID,
		Username: "targetuser",
	}
}

func TestResetPassword(t *testing.T) {
	ctx := context.Background()

	t.Run("generated password happy path", func(t *testing.T) {
		s := newResetTestSetup(t)
		player := s.targetPlayer()
		s.playerRepo.EXPECT().GetByUsername(ctx, "targetuser").Return(player, nil)
		s.hasher.EXPECT().Hash(mock.AnythingOfType("string")).Return("hashed-pw", nil)
		s.playerRepo.EXPECT().UpdatePasswordAndClearLockout(ctx, s.targetID, "hashed-pw").Return(nil)
		s.resetRepo.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(nil)
		s.playerSessions.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(nil)

		handler := NewResetPasswordHandler(s.deps())
		e := s.execAllowAll(t, "targetuser")
		err := handler(ctx, e)

		require.NoError(t, err)
		output := s.buf.String()
		assert.Contains(t, output, "Password for targetuser has been reset.")
		assert.Contains(t, output, "New password:")
	})

	t.Run("explicit password happy path", func(t *testing.T) {
		s := newResetTestSetup(t)
		player := s.targetPlayer()
		s.playerRepo.EXPECT().GetByUsername(ctx, "targetuser").Return(player, nil)
		s.hasher.EXPECT().Hash("newpassword1").Return("hashed-pw", nil)
		s.playerRepo.EXPECT().UpdatePasswordAndClearLockout(ctx, s.targetID, "hashed-pw").Return(nil)
		s.resetRepo.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(nil)
		s.playerSessions.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(nil)

		handler := NewResetPasswordHandler(s.deps())
		e := s.execAllowAll(t, "targetuser newpassword1")
		err := handler(ctx, e)

		require.NoError(t, err)
		output := s.buf.String()
		assert.Contains(t, output, "Password for targetuser has been reset.")
		assert.NotContains(t, output, "New password:")
	})

	t.Run("reset with --kick terminates game sessions", func(t *testing.T) {
		s := newResetTestSetup(t)
		player := s.targetPlayer()
		char1ID := ulid.Make()
		char2ID := ulid.Make()

		// Pre-populate sessions for both characters.
		require.NoError(t, s.sessionMgr.Set(ctx, "sess-1", &session.Info{
			ID: "sess-1", CharacterID: char1ID, CharacterName: "Char1",
		}))
		require.NoError(t, s.sessionMgr.Set(ctx, "sess-2", &session.Info{
			ID: "sess-2", CharacterID: char2ID, CharacterName: "Char2",
		}))

		s.playerRepo.EXPECT().GetByUsername(ctx, "targetuser").Return(player, nil)
		s.hasher.EXPECT().Hash(mock.AnythingOfType("string")).Return("hashed-pw", nil)
		s.playerRepo.EXPECT().UpdatePasswordAndClearLockout(ctx, s.targetID, "hashed-pw").Return(nil)
		s.resetRepo.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(nil)
		s.playerSessions.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(nil)
		s.charLister.On("ListByPlayer", ctx, s.targetID).Return([]*world.Character{
			{ID: char1ID, Name: "Char1"},
			{ID: char2ID, Name: "Char2"},
		}, nil)

		handler := NewResetPasswordHandler(s.deps())
		e := s.execAllowAll(t, "targetuser --kick")
		err := handler(ctx, e)

		require.NoError(t, err)
		assert.Contains(t, s.buf.String(), "Password for targetuser has been reset.")
		s.charLister.AssertCalled(t, "ListByPlayer", ctx, s.targetID)

		// Verify sessions were actually deleted — FindByCharacter returns
		// SESSION_NOT_FOUND when no active session exists.
		sess1, findErr := s.sessionMgr.FindByCharacter(ctx, char1ID)
		assert.Nil(t, sess1)
		assert.Error(t, findErr)
		sess2, findErr := s.sessionMgr.FindByCharacter(ctx, char2ID)
		assert.Nil(t, sess2)
		assert.Error(t, findErr)
	})

	t.Run("combined explicit password and --kick", func(t *testing.T) {
		s := newResetTestSetup(t)
		player := s.targetPlayer()
		char1ID := ulid.Make()

		// Pre-populate session for the character.
		require.NoError(t, s.sessionMgr.Set(ctx, "sess-1", &session.Info{
			ID: "sess-1", CharacterID: char1ID, CharacterName: "Char1",
		}))

		s.playerRepo.EXPECT().GetByUsername(ctx, "targetuser").Return(player, nil)
		s.hasher.EXPECT().Hash("mypassword").Return("hashed-pw", nil)
		s.playerRepo.EXPECT().UpdatePasswordAndClearLockout(ctx, s.targetID, "hashed-pw").Return(nil)
		s.resetRepo.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(nil)
		s.playerSessions.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(nil)
		s.charLister.On("ListByPlayer", ctx, s.targetID).Return([]*world.Character{
			{ID: char1ID, Name: "Char1"},
		}, nil)

		handler := NewResetPasswordHandler(s.deps())
		e := s.execAllowAll(t, "targetuser mypassword --kick")
		err := handler(ctx, e)

		require.NoError(t, err)
		assert.Contains(t, s.buf.String(), "Password for targetuser has been reset.")
		assert.NotContains(t, s.buf.String(), "New password:")

		// Verify session was actually deleted.
		sess, findErr := s.sessionMgr.FindByCharacter(ctx, char1ID)
		assert.Nil(t, sess)
		assert.Error(t, findErr)
	})

	t.Run("--kick before password is position independent", func(t *testing.T) {
		s := newResetTestSetup(t)
		player := s.targetPlayer()

		s.playerRepo.EXPECT().GetByUsername(ctx, "targetuser").Return(player, nil)
		s.hasher.EXPECT().Hash("mypassword").Return("hashed-pw", nil)
		s.playerRepo.EXPECT().UpdatePasswordAndClearLockout(ctx, s.targetID, "hashed-pw").Return(nil)
		s.resetRepo.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(nil)
		s.playerSessions.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(nil)
		s.charLister.On("ListByPlayer", ctx, s.targetID).Return([]*world.Character{}, nil)

		handler := NewResetPasswordHandler(s.deps())
		e := s.execAllowAll(t, "targetuser --kick mypassword")
		err := handler(ctx, e)

		require.NoError(t, err)
		assert.NotContains(t, s.buf.String(), "New password:")
	})

	t.Run("explicit password without capability returns permission denied", func(t *testing.T) {
		s := newResetTestSetup(t)

		engine := policytest.NewGrantEngine()

		handler := NewResetPasswordHandler(s.deps())
		e := s.execWithGrantEngine(t, "targetuser secret123", engine)
		err := handler(ctx, e)

		require.Error(t, err)
		oopsErr, ok := oops.AsOops(err)
		require.True(t, ok)
		assert.Equal(t, command.CodePermissionDenied, oopsErr.Code())
	})

	t.Run("--kick without capability returns permission denied", func(t *testing.T) {
		s := newResetTestSetup(t)

		engine := policytest.NewGrantEngine()

		handler := NewResetPasswordHandler(s.deps())
		e := s.execWithGrantEngine(t, "targetuser --kick", engine)
		err := handler(ctx, e)

		require.Error(t, err)
		oopsErr, ok := oops.AsOops(err)
		require.True(t, ok)
		assert.Equal(t, command.CodePermissionDenied, oopsErr.Code())
	})

	t.Run("player not found returns target not found", func(t *testing.T) {
		s := newResetTestSetup(t)
		s.playerRepo.EXPECT().GetByUsername(ctx, "unknownuser").Return(nil, auth.ErrNotFound)

		handler := NewResetPasswordHandler(s.deps())
		e := s.execAllowAll(t, "unknownuser")
		err := handler(ctx, e)

		require.Error(t, err)
		oopsErr, ok := oops.AsOops(err)
		require.True(t, ok)
		assert.Equal(t, command.CodeTargetNotFound, oopsErr.Code())
	})

	t.Run("password too short returns world error", func(t *testing.T) {
		s := newResetTestSetup(t)
		player := s.targetPlayer()
		s.playerRepo.EXPECT().GetByUsername(ctx, "targetuser").Return(player, nil)

		handler := NewResetPasswordHandler(s.deps())
		e := s.execAllowAll(t, "targetuser short")
		err := handler(ctx, e)

		require.Error(t, err)
		oopsErr, ok := oops.AsOops(err)
		require.True(t, ok)
		assert.Equal(t, command.CodeWorldError, oopsErr.Code())
	})

	t.Run("admin resetting own player password succeeds", func(t *testing.T) {
		s := newResetTestSetup(t)
		// Admin's playerID matches the target player's ID.
		selfPlayer := &auth.Player{
			ID:       s.playerID,
			Username: "adminuser",
		}
		s.playerRepo.EXPECT().GetByUsername(ctx, "adminuser").Return(selfPlayer, nil)
		s.hasher.EXPECT().Hash(mock.AnythingOfType("string")).Return("hashed-pw", nil)
		s.playerRepo.EXPECT().UpdatePasswordAndClearLockout(ctx, s.playerID, "hashed-pw").Return(nil)
		s.resetRepo.EXPECT().DeleteByPlayer(ctx, s.playerID).Return(nil)
		s.playerSessions.EXPECT().DeleteByPlayer(ctx, s.playerID).Return(nil)

		handler := NewResetPasswordHandler(s.deps())
		e := s.execAllowAll(t, "adminuser")
		err := handler(ctx, e)

		require.NoError(t, err)
		assert.Contains(t, s.buf.String(), "Password for adminuser has been reset.")
	})

	t.Run("no args returns invalid args", func(t *testing.T) {
		s := newResetTestSetup(t)

		handler := NewResetPasswordHandler(s.deps())
		e := s.execAllowAll(t, "")
		err := handler(ctx, e)

		require.Error(t, err)
		oopsErr, ok := oops.AsOops(err)
		require.True(t, ok)
		assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
	})

	t.Run("hash failure returns reset password failed", func(t *testing.T) {
		s := newResetTestSetup(t)
		player := s.targetPlayer()
		s.playerRepo.EXPECT().GetByUsername(ctx, "targetuser").Return(player, nil)
		s.hasher.EXPECT().Hash(mock.AnythingOfType("string")).Return("", errors.New("hash error"))

		handler := NewResetPasswordHandler(s.deps())
		e := s.execAllowAll(t, "targetuser")
		err := handler(ctx, e)

		require.Error(t, err)
		oopsErr, ok := oops.AsOops(err)
		require.True(t, ok)
		assert.Equal(t, command.CodeResetPasswordFailed, oopsErr.Code())
	})

	t.Run("update password failure returns reset password failed", func(t *testing.T) {
		s := newResetTestSetup(t)
		player := s.targetPlayer()
		s.playerRepo.EXPECT().GetByUsername(ctx, "targetuser").Return(player, nil)
		s.hasher.EXPECT().Hash(mock.AnythingOfType("string")).Return("hashed-pw", nil)
		s.playerRepo.EXPECT().UpdatePasswordAndClearLockout(ctx, s.targetID, "hashed-pw").Return(errors.New("db error"))

		handler := NewResetPasswordHandler(s.deps())
		e := s.execAllowAll(t, "targetuser")
		err := handler(ctx, e)

		require.Error(t, err)
		oopsErr, ok := oops.AsOops(err)
		require.True(t, ok)
		assert.Equal(t, command.CodeResetPasswordFailed, oopsErr.Code())
	})

	t.Run("web session delete failure still succeeds", func(t *testing.T) {
		s := newResetTestSetup(t)
		player := s.targetPlayer()
		s.playerRepo.EXPECT().GetByUsername(ctx, "targetuser").Return(player, nil)
		s.hasher.EXPECT().Hash(mock.AnythingOfType("string")).Return("hashed-pw", nil)
		s.playerRepo.EXPECT().UpdatePasswordAndClearLockout(ctx, s.targetID, "hashed-pw").Return(nil)
		s.resetRepo.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(nil)
		s.playerSessions.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(errors.New("session error"))

		handler := NewResetPasswordHandler(s.deps())
		e := s.execAllowAll(t, "targetuser")
		err := handler(ctx, e)

		require.NoError(t, err)
		assert.Contains(t, s.buf.String(), "Password for targetuser has been reset.")
	})

	t.Run("game session delete failure still succeeds", func(t *testing.T) {
		s := newResetTestSetup(t)
		player := s.targetPlayer()
		char1ID := ulid.Make()

		s.playerRepo.EXPECT().GetByUsername(ctx, "targetuser").Return(player, nil)
		s.hasher.EXPECT().Hash(mock.AnythingOfType("string")).Return("hashed-pw", nil)
		s.playerRepo.EXPECT().UpdatePasswordAndClearLockout(ctx, s.targetID, "hashed-pw").Return(nil)
		s.resetRepo.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(nil)
		s.playerSessions.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(nil)
		s.charLister.On("ListByPlayer", ctx, s.targetID).Return([]*world.Character{
			{ID: char1ID, Name: "Char1"},
		}, nil)

		handler := NewResetPasswordHandler(s.deps())
		e := s.execAllowAll(t, "targetuser --kick")
		err := handler(ctx, e)

		require.NoError(t, err)
		assert.Contains(t, s.buf.String(), "Password for targetuser has been reset.")
	})

	t.Run("reset token cleanup failure still succeeds", func(t *testing.T) {
		s := newResetTestSetup(t)
		player := s.targetPlayer()
		s.playerRepo.EXPECT().GetByUsername(ctx, "targetuser").Return(player, nil)
		s.hasher.EXPECT().Hash(mock.AnythingOfType("string")).Return("hashed-pw", nil)
		s.playerRepo.EXPECT().UpdatePasswordAndClearLockout(ctx, s.targetID, "hashed-pw").Return(nil)
		s.resetRepo.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(errors.New("cleanup error"))
		s.playerSessions.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(nil)

		handler := NewResetPasswordHandler(s.deps())
		e := s.execAllowAll(t, "targetuser")
		err := handler(ctx, e)

		require.NoError(t, err)
		assert.Contains(t, s.buf.String(), "Password for targetuser has been reset.")
	})

	t.Run("lockout cleared on reset", func(t *testing.T) {
		s := newResetTestSetup(t)
		player := s.targetPlayer()
		player.FailedAttempts = 5

		s.playerRepo.EXPECT().GetByUsername(ctx, "targetuser").Return(player, nil)
		s.hasher.EXPECT().Hash(mock.AnythingOfType("string")).Return("hashed-pw", nil)
		// UpdatePasswordAndClearLockout atomically clears lockout — no separate Update needed.
		s.playerRepo.EXPECT().UpdatePasswordAndClearLockout(ctx, s.targetID, "hashed-pw").Return(nil)
		s.resetRepo.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(nil)
		s.playerSessions.EXPECT().DeleteByPlayer(ctx, s.targetID).Return(nil)

		handler := NewResetPasswordHandler(s.deps())
		e := s.execAllowAll(t, "targetuser")
		err := handler(ctx, e)

		require.NoError(t, err)
		assert.Contains(t, s.buf.String(), "Password for targetuser has been reset.")
	})

	t.Run("player lookup DB error returns reset password failed", func(t *testing.T) {
		s := newResetTestSetup(t)
		s.playerRepo.EXPECT().GetByUsername(ctx, "targetuser").Return(nil, errors.New("connection refused"))

		handler := NewResetPasswordHandler(s.deps())
		e := s.execAllowAll(t, "targetuser")
		err := handler(ctx, e)

		require.Error(t, err)
		oopsErr, ok := oops.AsOops(err)
		require.True(t, ok)
		assert.Equal(t, command.CodeResetPasswordFailed, oopsErr.Code())
	})
}

func TestGeneratePassword(t *testing.T) {
	pw, err := generatePassword()
	require.NoError(t, err)
	assert.Len(t, pw, generatedPasswordLen)

	for _, ch := range pw {
		assert.Contains(t, alphanumericChars, string(ch))
	}
}

func TestParseResetArgs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    resetArgs
		wantErr bool
	}{
		{
			name:  "username only",
			input: "bob",
			want:  resetArgs{username: "bob"},
		},
		{
			name:  "username and password",
			input: "bob newpass12",
			want:  resetArgs{username: "bob", password: "newpass12"},
		},
		{
			name:  "username and --kick",
			input: "bob --kick",
			want:  resetArgs{username: "bob", kick: true},
		},
		{
			name:  "username password and --kick",
			input: "bob newpass12 --kick",
			want:  resetArgs{username: "bob", password: "newpass12", kick: true},
		},
		{
			name:  "--kick before password",
			input: "bob --kick newpass12",
			want:  resetArgs{username: "bob", password: "newpass12", kick: true},
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "too many non-flag args",
			input:   "bob pass1234 extra",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseResetArgs(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
