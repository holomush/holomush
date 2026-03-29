// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/world"
)

// accessTier represents the viewer's access level for examine output filtering.
type accessTier int

const (
	tierPlayer  accessTier = iota // name, description, public properties
	tierOwner                     // + private properties, exit destinations
	tierBuilder                   // + owner, type, created, restricted properties
	tierAdmin                     // + ULID, system/admin properties
)

// examineInfo holds a resolved target entity for examine output.
type examineInfo struct {
	kind       string // "location", "character", "exit", "object"
	id         ulid.ULID
	name       string
	desc       string
	ownerID    *ulid.ULID
	playerID   *ulid.ULID // character only
	locType    world.LocationType
	createdAt  string
	exits      []*world.Exit // location only
	properties []*world.EntityProperty
}

// ExamineHandler inspects objects, locations, and characters.
//
// Syntax:
//   - examine → examine current location
//   - examine here → examine current location
//   - examine <target> → examine named target
func ExamineHandler(ctx context.Context, exec *command.CommandExecution) error {
	args := strings.TrimSpace(exec.Args)
	subjectID := access.CharacterSubject(exec.CharacterID().String())

	if args == "" || strings.EqualFold(args, "here") {
		return examineCurrentLocation(ctx, exec, subjectID)
	}
	return examineNamedTarget(ctx, exec, subjectID, args)
}

// examineCurrentLocation examines the current location.
func examineCurrentLocation(ctx context.Context, exec *command.CommandExecution, subjectID string) error {
	loc, err := exec.Services().World().GetLocation(ctx, subjectID, exec.LocationID())
	if err != nil {
		writeOutput(ctx, exec, "examine", "You can't examine anything here.")
		return nil //nolint:nilerr // user-facing message shown; error is non-actionable
	}

	// Gather location contents for display. Errors are non-fatal — show what we can.
	exits, _ := exec.Services().World().GetExitsByLocation(ctx, subjectID, exec.LocationID()) //nolint:errcheck // non-fatal: exits section omitted on error
	props, _ := exec.Services().World().ListPropertiesByParent(ctx, subjectID, "location", loc.ID) //nolint:errcheck // non-fatal: properties section omitted on error

	tier := determineTier(ctx, exec, subjectID)
	info := examineInfo{
		kind:       "location",
		id:         loc.ID,
		name:       loc.Name,
		desc:       loc.Description,
		ownerID:    loc.OwnerID,
		locType:    loc.Type,
		createdAt:  loc.CreatedAt.Format("2006-01-02"),
		exits:      exits,
		properties: props,
	}

	writeExamineOutput(ctx, exec, info, tier)
	return nil
}

// examineNamedTarget resolves and examines a named target in the current location.
func examineNamedTarget(ctx context.Context, exec *command.CommandExecution, subjectID, name string) error {
	locID := exec.LocationID()

	// Gather all entities at the location. Errors are non-fatal.
	chars, _ := exec.Services().World().GetCharactersByLocation(ctx, subjectID, locID, world.ListOptions{}) //nolint:errcheck // non-fatal: characters omitted from matching on error
	exits, _ := exec.Services().World().GetExitsByLocation(ctx, subjectID, locID) //nolint:errcheck // non-fatal: exits omitted from matching on error
	objs, _ := exec.Services().World().GetObjectsByLocation(ctx, subjectID, locID) //nolint:errcheck // non-fatal: objects omitted from matching on error

	matches := resolveMatches(name, chars, exits, objs)

	if len(matches) == 0 {
		writeOutputf(ctx, exec, "examine", "I don't see %q here.\n", name)
		return nil
	}

	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.name + " (" + m.kind + ")"
		}
		writeOutputf(ctx, exec, "examine", "Multiple matches for %q: %s\n", name, strings.Join(names, ", "))
		return nil
	}

	m := matches[0]
	tier := determineTier(ctx, exec, subjectID)

	switch m.kind {
	case "character":
		return examineCharacterByID(ctx, exec, subjectID, chars, m.id, tier)
	case "exit":
		return examineExitByID(ctx, exec, subjectID, exits, m.id, tier)
	case "object":
		return examineObjectByID(ctx, exec, subjectID, objs, m.id, tier)
	}
	return nil
}

