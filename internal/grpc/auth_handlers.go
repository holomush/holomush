// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// AuthServiceProvider defines the auth.Service methods used by auth handlers.
type AuthServiceProvider interface {
	ValidateCredentials(ctx context.Context, username, password string) (*auth.Player, error)
	CreatePlayer(ctx context.Context, username, password, email string) (*auth.Player, *auth.PlayerToken, error)
	Logout(ctx context.Context, sessionID ulid.ULID) error
}

// CharacterServiceProvider defines the auth.CharacterService methods used by auth handlers.
type CharacterServiceProvider interface {
	Create(ctx context.Context, playerID ulid.ULID, name string) (*world.Character, error)
}

// ResetServiceProvider defines the auth.PasswordResetService methods used by auth handlers.
type ResetServiceProvider interface {
	RequestReset(ctx context.Context, email string) (string, error)
	ResetPassword(ctx context.Context, token, newPassword string) error
}

// WithAuthService sets the auth service for two-phase login.
func WithAuthService(svc AuthServiceProvider) CoreServerOption {
	return func(s *CoreServer) {
		s.authService = svc
	}
}

// WithResetService sets the password reset service.
func WithResetService(svc ResetServiceProvider) CoreServerOption {
	return func(s *CoreServer) {
		s.resetService = svc
	}
}

// WithCharacterService sets the character creation service.
func WithCharacterService(svc CharacterServiceProvider) CoreServerOption {
	return func(s *CoreServer) {
		s.characterService = svc
	}
}

// WithPlayerTokenRepo sets the player token repository.
func WithPlayerTokenRepo(repo auth.PlayerTokenRepository) CoreServerOption {
	return func(s *CoreServer) {
		s.playerTokenRepo = repo
	}
}

// WithPlayerRepo sets the player repository.
func WithPlayerRepo(repo auth.PlayerRepository) CoreServerOption {
	return func(s *CoreServer) {
		s.playerRepo = repo
	}
}

// WithCharacterRepo sets the character repository (for ListByPlayer).
func WithCharacterRepo(repo auth.CharacterRepository) CoreServerOption {
	return func(s *CoreServer) {
		s.charRepo = repo
	}
}

// AuthenticatePlayer validates credentials and returns a player token for character selection.
func (s *CoreServer) AuthenticatePlayer(ctx context.Context, req *corev1.AuthenticatePlayerRequest) (*corev1.AuthenticatePlayerResponse, error) {
	if s.authService == nil {
		return &corev1.AuthenticatePlayerResponse{
			Success:      false,
			ErrorMessage: "authentication not configured",
		}, nil
	}

	player, validateErr := s.authService.ValidateCredentials(ctx, req.Username, req.Password)
	if validateErr != nil {
		//nolint:nilerr // intentional: return user-facing error in response body
		return &corev1.AuthenticatePlayerResponse{
			Success:      false,
			ErrorMessage: "invalid username or password",
		}, nil
	}

	playerToken, tokenErr := auth.NewPlayerToken(player.ID, 5*time.Minute)
	if tokenErr != nil {
		return nil, oops.Code("TOKEN_GENERATION_FAILED").Wrap(tokenErr)
	}

	if storeErr := s.playerTokenRepo.Create(ctx, playerToken); storeErr != nil {
		return nil, oops.Code("TOKEN_STORE_FAILED").Wrap(storeErr)
	}

	characters, err := s.buildCharacterSummaries(ctx, player.ID)
	if err != nil {
		slog.WarnContext(ctx, "failed to build character summaries", "error", err)
	}

	var defaultCharID string
	if player.DefaultCharacterID != nil {
		defaultCharID = player.DefaultCharacterID.String()
	}

	return &corev1.AuthenticatePlayerResponse{
		Success:            true,
		PlayerToken:        playerToken.Token,
		Characters:         characters,
		DefaultCharacterId: defaultCharID,
	}, nil
}

