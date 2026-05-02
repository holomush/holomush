// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

func TestEventFrameCarriesMetadataOnlyFlag(t *testing.T) {
	frame := &corev1.EventFrame{
		Id:           "01HXX",
		Stream:       "events.scene.01ABC.ic",
		Type:         "core-comm:whisper",
		MetadataOnly: true,
	}
	assert.True(t, frame.GetMetadataOnly())
}
