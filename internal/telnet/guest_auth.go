// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"fmt"
	"sync"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/naming"
)

// GuestAuthenticator generates unique themed guest names and tracks the
// pool of active names so they can be released on disconnect. It is
// consumed by auth.GuestService (for the gRPC CreateGuest path) and the
// session disconnect hook.
type GuestAuthenticator struct {
	theme         naming.Theme
	startLocation ulid.ULID
	mu            sync.Mutex
	active        map[string]struct{}
}

// NewGuestAuthenticator creates a GuestAuthenticator with the given theme and start location.
func NewGuestAuthenticator(theme naming.Theme, startLocation ulid.ULID) *GuestAuthenticator {
	return &GuestAuthenticator{
		theme:         theme,
		startLocation: startLocation,
		active:        make(map[string]struct{}),
	}
}

// GenerateName generates a unique themed guest name and reserves it
// in the active set. The caller is responsible for calling ReleaseGuest
// when the name is no longer in use.
func (a *GuestAuthenticator) GenerateName() (string, error) {
	return a.generateUniqueName()
}

// StartLocation returns the start location for guest characters.
func (a *GuestAuthenticator) StartLocation() ulid.ULID {
	return a.startLocation
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
