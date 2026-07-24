// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// authFailureToStatus translates known auth-failure oops codes into a
// codes.Unauthenticated gRPC status, so the client side can distinguish
// auth-failure (legit expired/unknown cookie) from transport/lookup
// failures across the wire.
//
// Returns:
//   - non-nil status.Error iff err is a known auth-failure oops code
//   - nil otherwise (caller should fall through with original err)
func authFailureToStatus(err error) error {
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return nil
	}
	code, ok := oopsErr.Code().(string)
	if !ok {
		return nil
	}
	switch code {
	case "PLAYER_SESSION_NOT_FOUND",
		"PLAYER_SESSION_EXPIRED",
		"SESSION_NOT_FOUND":
		return status.Errorf(codes.Unauthenticated, "%s", oopsErr.Error())
	}
	return nil
}

// AuthServiceProvider defines the auth.Service methods used by auth handlers.
type AuthServiceProvider interface {
	ValidateCredentials(ctx context.Context, username, password string) (*auth.Player, error)
	AuthenticatePlayer(ctx context.Context, username, password, userAgent, ipAddress string) (string, *auth.Player, error)
	CreatePlayer(ctx context.Context, username, password, email string) (*auth.Player, *auth.PlayerSession, string, error)
	Logout(ctx context.Context, tokenHash string) (ulid.ULID, error)
}

// CharacterServiceProvider defines the auth.CharacterService methods used by auth
// handlers. Registered gRPC creation uses CreateBound, which commits the
// character + initial_bind binding + genesis envelope atomically through the
// genesis service (05-15) — there is no envelope-less degraded path.
type CharacterServiceProvider interface {
	CreateBound(ctx context.Context, playerID ulid.ULID, name, bindReason string) (*world.Character, error)
}

// ResetServiceProvider defines the auth.PasswordResetService methods used by auth handlers.
type ResetServiceProvider interface {
	RequestReset(ctx context.Context, email string) (string, error)
	ResetPassword(ctx context.Context, token, newPassword string) error
}

