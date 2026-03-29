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
		return &pluginsdk.CommandResponse{
			Output: "You can't examine anything here.\n",
		}, nil
	}

	props, _ := proxy.ListPropertiesByParent(ctx, cmd.CharacterID, "location", cmd.LocationID)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== %s ===\n", loc.Name))
	b.WriteString(fmt.Sprintf("Name: %s\n", loc.Name))
	if loc.Description != "" {
		b.WriteString(fmt.Sprintf("Description:\n  %s\n", loc.Description))
	}
	writeProperties(&b, props)

	return &pluginsdk.CommandResponse{
		Output: b.String(),
	}, nil
}

// examineTarget resolves and examines a named target.
func examineTarget(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy, name string) (*pluginsdk.CommandResponse, error) {
	// Try character match first
	chars, _ := proxy.GetCharactersByLocation(ctx, cmd.CharacterID, cmd.LocationID)
	for _, c := range chars {
		if strings.EqualFold(c.Name, name) {
			return examineCharacter(ctx, cmd, proxy, &c)
		}
	}

	// Try object match
	objs, _ := proxy.GetObjectsByLocation(ctx, cmd.CharacterID, cmd.LocationID)
	for _, o := range objs {
		if strings.EqualFold(o.Name, name) {
			return examineObject(ctx, cmd, proxy, &o)
		}
	}

	// Try prefix match on characters
	lower := strings.ToLower(name)
	for _, c := range chars {
		if strings.HasPrefix(strings.ToLower(c.Name), lower) {
			return examineCharacter(ctx, cmd, proxy, &c)
		}
	}

	// Try prefix match on objects
	for _, o := range objs {
		if strings.HasPrefix(strings.ToLower(o.Name), lower) {
			return examineObject(ctx, cmd, proxy, &o)
		}
	}

	return &pluginsdk.CommandResponse{
		Output: fmt.Sprintf("I don't see %q here.\n", name),
	}, nil
}

func examineCharacter(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy, c *plugins.CharacterResult) (*pluginsdk.CommandResponse, error) {
	props, _ := proxy.ListPropertiesByParent(ctx, cmd.CharacterID, "character", c.ID)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== %s ===\n", c.Name))
	b.WriteString(fmt.Sprintf("Name: %s\n", c.Name))
	if c.Description != "" {
		b.WriteString(fmt.Sprintf("Description:\n  %s\n", c.Description))
	}
	writeProperties(&b, props)

	return &pluginsdk.CommandResponse{
		Output: b.String(),
	}, nil
}

func examineObject(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy, o *plugins.ObjectResult) (*pluginsdk.CommandResponse, error) {
	props, _ := proxy.ListPropertiesByParent(ctx, cmd.CharacterID, "object", o.ID)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== %s ===\n", o.Name))
	b.WriteString(fmt.Sprintf("Name: %s\n", o.Name))
	if o.Description != "" {
		b.WriteString(fmt.Sprintf("Description:\n  %s\n", o.Description))
	}
	writeProperties(&b, props)

	return &pluginsdk.CommandResponse{
		Output: b.String(),
	}, nil
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
