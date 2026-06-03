// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package eventbus

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// internalFakePublisher is the in-package version of fakePublisher.
// Duplicated here because in-package tests cannot import the _test package.
type internalFakePublisher struct{}

func (internalFakePublisher) Publish(_ context.Context, _ Event) error { return nil }

// TestValidateRenderingRejectsSpeechWithoutLabel exercises INV-EVENTBUS-5.
func TestValidateRenderingRejectsSpeechWithoutLabel(t *testing.T) {
	rp := NewRenderingPublisher(internalFakePublisher{}, core.NewVerbRegistry())

	bad := &corev1.RenderingMetadata{
		Category:            "communication",
		Format:              "speech",
		Label:               "",
		DisplayTarget:       corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
		SourcePlugin:        "core-communication",
		SourcePluginVersion: "0.1.0",
	}

	err := rp.validateRendering(bad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "label", "validator error should mention the failing constraint")
}

// TestValidateRenderingRejectsUnspecifiedDisplayTarget verifies the enum
// not_in:[0] check that enforces INV-EVENTBUS-9.
func TestValidateRenderingRejectsUnspecifiedDisplayTarget(t *testing.T) {
	rp := NewRenderingPublisher(internalFakePublisher{}, core.NewVerbRegistry())

	bad := &corev1.RenderingMetadata{
		Category:            "communication",
		Format:              "narrative",
		Label:               "",
		DisplayTarget:       corev1.EventChannel_EVENT_CHANNEL_UNSPECIFIED,
		SourcePlugin:        "core-communication",
		SourcePluginVersion: "0.1.0",
	}

	err := rp.validateRendering(bad)
	require.Error(t, err)
}

// TestValidateRenderingAcceptsWellFormed sanity check.
func TestValidateRenderingAcceptsWellFormed(t *testing.T) {
	rp := NewRenderingPublisher(internalFakePublisher{}, core.NewVerbRegistry())

	good := &corev1.RenderingMetadata{
		Category:            "communication",
		Format:              "speech",
		Label:               "says",
		DisplayTarget:       corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
		SourcePlugin:        "core-communication",
		SourcePluginVersion: "0.1.0",
	}

	err := rp.validateRendering(good)
	require.NoError(t, err)
}
