// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"

	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/content"
	contentv1 "github.com/holomush/holomush/pkg/proto/holomush/content/v1"
)

// ContentServiceServer implements the gRPC ContentService, providing read
// access to the content store.
type ContentServiceServer struct {
	contentv1.UnimplementedContentServiceServer
	store content.Store
}

// NewContentServiceServer creates a new ContentServiceServer with the given
// content store.
func NewContentServiceServer(store content.Store) *ContentServiceServer {
	return &ContentServiceServer{store: store}
}

// GetContent retrieves a single content item by key. Returns a gRPC NotFound
// error if the key does not exist.
func (s *ContentServiceServer) GetContent(ctx context.Context, req *contentv1.GetContentRequest) (*contentv1.GetContentResponse, error) {
	item, err := s.store.Get(ctx, req.GetKey())
	if err != nil {
		return nil, oops.Code("CONTENT_GET_FAILED").With("key", req.GetKey()).Wrap(err)
	}
	if item == nil {
		return nil, status.Errorf(codes.NotFound, "content item not found: %s", req.GetKey())
	}
	return &contentv1.GetContentResponse{
		Item: itemToProto(item),
	}, nil
}

// ListContent returns all content items matching a key prefix with optional
// pagination via limit and cursor.
func (s *ContentServiceServer) ListContent(ctx context.Context, req *contentv1.ListContentRequest) (*contentv1.ListContentResponse, error) {
	opts := content.ListOptions{
		Limit:  int(req.GetLimit()),
		Cursor: req.GetCursor(),
	}
	result, err := s.store.List(ctx, req.GetPrefix(), opts)
	if err != nil {
		return nil, oops.Code("CONTENT_LIST_FAILED").With("prefix", req.GetPrefix()).Wrap(err)
	}
	items := make([]*contentv1.ContentItem, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, itemToProto(item))
	}
	return &contentv1.ListContentResponse{
		Items:      items,
		NextCursor: result.NextCursor,
	}, nil
}

// itemToProto converts a content.Item to the protobuf ContentItem type.
func itemToProto(item *content.Item) *contentv1.ContentItem {
	return &contentv1.ContentItem{
		Key:         item.Key,
		ContentType: item.ContentType,
		Body:        item.Body,
		Metadata:    item.Metadata,
	}
}
