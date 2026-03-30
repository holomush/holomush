// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package coreobjects

import (
	"context"
	"fmt"
	"sort"
	"strings"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// handleExamine inspects objects, locations, and characters.
//
// Syntax:
//   - examine        -- examine current location
//   - examine here   -- examine current location
//   - examine <name> -- examine named target
func handleExamine(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	args := strings.TrimSpace(cmd.Args)

	if args == "" || strings.EqualFold(args, "here") {
		return examineLocation(ctx, cmd, proxy)
	}
	return examineTarget(ctx, cmd, proxy, args)
}

// examineLocation examines the current location.
func examineLocation(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	loc, err := proxy.QueryLocation(ctx, cmd.CharacterID, cmd.LocationID)
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("examine: failed to query location %s: %v", cmd.LocationID, err))
		return pluginsdk.Failuref("Unable to examine this location right now. Please try again."), nil
	}

	props, propsErr := proxy.ListPropertiesByParent(ctx, cmd.CharacterID, "location", cmd.LocationID)
	if propsErr != nil {
		proxy.Log(ctx, "warn", fmt.Sprintf("examine: failed to list properties for location %s: %v", cmd.LocationID, propsErr))
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== %s ===\n", loc.Name))
	b.WriteString(fmt.Sprintf("Name: %s\n", loc.Name))
	if loc.Description != "" {
		b.WriteString(fmt.Sprintf("Description:\n  %s\n", loc.Description))
	}
	writeProperties(&b, props)

	return pluginsdk.OK(b.String()), nil
}

// examineCandidate represents a potential match during target resolution.
type examineCandidate struct {
	name string
	kind string // "character" or "object"
	idx  int
}

// disambiguate returns a user-facing error listing ambiguous matches.
func disambiguate(matches []examineCandidate) *pluginsdk.CommandResponse {
	var b strings.Builder
	b.WriteString("Which one? I see multiple matches:\n")
	for _, m := range matches {
		fmt.Fprintf(&b, "  %s (%s)\n", m.name, m.kind)
	}
	return pluginsdk.Errorf("%s", b.String())
}

// examineTarget resolves and examines a named target.
func examineTarget(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy, name string) (*pluginsdk.CommandResponse, error) {
	chars, err := proxy.GetCharactersByLocation(ctx, cmd.CharacterID, cmd.LocationID)
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("examine: failed to query characters at %s: %v", cmd.LocationID, err))
		return pluginsdk.Failuref("Unable to look around right now. Please try again."), nil
	}

	objs, err := proxy.GetObjectsByLocation(ctx, cmd.CharacterID, cmd.LocationID)
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("examine: failed to query objects at %s: %v", cmd.LocationID, err))
		return pluginsdk.Failuref("Unable to look around right now. Please try again."), nil
	}

	// Collect exact matches across both characters and objects.
	var exact []examineCandidate
	for i, c := range chars {
		if strings.EqualFold(c.Name, name) {
			exact = append(exact, examineCandidate{c.Name, "character", i})
		}
	}
	for i, o := range objs {
		if strings.EqualFold(o.Name, name) {
			exact = append(exact, examineCandidate{o.Name, "object", i})
		}
	}

	if len(exact) == 1 {
		if exact[0].kind == "character" {
			return examineCharacter(ctx, cmd, proxy, &chars[exact[0].idx])
		}
		return examineObject(ctx, cmd, proxy, &objs[exact[0].idx])
	}
	if len(exact) > 1 {
		return disambiguate(exact), nil
	}

	// No exact matches — try prefix matching.
	lower := strings.ToLower(name)
	var prefix []examineCandidate
	for i, c := range chars {
		if strings.HasPrefix(strings.ToLower(c.Name), lower) {
			prefix = append(prefix, examineCandidate{c.Name, "character", i})
		}
	}
	for i, o := range objs {
		if strings.HasPrefix(strings.ToLower(o.Name), lower) {
			prefix = append(prefix, examineCandidate{o.Name, "object", i})
		}
	}

	if len(prefix) == 1 {
		if prefix[0].kind == "character" {
			return examineCharacter(ctx, cmd, proxy, &chars[prefix[0].idx])
		}
		return examineObject(ctx, cmd, proxy, &objs[prefix[0].idx])
	}
	if len(prefix) > 1 {
		return disambiguate(prefix), nil
	}

	return pluginsdk.Errorf("I don't see %q here.", name), nil
}

func examineCharacter(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy, c *plugins.CharacterResult) (*pluginsdk.CommandResponse, error) {
	props, propsErr := proxy.ListPropertiesByParent(ctx, cmd.CharacterID, "character", c.ID)
	if propsErr != nil {
		proxy.Log(ctx, "warn", fmt.Sprintf("examine: failed to list properties for character %s: %v", c.ID, propsErr))
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== %s ===\n", c.Name))
	b.WriteString(fmt.Sprintf("Name: %s\n", c.Name))
	if c.Description != "" {
		b.WriteString(fmt.Sprintf("Description:\n  %s\n", c.Description))
	}
	writeProperties(&b, props)

	return pluginsdk.OK(b.String()), nil
}

func examineObject(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy, o *plugins.ObjectResult) (*pluginsdk.CommandResponse, error) {
	props, propsErr := proxy.ListPropertiesByParent(ctx, cmd.CharacterID, "object", o.ID)
	if propsErr != nil {
		proxy.Log(ctx, "warn", fmt.Sprintf("examine: failed to list properties for object %s: %v", o.ID, propsErr))
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== %s ===\n", o.Name))
	b.WriteString(fmt.Sprintf("Name: %s\n", o.Name))
	if o.Description != "" {
		b.WriteString(fmt.Sprintf("Description:\n  %s\n", o.Description))
	}
	writeProperties(&b, props)

	return pluginsdk.OK(b.String()), nil
}

// writeProperties appends formatted property lines to a builder.
func writeProperties(b *strings.Builder, props []plugins.PropertyInfo) {
	visible := filterVisibleProperties(props)
	if len(visible) == 0 {
		return
	}

	sort.Slice(visible, func(i, j int) bool {
		return visible[i].Name < visible[j].Name
	})

	b.WriteString("\nProperties:\n")
	for _, p := range visible {
		b.WriteString(fmt.Sprintf("  %s: %s\n", p.Name, p.Value))
	}
}

// filterVisibleProperties returns properties visible at player tier (public).
func filterVisibleProperties(props []plugins.PropertyInfo) []plugins.PropertyInfo {
	var result []plugins.PropertyInfo
	for _, p := range props {
		if p.Visibility == "public" {
			result = append(result, p)
		}
	}
	return result
}
