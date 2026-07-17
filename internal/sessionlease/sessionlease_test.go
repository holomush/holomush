// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package sessionlease

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestDefaultRefreshIntervalIsFifteenSeconds pins the value: three packages
// (internal/session, internal/telnet, internal/web) now agree on one number.
func TestDefaultRefreshIntervalIsFifteenSeconds(t *testing.T) {
	assert.Equal(t, 15*time.Second, DefaultRefreshInterval)
}
