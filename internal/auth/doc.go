// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package auth provides authentication primitives for HoloMUSH.
//
// # Domain Types
//
// Domain types (Player, PlayerSession, PasswordReset) should be created
// using their respective constructors:
//   - NewPlayer - creates a Player with validated username and password hash
//   - NewPlayerSession - creates a PlayerSession with validated player and token hash
//   - NewPasswordReset - creates a PasswordReset with validated player and expiry
//
// Direct struct initialization bypasses validation and may create invalid state.
// Repository implementations receive pre-validated types from these constructors.
//
// # Services
//
// Service types coordinate domain operations:
//   - AuthService - login, logout, session management
//   - CharacterService - character creation with validation
//   - PasswordResetService - password reset flow
//
// Services are created with New*Service constructors that validate dependencies.
package auth