// SelectCharacter validates a player token, creates or reattaches a game session.
func (s *CoreServer) SelectCharacter(ctx context.Context, req *corev1.SelectCharacterRequest) (*corev1.SelectCharacterResponse, error) {
	if s.playerTokenRepo == nil {
		return &corev1.SelectCharacterResponse{
			Success: false, ErrorMessage: "player token service not configured",
		}, nil
	}
	token, tokenErr := s.playerTokenRepo.GetByToken(ctx, req.PlayerToken)
	if tokenErr != nil {
		//nolint:nilerr // intentional: return user-facing error in response body
		return &corev1.SelectCharacterResponse{
			Success:      false,
			ErrorMessage: "invalid player token",
		}, nil
	}

	if token.IsExpired() {
		return &corev1.SelectCharacterResponse{
			Success:      false,
			ErrorMessage: "player token expired",
		}, nil
	}

	charID, parseErr := ulid.Parse(req.CharacterId)
	if parseErr != nil {
		//nolint:nilerr // intentional: return user-facing error in response body
		return &corev1.SelectCharacterResponse{
			Success:      false,
			ErrorMessage: "invalid character_id",
		}, nil
	}

	// Security check: character must belong to the authenticated player.
	chars, err := s.charRepo.ListByPlayer(ctx, token.PlayerID)
	if err != nil {
		return nil, oops.Code("CHARACTER_LOOKUP_FAILED").Wrap(err)
	}

	var selectedChar *world.Character
	for _, c := range chars {
		if c.ID == charID {
			selectedChar = c
			break
		}
	}
	if selectedChar == nil {
		return &corev1.SelectCharacterResponse{
			Success:      false,
			ErrorMessage: "character does not belong to this player",
		}, nil
	}

	// Check for existing session to reattach.
	existingSession, findErr := s.sessionStore.FindByCharacter(ctx, charID)
	if findErr == nil && existingSession != nil {
		// Reattach: update session status back to active.
		now := time.Now()
		if updateErr := s.sessionStore.UpdateStatus(ctx, existingSession.ID,
			session.StatusActive, nil, nil); updateErr != nil {
			slog.WarnContext(ctx, "failed to reactivate session", "error", updateErr)
		}
		existingSession.Status = session.StatusActive
		existingSession.UpdatedAt = now

		// Connect to session manager.
		connID := core.NewULID()
		s.sessions.Connect(charID, connID)

		// Delete the player token (one-time use).
		if delErr := s.playerTokenRepo.DeleteByToken(ctx, req.PlayerToken); delErr != nil {
			slog.WarnContext(ctx, "failed to delete player token", "error", delErr)
		}

		return &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     existingSession.ID,
			CharacterName: selectedChar.Name,
			Reattached:    true,
		}, nil
	}

	// Create new session.
	sessionID := s.newSessionID()
	connID := core.NewULID()

	now := time.Now()
	ttlSeconds := int(s.sessionDefaults.TTL.Seconds())
	if ttlSeconds <= 0 {
		ttlSeconds = 1800
	}
	maxHistory := s.sessionDefaults.MaxHistory
	if maxHistory <= 0 {
		maxHistory = 500
	}

	locationID := ulid.ULID{}
	if selectedChar.LocationID != nil {
		locationID = *selectedChar.LocationID
	}

	sessionInfo := &session.Info{
		ID:            sessionID.String(),
		CharacterID:   charID,
		CharacterName: selectedChar.Name,
		LocationID:    locationID,
		Status:        session.StatusActive,
		GridPresent:   true,
		EventCursors:  map[string]ulid.ULID{},
		TTLSeconds:    ttlSeconds,
		MaxHistory:    maxHistory,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.sessionStore.Set(ctx, sessionID.String(), sessionInfo); err != nil {
		return nil, oops.Code("SESSION_CREATE_FAILED").Wrap(err)
	}

	s.sessions.Connect(charID, connID)

	// Emit arrive event (best-effort).
	char := core.CharacterRef{ID: charID, Name: selectedChar.Name, LocationID: locationID}
	if err := s.engine.HandleConnect(ctx, char); err != nil {
		slog.WarnContext(ctx, "arrive event failed", "error", err)
	}

	// Delete the player token (one-time use).
	if delErr := s.playerTokenRepo.DeleteByToken(ctx, req.PlayerToken); delErr != nil {
		slog.WarnContext(ctx, "failed to delete player token", "error", delErr)
	}

	return &corev1.SelectCharacterResponse{
		Success:       true,
		SessionId:     sessionID.String(),
		CharacterName: selectedChar.Name,
		Reattached:    false,
	}, nil
}