type entityMatch struct {
	kind string
	name string
	id   ulid.ULID
}

// resolveMatches finds entities matching the given name via exact then prefix matching.
func resolveMatches(name string, chars []*world.Character, exits []*world.Exit, objs []*world.Object) []entityMatch {
	var matches []entityMatch

	// First pass: exact case-insensitive name match.
	for _, c := range chars {
		if strings.EqualFold(c.Name, name) {
			matches = append(matches, entityMatch{kind: "character", name: c.Name, id: c.ID})
		}
	}
	for _, e := range exits {
		if e.MatchesName(name) {
			matches = append(matches, entityMatch{kind: "exit", name: e.Name, id: e.ID})
		}
	}
	for _, o := range objs {
		if strings.EqualFold(o.Name, name) {
			matches = append(matches, entityMatch{kind: "object", name: o.Name, id: o.ID})
		}
	}

	if len(matches) > 0 {
		return matches
	}

	// Second pass: prefix match if no exact matches found.
	lower := strings.ToLower(name)
	for _, c := range chars {
		if strings.HasPrefix(strings.ToLower(c.Name), lower) {
			matches = append(matches, entityMatch{kind: "character", name: c.Name, id: c.ID})
		}
	}
	for _, e := range exits {
		if strings.HasPrefix(strings.ToLower(e.Name), lower) {
			matches = append(matches, entityMatch{kind: "exit", name: e.Name, id: e.ID})
		}
	}
	for _, o := range objs {
		if strings.HasPrefix(strings.ToLower(o.Name), lower) {
			matches = append(matches, entityMatch{kind: "object", name: o.Name, id: o.ID})
		}
	}

	return matches
}

func examineCharacterByID(ctx context.Context, exec *command.CommandExecution, subjectID string, chars []*world.Character, id ulid.ULID, tier accessTier) error {
	var char *world.Character
	for _, c := range chars {
		if c.ID == id {
			char = c
			break
		}
	}
	if char == nil {
		return nil
	}

	// Check ownership: if viewer's playerID matches character's playerID → owner tier.
	if tier < tierOwner && char.PlayerID == exec.PlayerID() {
		tier = tierOwner
	}

	props, _ := exec.Services().World().ListPropertiesByParent(ctx, subjectID, "character", char.ID) //nolint:errcheck // non-fatal: properties omitted on error

	info := examineInfo{
		kind:       "character",
		id:         char.ID,
		name:       char.Name,
		desc:       char.Description,
		playerID:   &char.PlayerID,
		createdAt:  char.CreatedAt.Format("2006-01-02"),
		properties: props,
	}

	writeExamineOutput(ctx, exec, info, tier)
	return nil
}

func examineExitByID(ctx context.Context, exec *command.CommandExecution, subjectID string, exits []*world.Exit, id ulid.ULID, tier accessTier) error {
	var exit *world.Exit
	for _, e := range exits {
		if e.ID == id {
			exit = e
			break
		}
	}
	if exit == nil {
		return nil
	}

	props, _ := exec.Services().World().ListPropertiesByParent(ctx, subjectID, "exit", exit.ID) //nolint:errcheck // non-fatal: properties omitted on error

	info := examineInfo{
		kind:       "exit",
		id:         exit.ID,
		name:       exit.Name,
		createdAt:  exit.CreatedAt.Format("2006-01-02"),
		exits:      []*world.Exit{exit},
		properties: props,
	}

	writeExamineOutput(ctx, exec, info, tier)
	return nil
}

