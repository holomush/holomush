// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package auditheader holds the typed JetStream-header parser used by
// both the host audit projection (events_audit writer) and the per-
// plugin dispatcher (plugin_consumer.go) — and by the SDK Layer 2
// helper pkg/plugin/audit.go::StoreFromMessage.
//
// The package is structurally a leaf: it imports only `nats.Header` +
// `oops` so it is safely importable from anywhere in the module without
// risking the test-time cycle that would result from putting the
// parser inside `internal/eventbus/audit` (which transitively imports
// `internal/eventbus` → `internal/core`, while
// `internal/core/event_test.go` imports `pkg/plugin`).
//
// Header-name constants are defined locally — they are protocol wire
// constants (App-Codec, App-Schema-Version, App-Dek-Ref, App-Dek-Version)
// stable across the bus, the audit projection, and the SDK round-trip.
// `internal/eventbus.HeaderDekRef` / `HeaderDekVersion` continue to
// exist for publisher-side use; values match by string equality.
package auditheader

import (
	"strconv"

	"github.com/nats-io/nats.go"
	"github.com/samber/oops"
)

// Wire constants — protocol header names. Same string values as
// internal/eventbus.{HeaderDekRef,HeaderDekVersion} and
// internal/eventbus/audit.{headerCodec,headerSchemaVersion}.
const (
	HeaderCodec         = "App-Codec"
	HeaderSchemaVersion = "App-Schema-Version"
	HeaderDekRef        = "App-Dek-Ref"
	HeaderDekVersion    = "App-Dek-Version"
)

// Metadata is the typed projection of audit-related JetStream headers.
// Both the host audit projection and the per-plugin dispatcher use this
// parser; byte-equality of typed values across the two branches is
// structural (INV-CRYPTO-39).
//
// schema_ver is co-located here despite not being a crypto field —
// single source of truth for header → typed-value conversion prevents
// the host-branch and plugin-branch from drifting on parse rules
// (default value, error code, header name spelling).
type Metadata struct {
	Codec      string
	SchemaVer  int32  // SMALLINT-bounded; out-of-range rejected
	DEKRef     *int64 // nil for codec=identity
	DEKVersion *int32 // nil for codec=identity
}

// Parse extracts typed audit metadata from JetStream message headers.
// Returns typed errors with codes the projection and plugin dispatcher
// both surface verbatim:
//
//   - AUDIT_MISSING_HEADER (codec / schema_version)
//   - AUDIT_BAD_SCHEMA_VERSION
//   - AUDIT_DEK_REF_PARSE_FAILED
//   - AUDIT_DEK_VERSION_PARSE_FAILED
func Parse(h nats.Header) (Metadata, error) {
	var meta Metadata

	codec := h.Get(HeaderCodec)
	if codec == "" {
		return meta, oops.Code("AUDIT_MISSING_HEADER").
			With("header", HeaderCodec).
			Errorf("missing header")
	}
	meta.Codec = codec

	schemaVerStr := h.Get(HeaderSchemaVersion)
	if schemaVerStr == "" {
		return meta, oops.Code("AUDIT_MISSING_HEADER").
			With("header", HeaderSchemaVersion).
			Errorf("missing header")
	}
	ver, err := strconv.ParseInt(schemaVerStr, 10, 32)
	if err != nil || ver < 0 || ver > 32767 {
		return meta, oops.Code("AUDIT_BAD_SCHEMA_VERSION").
			With("value", schemaVerStr).
			Errorf("schema version out of range or non-numeric")
	}
	meta.SchemaVer = int32(ver)

	if v := h.Get(HeaderDekRef); v != "" {
		parsed, parseErr := strconv.ParseInt(v, 10, 64)
		if parseErr != nil || parsed < 0 {
			return meta, oops.Code("AUDIT_DEK_REF_PARSE_FAILED").
				With("header", HeaderDekRef).
				With("value", v).
				Errorf("dek_ref must be a non-negative integer (parse=%v)", parseErr)
		}
		meta.DEKRef = &parsed
	}
	if v := h.Get(HeaderDekVersion); v != "" {
		parsed, parseErr := strconv.ParseInt(v, 10, 32)
		if parseErr != nil || parsed < 0 {
			return meta, oops.Code("AUDIT_DEK_VERSION_PARSE_FAILED").
				With("header", HeaderDekVersion).
				With("value", v).
				Errorf("dek_version must be a non-negative integer (parse=%v)", parseErr)
		}
		v32 := int32(parsed)
		meta.DEKVersion = &v32
	}

	return meta, nil
}
