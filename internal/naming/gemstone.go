// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package naming


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
	return gemstones[cryptoIntN(len(gemstones))], elements[cryptoIntN(len(elements))]
}
