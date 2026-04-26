// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	plugins "github.com/holomush/holomush/internal/plugin"
)

type pluginEvent struct {
	Plugin      string
	EventType   string
	Sensitivity plugins.Sensitivity
	Description string
}

// NewPluginEventsCmd is `holomush plugin events`. Parent for list/show.
func NewPluginEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Inspect plugin event-type declarations",
	}
	cmd.AddCommand(newPluginEventsListCmd())
	cmd.AddCommand(newPluginEventsShowCmd())
	return cmd
}

func newPluginEventsListCmd() *cobra.Command {
	var pluginDir string
	var filterPlugin string
	var filterSensitivities []string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all event types declared by plugins under --plugin-dir",
		RunE: func(cmd *cobra.Command, _ []string) error {
			events, err := scanPluginEvents(pluginDir)
			if err != nil {
				return err
			}
			rows := filterEvents(events, filterPlugin, filterSensitivities)
			return printEventsTable(cmd.OutOrStdout(), rows)
		},
	}
	cmd.Flags().StringVar(&pluginDir, "plugin-dir", "plugins", "directory containing plugin subdirectories")
	cmd.Flags().StringVar(&filterPlugin, "plugin", "", "filter to a single plugin")
	cmd.Flags().StringSliceVar(&filterSensitivities, "sensitivity", nil, "filter to specific sensitivities (always, may, never)")
	return cmd
}

func newPluginEventsShowCmd() *cobra.Command {
	var pluginDir string
	cmd := &cobra.Command{
		Use:   "show <plugin>:<event_type>",
		Short: "Show full declaration for one event type",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			events, err := scanPluginEvents(pluginDir)
			if err != nil {
				return err
			}
			ref := args[0]
			for _, e := range events {
				qualified := e.Plugin + ":" + e.EventType
				if qualified == ref {
					_, err = fmt.Fprintf(cmd.OutOrStdout(),
						"Owned by: %s\nEvent: %s\nSensitivity: %s\nDescription: %s\n",
						e.Plugin, e.EventType, e.Sensitivity, e.Description)
					return err
				}
			}
			return fmt.Errorf("event type %q not found", ref)
		},
	}
	cmd.Flags().StringVar(&pluginDir, "plugin-dir", "plugins", "directory containing plugin subdirectories")
	return cmd
}

func scanPluginEvents(rootDir string) ([]pluginEvent, error) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, err
	}
	var out []pluginEvent
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		manifestPath := filepath.Join(rootDir, ent.Name(), "plugin.yaml")
		raw, err := os.ReadFile(manifestPath)
		if err != nil {
			continue // not a plugin directory
		}
		m, err := plugins.ParseManifest(raw)
		if err != nil {
			continue
		}
		if m.Crypto == nil {
			continue
		}
		for _, e := range m.Crypto.Emits {
			out = append(out, pluginEvent{
				Plugin:      m.Name,
				EventType:   e.EventType,
				Sensitivity: e.Sensitivity,
				Description: e.Description,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Plugin != out[j].Plugin {
			return out[i].Plugin < out[j].Plugin
		}
		return out[i].EventType < out[j].EventType
	})
	return out, nil
}

func filterEvents(in []pluginEvent, plugin string, sensitivities []string) []pluginEvent {
	if plugin == "" && len(sensitivities) == 0 {
		return in
	}
	allowed := map[string]bool{}
	for _, s := range sensitivities {
		allowed[s] = true
	}
	var out []pluginEvent
	for _, e := range in {
		if plugin != "" && e.Plugin != plugin {
			continue
		}
		if len(allowed) > 0 && !allowed[string(e.Sensitivity)] {
			continue
		}
		out = append(out, e)
	}
	return out
}

func printEventsTable(w io.Writer, events []pluginEvent) error {
	for _, e := range events {
		_, err := fmt.Fprintf(w, "%-32s %-8s %s\n",
			e.Plugin+":"+e.EventType,
			string(e.Sensitivity),
			e.Description)
		if err != nil {
			return err
		}
	}
	return nil
}
