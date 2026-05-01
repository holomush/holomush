// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package sceneopseventsappendonly

import (
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() { register.Plugin("sceneopseventsappendonly", newPlugin) }

func newPlugin(_ any) (register.LinterPlugin, error) { return &linterPlugin{}, nil }

type linterPlugin struct{}

func (linterPlugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{Analyzer}, nil
}

func (linterPlugin) GetLoadMode() string { return register.LoadModeTypesInfo }
