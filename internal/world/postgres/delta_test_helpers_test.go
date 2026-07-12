// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import "github.com/holomush/holomush/internal/world/wmodel"

// delErr discards the *wmodel.MutationDelta a world repository write now returns,
// yielding just the error — a mechanical 05-14 test bridge (behavior-preserving).
func delErr(_ *wmodel.MutationDelta, err error) error { return err }
