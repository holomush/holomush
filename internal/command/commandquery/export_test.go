// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package commandquery

// MaxEngineErrors re-exports the unexported circuit-breaker threshold so the
// external commandquery_test package can pin the trip behavior against the real
// constant instead of a hardcoded literal.
const MaxEngineErrors = maxEngineErrors
