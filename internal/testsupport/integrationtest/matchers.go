// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

import (
	"fmt"

	"github.com/onsi/gomega/types"
	"github.com/samber/oops"
)

// MatchOopsCode returns a Gomega matcher that succeeds when the actual error
// is an oops error whose Code() matches expected. Use:
//
//	Expect(err).To(integrationtest.MatchOopsCode("STREAM_ACCESS_DENIED"))
func MatchOopsCode(expected string) types.GomegaMatcher {
	return &oopsCodeMatcher{expected: expected}
}

type oopsCodeMatcher struct {
	expected string
	gotCode  string
}

func (m *oopsCodeMatcher) Match(actual interface{}) (bool, error) {
	err, ok := actual.(error)
	if !ok || err == nil {
		return false, fmt.Errorf("MatchOopsCode expects an error, got %T", actual)
	}
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return false, nil
	}
	code, _ := oopsErr.Code().(string)
	m.gotCode = code
	return m.gotCode == m.expected, nil
}

func (m *oopsCodeMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("expected oops code %q, got %q (full err: %v)", m.expected, m.gotCode, actual)
}

func (m *oopsCodeMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("expected oops code NOT to be %q, but it was", m.expected)
}