// BindingRepo provides player↔character binding lookup used by the Subscribe and
// QueryStreamHistory handlers (Current). Binding CREATION at character-creation
// time is owned by the genesis service (05-15), not the handler.
type BindingRepo interface {
	// Current returns the active binding_id for characterID.
	// Returns BINDING_NOT_FOUND if no active binding exists.
	Current(ctx context.Context, characterID string) (string, error)
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

// WithPlayerSessionRepo sets the player session repository.
func WithPlayerSessionRepo(repo auth.PlayerSessionRepository) CoreServerOption {
	return func(s *CoreServer) {
		s.playerSessionRepo = repo
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

// WithGuestService sets the guest creation service.
func WithGuestService(svc *auth.GuestService) CoreServerOption {
	return func(s *CoreServer) {
		s.guestService = svc
	}
}

// WithBindingRepository sets the binding repository for player↔character
// current-binding lookup (Current) used by Subscribe / QueryStreamHistory.
func WithBindingRepository(b BindingRepo) CoreServerOption {
	return func(s *CoreServer) {
		s.bindings = b
	}
}

// WithCryptoActive gates sensitive-event crypto features (binding lookup in
// Subscribe and QueryStreamHistory) on KEK presence. Pass true when
// RekeyManager != nil (wired via cryptoActiveFor in the gRPC subsystem).
// Default false so KEK-less deployments skip binding resolution rather than
// failing with BINDING_NOT_FOUND.
func WithCryptoActive(active bool) CoreServerOption {
	return func(s *CoreServer) {
		s.cryptoActive = active
	}
}

// isPlayerSessionAuthError reports whether err is a user-facing authentication
// failure (session not found, expired, or service not configured) as opposed to
// an infrastructure error (e.g., database unavailable).
func isPlayerSessionAuthError(err error) bool {
	if errors.Is(err, auth.ErrNotFound) {
		return true
	}
	oopsErr, isOops := oops.AsOops(err)
	if !isOops {
		return false
	}
	switch oopsErr.Code() {
	case "PLAYER_SESSION_NOT_FOUND", "PLAYER_SESSION_EXPIRED", "NOT_CONFIGURED":
		return true
	}
	return false
}

// resolvePlayerSessionWithRepo looks up a PlayerSession by raw token, validates
// it, and refreshes the TTL. It is the package-level implementation shared by
// CoreServer and SceneAccessServer (same package). Returns the session or an error.
func resolvePlayerSessionWithRepo(ctx context.Context, repo auth.PlayerSessionRepository, rawToken string) (*auth.PlayerSession, error) {
	if repo == nil {
		return nil, oops.Code("NOT_CONFIGURED").Errorf("player session service not configured")
	}
	tokenHash := auth.HashSessionToken(rawToken)
	ps, err := repo.GetByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, err //nolint:wrapcheck // intentional: preserve repository error codes (PLAYER_SESSION_NOT_FOUND / PLAYER_SESSION_EXPIRED)
	}
	// Best-effort TTL refresh — intentionally ignore errors.
	repo.RefreshTTL(ctx, ps.ID, auth.PlayerSessionTTL) //nolint:errcheck // best-effort
	return ps, nil
}

// resolvePlayerSession looks up a PlayerSession by raw token, validates it,
// and refreshes the TTL. Returns the session or an error.
func (s *CoreServer) resolvePlayerSession(ctx context.Context, rawToken string) (*auth.PlayerSession, error) {
	return resolvePlayerSessionWithRepo(ctx, s.playerSessionRepo, rawToken)
}

// AuthenticatePlayer validates credentials and returns a player session token for character selection.
func (s *CoreServer) AuthenticatePlayer(ctx context.Context, req *corev1.AuthenticatePlayerRequest) (*corev1.AuthenticatePlayerResponse, error) {
	slog.DebugContext(ctx, "grpc: AuthenticatePlayer", "username", req.GetUsername())

	if s.authService == nil {
		return &corev1.AuthenticatePlayerResponse{
			Success:      false,
			ErrorMessage: "authentication not configured",
		}, nil
	}
	if s.playerSessionRepo == nil {
		return &corev1.AuthenticatePlayerResponse{
			Success:      false,
			ErrorMessage: "player session service not configured",
		}, nil
	}

	// AuthenticatePlayer validates credentials, enforces the per-player session
	// cap (evicting the oldest session if needed), and persists a new
	// PlayerSession in a single service call.
	rawToken, player, authErr := s.authService.AuthenticatePlayer(ctx, req.Username, req.Password, "", "")
	if authErr != nil {
		//nolint:nilerr // intentional: return user-facing error in response body
		return &corev1.AuthenticatePlayerResponse{
			Success:      false,
			ErrorMessage: "invalid username or password",
		}, nil
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
		PlayerSessionToken: rawToken,
		Characters:         characters,
		DefaultCharacterId: defaultCharID,
		SessionTtlSeconds:  int64(auth.PlayerSessionTTL.Seconds()),
	}, nil
}

// SelectCharacter validates a player session, creates or reattaches a game session.
// TODO: Thread connID through SelectCharacterResponse proto once the field is added.
func (s *CoreServer) SelectCharacter(ctx context.Context, req *corev1.SelectCharacterRequest) (*corev1.SelectCharacterResponse, error) {
	slog.DebugContext(ctx, "grpc: SelectCharacter", "character_id", req.GetCharacterId())

	playerSession, err := s.resolvePlayerSession(ctx, req.GetPlayerSessionToken())
	if err != nil {
		if isPlayerSessionAuthError(err) {
			return &corev1.SelectCharacterResponse{
				Success: false, ErrorMessage: "invalid or expired player session",
			}, nil
		}
		return nil, err
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
	chars, err := s.charRepo.ListByPlayer(ctx, playerSession.PlayerID)
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

	// Determine the guest temporal floor for INV-PRIVACY-2 BEFORE the reattach
	// branch — both fresh and reattach paths need GuestCharacterCreatedAt
	// so a reattach to a pre-iwzt-T5 session can backfill the floor.
	// Best-effort: if the player lookup fails (or playerRepo is
	// unconfigured) we log a warning but do not block session creation —
	// the floor is left at zero (no guest overlay).
	//
	// Note: we intentionally DO NOT set session.Info.IsGuest=true here.
	// The IsGuest flag is also read by Disconnect at server.go:1260 to
	// trigger immediate session deletion, which breaks page-reload
	// reattach. The non-zero GuestCharacterCreatedAt timestamp is the
	// guest-overlay signal used by streamScopeFloor; redesigning the
	// disconnect path is tracked as a separate follow-up.
	var guestCharCreatedAt time.Time
	if s.playerRepo != nil {
		player, playerErr := s.playerRepo.GetByID(ctx, playerSession.PlayerID)
		if playerErr != nil {
			slog.WarnContext(ctx, "select_character: player lookup failed; guest floor will be zero",
				"player_id", playerSession.PlayerID.String(), "error", playerErr)
		} else if player.IsGuest {
			guestCharCreatedAt = selectedChar.CreatedAt
		}
	}

	// Check for existing session to reattach.
	existingSession, findErr := s.sessionStore.FindByCharacter(ctx, charID)
	if findErr != nil {
		oopsErr, isOops := oops.AsOops(findErr)
		if !isOops || oopsErr.Code() != "SESSION_NOT_FOUND" {
			return nil, oops.Code("SESSION_LOOKUP_FAILED").Wrap(findErr)
		}
	}
	if findErr == nil && existingSession != nil {
		// Reattach: the session row was held open across a transport
		// disconnect (status=Detached, TTL window in `expires_at`). The
		// session is the SAME continuing session — its LocationArrivedAt
		// MUST NOT be reset. Per spec §2 (post-2026-05-17 amendment): only
		// session-create and character-move advance the floor; reattach
		// within TTL preserves it so the user's own pre-disconnect
		// scrollback survives page reload, WiFi blip, and tmux-style
		// telnet reattach. A genuinely-absent character whose session
		// expires past TTL gets a fresh floor via §5 row 1
		// (SelectCharacter creates a new session row).
		now := time.Now()
		if updateErr := s.sessionStore.UpdateStatus(ctx, existingSession.ID,
			session.StatusActive, nil, nil); updateErr != nil {
			slog.WarnContext(ctx, "failed to reactivate session", "error", updateErr)
		}
		existingSession.Status = session.StatusActive
		existingSession.UpdatedAt = now

		// In-memory backfill of guest floor if absent (session created
		// pre-iwzt-T5). NOT persisted here: calling sessionStore.Set would
		// overwrite the just-issued UpdateStatus(active) clearing of
		// detached_at/expires_at because the in-memory existingSession still
		// carries the stale pre-reattach values for those fields. A targeted
		// SetGuestMetadata store method is the proper persistent backfill —
		// tracked as holomush-omy8. The temporal floor for QueryStreamHistory
		// uses the in-memory session.Info, so the response still gets the
		// right guest overlay even without persistence.
		if !guestCharCreatedAt.IsZero() && existingSession.GuestCharacterCreatedAt.IsZero() {
			existingSession.GuestCharacterCreatedAt = guestCharCreatedAt
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
		ID:                sessionID.String(),
		CharacterID:       charID,
		PlayerID:          playerSession.PlayerID,
		PlayerSessionID:   playerSession.ID,
		CharacterName:     selectedChar.Name,
		LocationID:        locationID,
		LocationArrivedAt: now,
		Status:            session.StatusActive,
		// comms_hub sessions must not appear on the grid: the EXISTS predicate in
		// ListActiveByLocation is the authoritative presence gate, but setting
		// GridPresent=false here keeps the flag consistent with reality and avoids
		// the reaper needing to correct it on first sweep (holomush-5rh.8.9).
		GridPresent:             req.GetClientType() != "comms_hub",
		TTLSeconds:              ttlSeconds,
		MaxHistory:              maxHistory,
		GuestCharacterCreatedAt: guestCharCreatedAt,
		CreatedAt:               now,
		UpdatedAt:               now,
	}

	if err := s.sessionStore.Set(ctx, sessionID.String(), sessionInfo); err != nil {
		return nil, oops.Code("SESSION_CREATE_FAILED").Wrap(err)
	}

	// Emit arrive event (best-effort). Skipped for comms_hub client type:
	// scenes-workspace sessions must not announce the character on the grid
	// (spec 2026-06-07 §V2).
	if req.GetClientType() != "comms_hub" {
		char := core.CharacterRef{ID: charID, Name: selectedChar.Name, LocationID: locationID}
		if err := s.presence.EmitArrive(ctx, char); err != nil {
			slog.WarnContext(ctx, "arrive event failed", "error", err)
		}
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
	slog.DebugContext(ctx, "grpc: CreatePlayer", "username", req.GetUsername())

	if s.authService == nil {
		return &corev1.CreatePlayerResponse{
			Success:      false,
			ErrorMessage: "registration not configured",
		}, nil
	}
	if s.playerSessionRepo == nil {
		return &corev1.CreatePlayerResponse{
			Success:      false,
			ErrorMessage: "player session service not configured",
		}, nil
	}

	player, playerSession, rawToken, createErr := s.authService.CreatePlayer(ctx, req.Username, req.Password, req.Email)
	if createErr != nil {
		// SECURITY: log full error server-side; return sanitized message only.
		// Raw err.Error() on oops errors leaks structured context (operation
		// names, parameter values) including schema/constraint details.
		slog.WarnContext(ctx, "grpc: CreatePlayer failed",
			"username", req.GetUsername(),
			"error", createErr)
		return &corev1.CreatePlayerResponse{
			Success:      false,
			ErrorMessage: sanitizeAuthError(createErr),
		}, nil
	}

	// Persist the player session (repo confirmed non-nil above).
	if err := s.playerSessionRepo.Create(ctx, playerSession); err != nil {
		return nil, oops.Code("SESSION_STORE_FAILED").
			With("player_id", player.ID.String()).
			Wrap(err)
	}

	return &corev1.CreatePlayerResponse{
		Success:            true,
		PlayerSessionToken: rawToken,
		Characters:         []*corev1.CharacterSummary{}, // new player has no characters
		SessionTtlSeconds:  int64(auth.PlayerSessionTTL.Seconds()),
	}, nil
}

// CreateCharacter creates a new character for an authenticated player.
func (s *CoreServer) CreateCharacter(ctx context.Context, req *corev1.CreateCharacterRequest) (*corev1.CreateCharacterResponse, error) {
	slog.DebugContext(ctx, "grpc: CreateCharacter", "character_name", req.GetCharacterName())

	playerSession, err := s.resolvePlayerSession(ctx, req.GetPlayerSessionToken())
	if err != nil {
		if isPlayerSessionAuthError(err) {
			return &corev1.CreateCharacterResponse{
				Success: false, ErrorMessage: "invalid or expired player session",
			}, nil
		}
		return nil, err
	}

	if s.characterService == nil {
		return &corev1.CreateCharacterResponse{
			Success:      false,
			ErrorMessage: "character creation not configured",
		}, nil
	}

	// Character + initial_bind binding + genesis envelope commit atomically inside
	// the genesis service (05-15). There is no envelope-less degraded path: a
	// misconfigured genesis service fails closed with a typed error rather than
	// silently creating an envelope-less character.
	char, createErr := s.characterService.CreateBound(ctx, playerSession.PlayerID, req.CharacterName, "initial_bind")
	if createErr != nil {
		// SECURITY: log full error server-side; return sanitized message only.
		slog.WarnContext(ctx, "grpc: CreateCharacter failed",
			"player_id", playerSession.PlayerID.String(),
			"character_name", req.GetCharacterName(),
			"error", createErr)
		return &corev1.CreateCharacterResponse{
			Success:      false,
			ErrorMessage: sanitizeAuthError(createErr),
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
	slog.DebugContext(ctx, "grpc: ListCharacters")

	playerSession, err := s.resolvePlayerSession(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err // propagates PLAYER_SESSION_NOT_FOUND or PLAYER_SESSION_EXPIRED
	}

	characters, err := s.buildCharacterSummaries(ctx, playerSession.PlayerID)
	if err != nil {
		return nil, oops.Code("CHARACTER_LIST_FAILED").Wrap(err)
	}

	return &corev1.ListCharactersResponse{Characters: characters}, nil
}

// ListAllCharacters returns id+name for every character in the directory
// (fetch-all for the directory picker). The acting character (identified by
// CharacterId) must belong to the calling player session — this prevents
// ABAC-subject spoofing. ABAC gate: action=list_character_directory on
// resource=character_directory:all; seeded default-permit for any
// authenticated character including guests (INV-ACCESS-9). Connection/online
// state is excluded — it requires a separate, more-restrictive permission.
func (s *CoreServer) ListAllCharacters(ctx context.Context, req *corev1.ListAllCharactersRequest) (*corev1.ListAllCharactersResponse, error) {
	ps, err := s.resolvePlayerSession(ctx, req.GetPlayerSessionToken())
	if err != nil {
		if st := authFailureToStatus(err); st != nil {
			return nil, st // Unauthenticated on bad/missing/expired token
		}
		slog.ErrorContext(ctx, "core: list-directory session resolve failed", "error", err)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	// Ownership: the acting character must belong to this session to prevent
	// ABAC-subject spoofing (a caller cannot assert an arbitrary character ID).
	charID, err := ulid.Parse(req.GetCharacterId())
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "character not found")
	}
	ownedChars, err := s.charRepo.ListByPlayer(ctx, ps.PlayerID)
	if err != nil {
		slog.ErrorContext(ctx, "core: list-directory ownership lookup failed", "error", err)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	owned := false
	for _, c := range ownedChars {
		if c.ID == charID {
			owned = true
			break
		}
	}
	if !owned {
		return nil, status.Errorf(codes.NotFound, "character not found")
	}

	// Fail-closed: a nil ABAC engine means default-deny. Production wire-up
	// (cmd/holomush/sub_grpc.go) always sets it, but tests and future
	// construction sites might not — mirrors list_focus_presence.go:100-107.
	if s.accessEngine == nil {
		slog.ErrorContext(ctx, "core: list-directory access engine not configured",
			"character_id", req.GetCharacterId())
		return nil, status.Errorf(codes.PermissionDenied, "not permitted to list the character directory")
	}

	// ABAC gate — mirrors list_focus_presence.go:116-137.
	accessReq, err := types.NewAccessRequest(
		access.CharacterSubject(req.GetCharacterId()),
		"list_character_directory",
		access.CharacterDirectoryResource(),
		nil,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	decision, err := s.accessEngine.Evaluate(ctx, accessReq)
	if err != nil {
		slog.ErrorContext(ctx, "core: list-directory ABAC error", "error", err)
		return nil, status.Errorf(codes.Internal, "internal error")
	}
	if !decision.IsAllowed() {
		return nil, status.Errorf(codes.PermissionDenied, "not permitted to list the character directory")
	}

	allChars, err := s.charRepo.ListAll(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "core: list all characters failed", "error", err)
		return nil, status.Errorf(codes.Internal, "internal error")
	}

	out := make([]*corev1.CharacterDirectoryEntry, 0, len(allChars))
	for _, c := range allChars {
		out = append(out, &corev1.CharacterDirectoryEntry{
			CharacterId: c.ID.String(),
			Name:        c.Name,
		})
	}
	return &corev1.ListAllCharactersResponse{Characters: out}, nil
}

// RequestPasswordReset handles password reset requests.
// Always returns success to prevent email enumeration.
func (s *CoreServer) RequestPasswordReset(ctx context.Context, req *corev1.RequestPasswordResetRequest) (*corev1.RequestPasswordResetResponse, error) {
	if s.resetService != nil {
		token, err := s.resetService.RequestReset(ctx, req.Email)
		if err != nil {
			slog.WarnContext(ctx, "password reset request failed", "error", err)
		} else if token != "" {
			// SECURITY: Never log the token value — it grants password reset access.
			slog.InfoContext(ctx, "password reset token generated", "email", req.Email)
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
		// SECURITY: log full error server-side; return sanitized message only.
		// Never log the raw token value.
		slog.WarnContext(ctx, "grpc: ConfirmPasswordReset failed", "error", resetErr)
		return &corev1.ConfirmPasswordResetResponse{
			Success:      false,
			ErrorMessage: sanitizeAuthError(resetErr),
		}, nil
	}

	return &corev1.ConfirmPasswordResetResponse{
		Success: true,
	}, nil
}

// Logout ends a player session.
//
// Before invoking authService.Logout (which deletes the PlayerSession row),
// the handler enumerates child game sessions via sessionStore.ListByPlayerSession
// and emits HandleDisconnect + EndSession(cause=logout) + Delete + hooks for
// each. This closes the "orphaned Subscribe on logout" gap documented in the
// session lifecycle spec (§ Logout & eviction fanout).
//
// Ordering invariant: per-session signals complete before PlayerSession deletion
// so ValidateSessionOwnership (which runs at Subscribe open time) does not flap
// mid-fanout.
func (s *CoreServer) Logout(ctx context.Context, req *corev1.LogoutRequest) (*corev1.LogoutResponse, error) {
	// Hash the raw token before logging — never log the live bearer credential.
	tokenHash := auth.HashSessionToken(req.PlayerSessionToken)
	slog.DebugContext(ctx, "grpc: Logout", "token_hash_prefix", tokenHash[:16])

	if s.authService == nil {
		return nil, oops.Code("NOT_CONFIGURED").Errorf("auth service not configured")
	}

	// Fanout: enumerate child game sessions before deleting the PlayerSession.
	// If playerSessionRepo is not configured we skip fanout gracefully.
	if s.playerSessionRepo != nil {
		ps, lookupErr := s.playerSessionRepo.GetByTokenHash(ctx, tokenHash)
		if lookupErr != nil {
			slog.WarnContext(ctx, "logout: player session lookup failed — proceeding without fanout",
				"token_hash_prefix", tokenHash[:16], "error", lookupErr)
		} else {
			childSessions, listErr := s.sessionStore.ListByPlayerSession(ctx, []ulid.ULID{ps.ID})
			if listErr != nil {
				slog.WarnContext(ctx, "logout: list child sessions failed — proceeding without fanout",
					"player_session_id", ps.ID.String(), "error", listErr)
			}
			for _, info := range childSessions {
				char := core.CharacterRef{
					ID:         info.CharacterID,
					Name:       info.CharacterName,
					LocationID: info.LocationID,
				}
				if dcErr := s.presence.EmitLeave(ctx, char, "logout"); dcErr != nil {
					slog.WarnContext(ctx, "logout: leave event failed",
						"session_id", info.ID, "error", dcErr)
				}
				if endErr := s.presence.EmitSessionEnded(ctx, char, info.ID,
					core.SessionEndedCauseLogout, "Session ended by logout."); endErr != nil {
					slog.WarnContext(ctx, "logout: session_ended event failed",
						"session_id", info.ID, "error", endErr)
				}
				if delErr := s.sessionStore.Delete(ctx, info.ID); delErr != nil {
					slog.WarnContext(ctx, "logout: game session delete failed",
						"session_id", info.ID, "error", delErr)
				}
				s.lifecycleHandler.runDisconnectHooks(ctx, *info)
			}
		}
	}

	if _, err := s.authService.Logout(ctx, tokenHash); err != nil {
		return nil, oops.Code("LOGOUT_FAILED").
			With("token_hash_prefix", tokenHash[:16]).
			Wrap(err)
	}

	return &corev1.LogoutResponse{}, nil
}

// CheckPlayerSession validates a player session token and returns the player +
// characters. Failure-path contract (nil, err with PLAYER_SESSION_NOT_FOUND /
// PLAYER_SESSION_EXPIRED) is preserved exactly. See spec §4.3 / §4.3.1.
func (s *CoreServer) CheckPlayerSession(ctx context.Context, req *corev1.CheckPlayerSessionRequest) (*corev1.CheckPlayerSessionResponse, error) {
	slog.DebugContext(ctx, "grpc: CheckPlayerSession")

	// resolvePlayerSession does the token-hash lookup and the best-effort
	// RefreshTTL — TTL refresh on the success path is inherited.
	ps, err := s.resolvePlayerSession(ctx, req.GetPlayerSessionToken())
	if err != nil {
		// Translate known auth-failure oops codes to codes.Unauthenticated
		// so the client wrapper can re-inject an oops auth-failure code
		// without the gRPC client's generic RPC_FAILED wrap hiding it.
		// Infrastructure failures (NOT_CONFIGURED, etc.) fall through
		// unchanged — gRPC will surface them as codes.Unknown.
		if authErr := authFailureToStatus(err); authErr != nil {
			return nil, authErr
		}
		return nil, err
	}

	if s.playerRepo == nil {
		return nil, oops.Code("NOT_CONFIGURED").Errorf("player repository not configured")
	}

	player, err := s.playerRepo.GetByID(ctx, ps.PlayerID)
	if err != nil {
		return nil, oops.Code("PLAYER_LOOKUP_FAILED").Wrap(err)
	}

	characters, err := s.buildCharacterSummaries(ctx, player.ID)
	if err != nil {
		return nil, oops.Code("CHARACTER_LOOKUP_FAILED").Wrap(err)
	}

	return &corev1.CheckPlayerSessionResponse{
		PlayerName: player.Username,
		PlayerId:   player.ID.String(),
		IsGuest:    player.IsGuest,
		Characters: characters,
	}, nil
}

// CreateGuest creates an ephemeral guest player and character.
func (s *CoreServer) CreateGuest(ctx context.Context, _ *corev1.CreateGuestRequest) (*corev1.CreateGuestResponse, error) {
	slog.DebugContext(ctx, "grpc: CreateGuest")

	if s.guestService == nil {
		return &corev1.CreateGuestResponse{
			Success:      false,
			ErrorMessage: "guest login not configured",
		}, nil
	}

	result, err := s.guestService.CreateGuest(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "grpc: guest creation failed", "error", err)
		return &corev1.CreateGuestResponse{
			Success:      false,
			ErrorMessage: "guest creation failed",
		}, nil
	}

	charSummary := &corev1.CharacterSummary{
		CharacterId:   result.Character.ID.String(),
		CharacterName: result.Character.Name,
	}

	return &corev1.CreateGuestResponse{
		Success:            true,
		PlayerSessionToken: result.RawToken,
		Characters:         []*corev1.CharacterSummary{charSummary},
		DefaultCharacterId: result.Character.ID.String(),
		SessionTtlSeconds:  int64(auth.GuestSessionTTL.Seconds()),
	}, nil
}

// ListPlayerSessions returns the caller's active PlayerSessions with metadata.
// Tokens are never included in the response - each PlayerSessionInfo is
// identified by its ULID only. Exactly one entry has is_current=true (the
// session that made this request) to support "this device" UX.
//
// SECURITY: On any auth failure (invalid token, expired session, repo error)
// returns an empty list - callers cannot distinguish "invalid token" from
// "player has zero sessions", preventing enumeration.
func (s *CoreServer) ListPlayerSessions(ctx context.Context, req *corev1.ListPlayerSessionsRequest) (*corev1.ListPlayerSessionsResponse, error) {
	if s.playerSessionRepo == nil {
		return &corev1.ListPlayerSessionsResponse{}, nil
	}

	caller, err := s.playerSessionRepo.GetByTokenHash(ctx, auth.HashSessionToken(req.GetPlayerSessionToken()))
	if err != nil || caller.IsExpired() {
		// SECURITY: empty response on auth failure is the enumeration-safe
		// signal - callers cannot distinguish "invalid token" from "player
		// has 0 sessions".
		return &corev1.ListPlayerSessionsResponse{}, nil //nolint:nilerr // intentional: enumeration-safe auth-failure response
	}
	// Best-effort TTL refresh — active session management keeps the session alive.
	s.playerSessionRepo.RefreshTTL(ctx, caller.ID, auth.PlayerSessionTTL) //nolint:errcheck // best-effort

	sessions, err := s.playerSessionRepo.ListByPlayer(ctx, caller.PlayerID)
	if err != nil {
		return nil, oops.Code("LIST_PLAYER_SESSIONS_FAILED").Wrap(err)
	}

	out := make([]*corev1.PlayerSessionInfo, 0, len(sessions))
	for _, ps := range sessions {
		out = append(out, &corev1.PlayerSessionInfo{
			Id:         ps.ID.String(),
			CreatedAt:  timestamppb.New(ps.CreatedAt),
			LastActive: timestamppb.New(ps.UpdatedAt),
			UserAgent:  ps.UserAgent,
			IpAddress:  ps.IPAddress,
			IsCurrent:  ps.ID.Compare(caller.ID) == 0,
		})
	}
	return &corev1.ListPlayerSessionsResponse{Sessions: out}, nil
}

// RevokePlayerSession deletes a specific PlayerSession owned by the caller.
// Attempts to revoke another player's session return SESSION_NOT_FOUND (same
// enumeration-prevention pattern as the post-auth ownership fixes). Cross-
// player attempts log WARN for security auditing.
func (s *CoreServer) RevokePlayerSession(ctx context.Context, req *corev1.RevokePlayerSessionRequest) (*corev1.RevokePlayerSessionResponse, error) {
	if s.playerSessionRepo == nil {
		return &corev1.RevokePlayerSessionResponse{Success: false, ErrorMessage: "session not found"}, nil
	}

	caller, err := s.playerSessionRepo.GetByTokenHash(ctx, auth.HashSessionToken(req.GetPlayerSessionToken()))
	if err != nil || caller.IsExpired() {
		//nolint:nilerr // intentional: enumeration-safe - all auth failures collapse to "session not found"
		return &corev1.RevokePlayerSessionResponse{Success: false, ErrorMessage: "session not found"}, nil
	}
	// Best-effort TTL refresh — active session management keeps the session alive.
	s.playerSessionRepo.RefreshTTL(ctx, caller.ID, auth.PlayerSessionTTL) //nolint:errcheck // best-effort

	targetID, err := ulid.Parse(req.GetTargetSessionId())
	if err != nil {
		//nolint:nilerr // intentional: enumeration-safe
		return &corev1.RevokePlayerSessionResponse{Success: false, ErrorMessage: "session not found"}, nil
	}

	target, err := s.playerSessionRepo.GetByID(ctx, targetID)
	if err != nil {
		//nolint:nilerr // intentional: enumeration-safe
		return &corev1.RevokePlayerSessionResponse{Success: false, ErrorMessage: "session not found"}, nil
	}
	if target.PlayerID.Compare(caller.PlayerID) != 0 {
		slog.WarnContext(
			ctx, "revoke player session: cross-player attempt",
			"caller_id", caller.PlayerID.String(),
			"target_owner", target.PlayerID.String(),
			"target_id", targetID.String(),
		)
		return &corev1.RevokePlayerSessionResponse{Success: false, ErrorMessage: "session not found"}, nil
	}

	if err := s.playerSessionRepo.Delete(ctx, target.ID); err != nil {
		return nil, oops.Code("REVOKE_PLAYER_SESSION_FAILED").Wrap(err)
	}
	return &corev1.RevokePlayerSessionResponse{Success: true}, nil
}

// RevokeOtherPlayerSessions bulk-revokes all PlayerSessions owned by the
// caller except the current one. Useful after a suspected compromise or
// password reset.
func (s *CoreServer) RevokeOtherPlayerSessions(ctx context.Context, req *corev1.RevokeOtherPlayerSessionsRequest) (*corev1.RevokeOtherPlayerSessionsResponse, error) {
	if s.playerSessionRepo == nil {
		return &corev1.RevokeOtherPlayerSessionsResponse{Success: false}, nil
	}

	caller, err := s.playerSessionRepo.GetByTokenHash(ctx, auth.HashSessionToken(req.GetPlayerSessionToken()))
	if err != nil || caller.IsExpired() {
		//nolint:nilerr // intentional: enumeration-safe auth-failure response
		return &corev1.RevokeOtherPlayerSessionsResponse{Success: false}, nil
	}
	// Best-effort TTL refresh — active session management keeps the session alive.
	s.playerSessionRepo.RefreshTTL(ctx, caller.ID, auth.PlayerSessionTTL) //nolint:errcheck // best-effort

	sessions, err := s.playerSessionRepo.ListByPlayer(ctx, caller.PlayerID)
	if err != nil {
		return nil, oops.Code("REVOKE_OTHER_LIST_FAILED").Wrap(err)
	}

	var count int32
	for _, ps := range sessions {
		if ps.ID.Compare(caller.ID) == 0 {
			continue
		}
		if delErr := s.playerSessionRepo.Delete(ctx, ps.ID); delErr != nil {
			return nil, oops.Code("REVOKE_OTHER_DELETE_FAILED").
				With("target_id", ps.ID.String()).Wrap(delErr)
		}
		count++
	}

	return &corev1.RevokeOtherPlayerSessionsResponse{Success: true, RevokedCount: count}, nil
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

		if c.LocationID != nil && s.worldQuerier != nil {
			subj := access.CharacterSubject(c.ID.String())
			if loc, locErr := s.worldQuerier.GetLocation(ctx, subj, *c.LocationID); locErr == nil && loc != nil {
				summary.LastLocation = loc.Name
			}
		}

		if sess, ok := sessionByChar[c.ID]; ok {
			summary.HasActiveSession = sess.Status == session.StatusActive
			summary.SessionStatus = string(sess.Status)
		}

		summaries = append(summaries, summary)
	}

	return summaries, nil
}
