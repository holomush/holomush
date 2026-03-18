// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	grpcserver "github.com/holomush/holomush/internal/grpc"
)

// NameTheme generates two-part themed names.
type NameTheme interface {
	Name() string
	Generate() (firstName, secondName string)
}

var gemstones = []string{
	"Amber", "Amethyst", "Beryl", "Coral", "Diamond",
	"Emerald", "Garnet", "Jade", "Jasper", "Lapis",
	"Moonstone", "Obsidian", "Onyx", "Opal", "Pearl",
	"Quartz", "Ruby", "Sapphire", "Topaz", "Turquoise",
}

var elements = []string{
	"Argon", "Boron", "Carbon", "Cobalt", "Copper",
	"Gold", "Helium", "Iodine", "Iron", "Krypton",
	"Neon", "Nickel", "Osmium", "Radium", "Radon",
	"Silver", "Titanium", "Xenon", "Zinc", "Zircon",
}

// GemstoneElementTheme generates names like "Amber_Argon".
type GemstoneElementTheme struct{}

// NewGemstoneElementTheme creates a new GemstoneElementTheme.
func NewGemstoneElementTheme() *GemstoneElementTheme {
	return &GemstoneElementTheme{}
}

// Name returns the theme identifier.
func (t *GemstoneElementTheme) Name() string {
	return "gemstone_element"
}

// Generate returns a random (gemstone, element) pair.
func (t *GemstoneElementTheme) Generate() (firstName, secondName string) {
	return gemstones[rand.IntN(len(gemstones))], elements[rand.IntN(len(elements))] //nolint:gosec // non-security name generation
}

// GuestAuthenticator implements grpc.Authenticator for guest logins.
type GuestAuthenticator struct {
	theme         NameTheme
	startLocation ulid.ULID
	mu            sync.Mutex
	active        map[string]struct{}
}

// Ensure GuestAuthenticator satisfies grpc.Authenticator at compile time.
var _ grpcserver.Authenticator = (*GuestAuthenticator)(nil)

// NewGuestAuthenticator creates a GuestAuthenticator with the given theme and start location.
func NewGuestAuthenticator(theme NameTheme, startLocation ulid.ULID) *GuestAuthenticator {
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
		CharacterID:   ulid.Make(),
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
