// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"strconv"

	"github.com/nats-io/nats.go"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
)

// HeaderAuditMetadata is the typed projection of a JetStream message's
// audit-related headers. Both the host audit projection (events_audit
// writer) and the per-plugin dispatcher use this parser; byte-equality
// of typed values across the two branches is structural (INV-P7-2).
//
// schema_ver is co-located here despite not being a crypto field —
// single source of truth for header → typed-value conversion prevents
// the host-branch and plugin-branch from drifting on parse rules
// (default value, error code, header name spelling).
type HeaderAuditMetadata struct {
	Codec      string
	SchemaVer  int32  // SMALLINT-bounded; out-of-range rejected
	DEKRef     *int64 // nil for codec=identity
	DEKVersion *int32 // nil for codec=identity
}

// ParseAuditHeaders extracts typed audit metadata from JetStream message
// headers. Returns typed errors with codes the projection and plugin
// dispatcher both surface verbatim:
//   - AUDIT_MISSING_HEADER (codec / schema_version)
//   - AUDIT_BAD_SCHEMA_VERSION
//   - AUDIT_DEK_REF_PARSE_FAILED
//   - AUDIT_DEK_VERSION_PARSE_FAILED
func ParseAuditHeaders(h nats.Header) (HeaderAuditMetadata, error) {
	var meta HeaderAuditMetadata

	codec := h.Get(headerCodec)
	if codec == "" {
		return meta, oops.Code("AUDIT_MISSING_HEADER").
			With("header", headerCodec).
			Errorf("missing header")
	}
	meta.Codec = codec

	schemaVerStr := h.Get(headerSchemaVersion)
	if schemaVerStr == "" {
		return meta, oops.Code("AUDIT_MISSING_HEADER").
			With("header", headerSchemaVersion).
			Errorf("missing header")
	}
	ver, err := strconv.ParseInt(schemaVerStr, 10, 32)
	if err != nil || ver < 0 || ver > 32767 {
		return meta, oops.Code("AUDIT_BAD_SCHEMA_VERSION").
			With("value", schemaVerStr).
			Errorf("schema version out of range or non-numeric")
	}
	meta.SchemaVer = int32(ver)

	if v := h.Get(eventbus.HeaderDekRef); v != "" {
		parsed, parseErr := strconv.ParseInt(v, 10, 64)
		if parseErr != nil {
			return meta, oops.Code("AUDIT_DEK_REF_PARSE_FAILED").
				With("header", eventbus.HeaderDekRef).
				With("value", v).
				Wrap(parseErr)
		}
		meta.DEKRef = &parsed
	}
	if v := h.Get(eventbus.HeaderDekVersion); v != "" {
		parsed, parseErr := strconv.ParseInt(v, 10, 32)
		if parseErr != nil {
			return meta, oops.Code("AUDIT_DEK_VERSION_PARSE_FAILED").
				With("header", eventbus.HeaderDekVersion).
				With("value", v).
				Wrap(parseErr)
		}
		v32 := int32(parsed)
		meta.DEKVersion = &v32
	}

	return meta, nil
}
