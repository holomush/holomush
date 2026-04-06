// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"log/slog"

	"github.com/samber/oops"
	"google.golang.org/grpc"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// channelPlugin implements Handler and CommandHandler for the binary plugin.
type channelPlugin struct {
	store *channelStore
}

// HandleEvent is a no-op — the channel plugin does not subscribe to events.
func (p *channelPlugin) HandleEvent(_ context.Context, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	return nil, nil
}

// HandleCommand routes to subcommand handlers.
func (p *channelPlugin) HandleCommand(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return handleCommand(ctx, p.store, req)
}

// RegisterServices is a no-op — the channel plugin does not expose extra gRPC services.
func (p *channelPlugin) RegisterServices(_ grpc.ServiceRegistrar) {}

// Init connects to the schema-isolated database, runs migrations, and seeds
// default channels.
func (p *channelPlugin) Init(ctx context.Context, config *pluginv1.ServiceConfig) error {
	connStr := config.GetConnectionString()
	if connStr == "" {
		return oops.Code("CHANNEL_INIT_FAILED").Errorf("connection_string is required")
	}

	store, err := newChannelStore(ctx, connStr)
	if err != nil {
		return oops.Code("CHANNEL_INIT_FAILED").Wrap(err)
	}
	p.store = store

	seedDefaultChannels(ctx, store)

	slog.Info("core-channels plugin initialised", "storage", "postgres")
	return nil
}

// seedDefaultChannels creates the "Public" channel if it doesn't exist.
func seedDefaultChannels(ctx context.Context, store *channelStore) {
	seeds := []struct {
		name        string
		chanType    channelType
		description string
	}{
		{"Public", channelTypePublic, "General public discussion"},
	}

	for _, seed := range seeds {
		if _, err := store.getChannelByName(ctx, seed.name); err == nil {
			continue
		}
		ch, err := newChannel(seed.name, seed.chanType, seed.description, "system")
		if err != nil {
			slog.Warn("failed to create seeded channel", "name", seed.name, "error", err)
			continue
		}
		if err := store.createChannel(ctx, ch); err != nil {
			slog.Warn("failed to seed channel", "name", seed.name, "error", err)
			continue
		}
		slog.Info("seeded channel", "name", ch.Name, "type", ch.Type)
	}
}

func main() {
	plugin := &channelPlugin{}
	pluginsdk.ServeWithServices(&pluginsdk.ServeConfig{Handler: plugin}, plugin)
}