func examineObjectByID(ctx context.Context, exec *command.CommandExecution, subjectID string, objs []*world.Object, id ulid.ULID, tier accessTier) error {
	var obj *world.Object
	for _, o := range objs {
		if o.ID == id {
			obj = o
			break
		}
	}
	if obj == nil {
		return nil
	}

	// Check ownership.
	if tier < tierOwner && obj.OwnerID != nil {
		charID := exec.CharacterID()
		if *obj.OwnerID == charID {
			tier = tierOwner
		}
	}

	props, _ := exec.Services().World().ListPropertiesByParent(ctx, subjectID, "object", obj.ID) //nolint:errcheck // non-fatal: properties omitted on error

	info := examineInfo{
		kind:       "object",
		id:         obj.ID,
		name:       obj.Name,
		desc:       obj.Description,
		ownerID:    obj.OwnerID,
		createdAt:  obj.CreatedAt.Format("2006-01-02"),
		properties: props,
	}

	writeExamineOutput(ctx, exec, info, tier)
	return nil
}

// determineTier checks ABAC capabilities to determine the viewer's access tier.
func determineTier(ctx context.Context, exec *command.CommandExecution, subjectID string) accessTier {
	engine := exec.Services().Engine()

	if command.CheckCapability(ctx, engine, subjectID, "admin.examine", "examine") == nil {
		return tierAdmin
	}
	if command.CheckCapability(ctx, engine, subjectID, "build.examine", "examine") == nil {
		return tierBuilder
	}
	return tierPlayer
}

// writeExamineOutput formats and writes examine output filtered by tier.
func writeExamineOutput(ctx context.Context, exec *command.CommandExecution, info examineInfo, tier accessTier) {
	w := exec.Output()
	cmd := "examine"
	charID := exec.CharacterID().String()

	write := func(format string, args ...any) {
		if n, err := fmt.Fprintf(w, format, args...); err != nil {
			logOutputError(ctx, cmd, charID, n, err)
		}
	}

	// Header
	write("=== %s ===\n", info.name)

	// Admin-only: ULID
	if tier >= tierAdmin {
		write("ULID: %s\n", info.id.String())
	}

	// Builder+: Owner
	if tier >= tierBuilder {
		if info.ownerID != nil {
			write("Owner: %s\n", info.ownerID.String())
		}
		if info.playerID != nil {
			write("Owner: %s\n", info.playerID.String())
		}
	}

	// Builder+: Type (location only)
	if tier >= tierBuilder && info.kind == "location" {
		write("Type: %s\n", info.locType.String())
	}

	// Builder+: Created
	if tier >= tierBuilder {
		write("Created: %s\n", info.createdAt)
	}

	// Always show: Name, Description
	write("Name: %s\n", info.name)
	if info.desc != "" {
		write("Description:\n  %s\n", info.desc)
	}

	// Exits section
	if len(info.exits) > 0 {
		write("\nExits:\n")
		for _, e := range info.exits {
			if tier >= tierOwner {
				write("  %s -> %s\n", e.Name, e.ToLocationID.String())
			} else {
				write("  %s\n", e.Name)
			}
		}
	}

	// Properties section
	filtered := filterProperties(info.properties, tier)
	if len(filtered) > 0 {
		write("\nProperties:\n")
		// Sort for stable output
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].Name < filtered[j].Name
		})
		for _, p := range filtered {
			val := ""
			if p.Value != nil {
				val = *p.Value
			}
			write("  %s: %s\n", p.Name, val)
		}
	}
}

// filterProperties returns properties visible at the given tier.
func filterProperties(props []*world.EntityProperty, tier accessTier) []*world.EntityProperty {
	if len(props) == 0 {
		return nil
	}

	var result []*world.EntityProperty
	for _, p := range props {
		if propertyVisible(p.Visibility, tier) {
			result = append(result, p)
		}
	}
	return result
}

// propertyVisible returns true if a property with the given visibility is visible at the tier.
func propertyVisible(visibility string, tier accessTier) bool {
	switch visibility {
	case "public":
		return tier >= tierPlayer
	case "private":
		return tier >= tierOwner
	case "restricted":
		return tier >= tierBuilder
	case "system", "admin":
		return tier >= tierAdmin
	default:
		return false
	}
}
