// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package naming


var stars = []string{
	"Sirius", "Vega", "Rigel", "Altair", "Deneb",
	"Polaris", "Antares", "Betelgeuse", "Capella", "Arcturus",
	"Spica", "Regulus", "Procyon", "Aldebaran", "Fomalhaut",
	"Canopus", "Achernar", "Bellatrix", "Castor", "Pollux",
}

// StarTheme generates single star names for admin/staff characters.
type StarTheme struct{}

// NewStarTheme creates a new StarTheme.
func NewStarTheme() *StarTheme {
	return &StarTheme{}
}

// Name returns the theme identifier.
func (t *StarTheme) Name() string {
	return "star"
}

// Generate returns a random star name as firstName, with empty secondName.
func (t *StarTheme) Generate() (firstName, secondName string) {
	return stars[cryptoIntN(len(stars))], ""
}
