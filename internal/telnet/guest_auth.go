// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"context"
	"fmt"
	"sync"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	grpcserver "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/naming"
)

// GuestAuthenticator implements grpc.Authenticator for guest logins.
type GuestAuthenticator struct {
	theme         naming.Theme
	startLocation ulid.ULID
	mu            sync.Mutex
	active        map[string]struct{}
}

// Ensure GuestAuthenticator satisfies grpc.Authenticator at compile time.
var _ grpcserver.Authenticator = (*GuestAuthenticator)(nil)

// NewGuestAuthenticator creates a GuestAuthenticator with the given theme and start location.
func NewGuestAuthenticator(theme naming.Theme, startLocation ulid.ULID) *GuestAuthenticator {
	return &GuestAuthenticator{
		theme:         theme,
		startLocation: startLocation,
		active:        make(map[string]struct{}),
	}
}

// Authenticate handles guest logins. Only "guest" username is accepted.
func (a *GuestAuthenticator) Authenticate(_ context.Context, username, _ string) (*grpcserver.AuthResult, error) {
	if username != "guest" {
		return nil, oops.Errorf("Registered accounts are not yet available. Use `connect guest` to play.")
	}

	name, err := a.generateUniqueName()
	if err != nil {
		return nil, err
	}

	return &grpcserver.AuthResult{
		CharacterID:   idgen.New(),
		CharacterName: name,
		LocationID:    a.startLocation,
		IsGuest:       true,
	}, nil
}

// ActiveCount returns the number of currently active guest names.
func (a *GuestAuthenticator) ActiveCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.active)
}

// ReleaseGuest removes the name from the active set, freeing it for reuse.
func (a *GuestAuthenticator) ReleaseGuest(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.active, name)
}

// generateUniqueName tries up to 50 times to find an unused themed name.
func (a *GuestAuthenticator) generateUniqueName() (string, error) {
	for range 50 {
		first, second := a.theme.Generate()
		name := fmt.Sprintf("%s_%s", first, second)

		a.mu.Lock()
		_, taken := a.active[name]
		if !taken {
			a.active[name] = struct{}{}
			a.mu.Unlock()
			return name, nil
		}
		a.mu.Unlock()
	}
	return "", oops.Errorf("unable to generate unique guest name after 50 attempts")
}
