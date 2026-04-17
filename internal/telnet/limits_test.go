// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultLimitsMatchSpec(t *testing.T) {
	assert.Equal(t, 5*time.Minute, DefaultLimits.IdleReadTimeout,
		"spec pins idle read timeout default to 5m")
	assert.Equal(t, 30*time.Second, DefaultLimits.WriteTimeout,
		"spec pins write timeout default to 30s")
	assert.Equal(t, 2*time.Minute, DefaultLimits.PreAuthTimeout,
		"spec pins pre-auth timeout default to 2m")
}
