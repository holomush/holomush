// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"github.com/nats-io/nats.go"

	"github.com/holomush/holomush/internal/eventbus/audit/auditheader"
)

// HeaderAuditMetadata is the typed projection of a JetStream message's
// audit-related headers. Both the host audit projection (events_audit
// writer) and the per-plugin dispatcher use this parser; byte-equality
// of typed values across the two branches is structural (INV-CRYPTO-39).
//
// Aliased from internal/eventbus/audit/auditheader.Metadata. The leaf
// package exists so pkg/plugin/audit.go::StoreFromMessage can use the
// same parser without importing internal/eventbus/audit (which would
// create a test-time cycle: internal/core (event_test.go) → pkg/plugin
// → internal/eventbus/audit → internal/eventbus → internal/core).
type HeaderAuditMetadata = auditheader.Metadata

// ParseAuditHeaders extracts typed audit metadata from JetStream message
// headers. Delegates to the leaf parser package; keeps the existing
// audit-package API stable (host projection callers don't change).
func ParseAuditHeaders(h nats.Header) (HeaderAuditMetadata, error) {
	return auditheader.Parse(h) //nolint:wrapcheck // thin re-export; oops codes already attached by the leaf parser
}