// CreatePlayer creates a new player account.
func (s *CoreServer) CreatePlayer(ctx context.Context, req *corev1.CreatePlayerRequest) (*corev1.CreatePlayerResponse, error) {
	if s.authService == nil {
		return &corev1.CreatePlayerResponse{
			Success:      false,
			ErrorMessage: "registration not configured",
		}, nil
	}

	player, playerToken, createErr := s.authService.CreatePlayer(ctx, req.Username, req.Password, req.Email)
	if createErr != nil {
		//nolint:nilerr // intentional: return user-facing error in response body
		return &corev1.CreatePlayerResponse{
			Success:      false,
			ErrorMessage: createErr.Error(),
		}, nil
	}

	if s.playerTokenRepo != nil {
		if err := s.playerTokenRepo.Create(ctx, playerToken); err != nil {
			return nil, oops.Code("TOKEN_STORE_FAILED").
				With("player_id", player.ID.String()).
				Wrap(err)
		}
	}

	return &corev1.CreatePlayerResponse{
		Success:     true,
		PlayerToken: playerToken.Token,
		Characters:  []*corev1.CharacterSummary{}, // new player has no characters
	}, nil
}

// CreateCharacter creates a new character for an authenticated player.
func (s *CoreServer) CreateCharacter(ctx context.Context, req *corev1.CreateCharacterRequest) (*corev1.CreateCharacterResponse, error) {
	if s.playerTokenRepo == nil {
		return &corev1.CreateCharacterResponse{
			Success: false, ErrorMessage: "character creation not configured",
		}, nil
	}
	token, tokenErr := s.playerTokenRepo.GetByToken(ctx, req.PlayerToken)
	if tokenErr != nil {
		//nolint:nilerr // intentional: return user-facing error in response body
		return &corev1.CreateCharacterResponse{
			Success:      false,
			ErrorMessage: "invalid player token",
		}, nil
	}

	if token.IsExpired() {
		return &corev1.CreateCharacterResponse{
			Success:      false,
			ErrorMessage: "player token expired",
		}, nil
	}

	if s.characterService == nil {
		return &corev1.CreateCharacterResponse{
			Success:      false,
			ErrorMessage: "character creation not configured",
		}, nil
	}

	char, createErr := s.characterService.Create(ctx, token.PlayerID, req.CharacterName)
	if createErr != nil {
		//nolint:nilerr // intentional: return user-facing error in response body
		return &corev1.CreateCharacterResponse{
			Success:      false,
			ErrorMessage: createErr.Error(),
		}, nil
	}

	return &corev1.CreateCharacterResponse{
		Success:       true,
		CharacterId:   char.ID.String(),
		CharacterName: char.Name,
	}, nil
}

// ListCharacters returns characters for an authenticated player.
func (s *CoreServer) ListCharacters(ctx context.Context, req *corev1.ListCharactersRequest) (*corev1.ListCharactersResponse, error) {
	if s.playerTokenRepo == nil {
		return &corev1.ListCharactersResponse{}, nil
	}
	token, tokenErr := s.playerTokenRepo.GetByToken(ctx, req.PlayerToken)
	if tokenErr != nil {
		//nolint:nilerr // intentional: invalid token returns empty list
		return &corev1.ListCharactersResponse{}, nil
	}

	if token.IsExpired() {
		return &corev1.ListCharactersResponse{}, nil
	}

	characters, err := s.buildCharacterSummaries(ctx, token.PlayerID)
	if err != nil {
		return nil, oops.Code("CHARACTER_LIST_FAILED").Wrap(err)
	}

	return &corev1.ListCharactersResponse{
		Characters: characters,
	}, nil
}

