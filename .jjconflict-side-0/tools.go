// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build tools
// +build tools

// Package main pins tool and test dependencies to go.mod.
// See https://go.dev/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module
package main

import (
	// Testing frameworks
	_ "github.com/onsi/ginkgo/v2"
	_ "github.com/onsi/gomega"
	_ "github.com/stretchr/testify/assert"
	_ "github.com/stretchr/testify/mock"
	_ "github.com/stretchr/testify/require"
)
