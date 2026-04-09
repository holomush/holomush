// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"strconv"

	"google.golang.org/grpc/metadata"
)

const (
	actorKindHeader = "x-holomush-actor-kind"
	actorIDHeader   = "x-holomush-actor-id"
)

type actorMetadataContextKey struct{}

type actorMetadata struct {
	kind ActorKind
	id   string
}

// WithOutgoingActorMetadata attaches trusted host actor metadata to an
// outgoing gRPC context for plugin RPC calls.
func WithOutgoingActorMetadata(ctx context.Context, kind ActorKind, id string) context.Context {
	return metadata.AppendToOutgoingContext(
		ctx,
		actorKindHeader, strconv.Itoa(int(kind)),
		actorIDHeader, id,
	)
}

// ActorMetadataFromOutgoingContext returns trusted actor metadata carried on
// outgoing gRPC metadata.
func ActorMetadataFromOutgoingContext(ctx context.Context) (ActorKind, string, bool) {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return 0, "", false
	}
	return actorMetadataFromMetadata(md)
}

// ActorMetadataFromIncomingContext returns trusted actor metadata carried on
// incoming gRPC metadata.
func ActorMetadataFromIncomingContext(ctx context.Context) (ActorKind, string, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return 0, "", false
	}
	return actorMetadataFromMetadata(md)
}

func contextWithIncomingActorMetadata(ctx context.Context) context.Context {
	kind, id, ok := ActorMetadataFromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	return context.WithValue(ctx, actorMetadataContextKey{}, actorMetadata{
		kind: kind,
		id:   id,
	})
}

func actorMetadataFromContext(ctx context.Context) (ActorKind, string, bool) {
	if meta, ok := ctx.Value(actorMetadataContextKey{}).(actorMetadata); ok && meta.id != "" {
		return meta.kind, meta.id, true
	}
	if kind, id, ok := ActorMetadataFromIncomingContext(ctx); ok {
		return kind, id, true
	}
	if kind, id, ok := ActorMetadataFromOutgoingContext(ctx); ok {
		return kind, id, true
	}
	return 0, "", false
}

func actorMetadataFromMetadata(md metadata.MD) (ActorKind, string, bool) {
	kinds := md.Get(actorKindHeader)
	ids := md.Get(actorIDHeader)
	if len(kinds) == 0 || len(ids) == 0 || ids[0] == "" {
		return 0, "", false
	}
	kind, err := strconv.ParseUint(kinds[0], 10, 8)
	if err != nil {
		return 0, "", false
	}
	return ActorKind(kind), ids[0], true
}