// RequestPasswordReset handles password reset requests.
// Always returns success to prevent email enumeration.
func (s *CoreServer) RequestPasswordReset(ctx context.Context, req *corev1.RequestPasswordResetRequest) (*corev1.RequestPasswordResetResponse, error) {
	if s.resetService != nil {
		token, err := s.resetService.RequestReset(ctx, req.Email)
		if err != nil {
			slog.WarnContext(ctx, "password reset request failed", "error", err)
		} else if token != "" {
			slog.InfoContext(ctx, "password reset token generated", "token", token)
		}
	}

	return &corev1.RequestPasswordResetResponse{
		Success: true,
	}, nil
}

// ConfirmPasswordReset resets a password using a valid reset token.
func (s *CoreServer) ConfirmPasswordReset(ctx context.Context, req *corev1.ConfirmPasswordResetRequest) (*corev1.ConfirmPasswordResetResponse, error) {
	if s.resetService == nil {
		return &corev1.ConfirmPasswordResetResponse{
			Success:      false,
			ErrorMessage: "password reset not configured",
		}, nil
	}

	if resetErr := s.resetService.ResetPassword(ctx, req.Token, req.NewPassword); resetErr != nil {
		//nolint:nilerr // intentional: return user-facing error in response body
		return &corev1.ConfirmPasswordResetResponse{
			Success:      false,
			ErrorMessage: resetErr.Error(),
		}, nil
	}

	return &corev1.ConfirmPasswordResetResponse{
		Success: true,
	}, nil
}

// Logout ends a web session.
func (s *CoreServer) Logout(ctx context.Context, req *corev1.LogoutRequest) (*corev1.LogoutResponse, error) {
	if s.authService == nil {
		return nil, oops.Code("NOT_CONFIGURED").Errorf("auth service not configured")
	}

	sessionID, err := ulid.Parse(req.SessionId)
	if err != nil {
		return nil, oops.Code("INVALID_SESSION_ID").
			With("session_id", req.SessionId).
			Errorf("invalid session ID")
	}

	if err := s.authService.Logout(ctx, sessionID); err != nil {
		return nil, oops.Code("LOGOUT_FAILED").
			With("session_id", req.SessionId).
			Wrap(err)
	}

	return &corev1.LogoutResponse{}, nil
}

// buildCharacterSummaries lists characters for a player and enriches with session status.
func (s *CoreServer) buildCharacterSummaries(ctx context.Context, playerID ulid.ULID) ([]*corev1.CharacterSummary, error) {
	if s.charRepo == nil {
		return nil, nil
	}

	chars, err := s.charRepo.ListByPlayer(ctx, playerID)
	if err != nil {
		return nil, oops.With("player_id", playerID.String()).Wrap(err)
	}

	// Get active sessions for cross-reference (best-effort).
	playerSessions, listErr := s.sessionStore.ListByPlayer(ctx, playerID)
	if listErr != nil {
		slog.WarnContext(ctx, "failed to list player sessions", "error", listErr)
	}
	sessionByChar := make(map[ulid.ULID]*session.Info, len(playerSessions))
	for _, sess := range playerSessions {
		sessionByChar[sess.CharacterID] = sess
	}

	summaries := make([]*corev1.CharacterSummary, 0, len(chars))
	for _, c := range chars {
		summary := &corev1.CharacterSummary{
			CharacterId:   c.ID.String(),
			CharacterName: c.Name,
		}

		if c.LocationID != nil {
			summary.LastLocation = c.LocationID.String()
		}

		if sess, ok := sessionByChar[c.ID]; ok {
			summary.HasActiveSession = sess.Status == session.StatusActive
			summary.SessionStatus = string(sess.Status)
		}

		summaries = append(summaries, summary)
	}

	return summaries, nil
}
