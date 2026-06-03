// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"log/slog"

	lua "github.com/yuin/gopher-lua"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// AuditDecryptor is the narrow interface the decrypt_own_audit_rows Lua
// hostfunc calls to reach the host-side read-back decrypt primitive.
// Satisfied by *history.ReadbackDecryptor in production and by test
// doubles in unit tests.
//
// The plugin name is passed so the host can enforce the OwnerMap g1 gate
// (rows belonging to a different plugin are refused with
// no_plaintext_reason="not_owner").
type AuditDecryptor interface {
	DecryptOwnAuditRows(ctx context.Context, pluginName string, rows []*pluginv1.AuditRow) (*pluginv1.DecryptOwnAuditRowsResponse, error)
}

// RegisterAuditFuncs adds holomush.decrypt_own_audit_rows to an existing
// holomush module table. dec may be nil; calls will be no-ops in that case.
// pluginName and dec are captured in the registered function's Go closure —
// never stored in mutable Lua state — so plugin code cannot overwrite the
// identity the host uses for the OwnerMap g1 gate. This matches the
// host-stamped closure pattern used by the other hostfunc capabilities
// (see cap_audit.go / cap_alias.go).
func RegisterAuditFuncs(ls *lua.LState, mod *lua.LTable, pluginName string, dec AuditDecryptor) {
	ls.SetField(mod, "decrypt_own_audit_rows", ls.NewFunction(func(l *lua.LState) int {
		return decryptOwnAuditRowsImpl(l, pluginName, dec)
	}))
}

// decryptOwnAuditRowsImpl implements holomush.decrypt_own_audit_rows(rows).
// pluginName and dec are captured by RegisterAuditFuncs's closure (host-
// authoritative identity; never read from mutable Lua state).
//
// rows is a Lua array of tables, each with fields:
//
//	id        string  (required) — row identifier (opaque bytes, passed as string)
//	subject   string  (required) — event subject
//	type      string  (optional) — event type
//	payload   string  (required) — ciphertext bytes (opaque)
//	codec     string  (required) — e.g. "xchacha20poly1305-v1"
//	dek_ref   number  (optional) — DEK reference; absent for identity codec
//	dek_version number (optional)
//
// On success returns an array of result tables:
//
//	{ plaintext = string|nil, no_plaintext_reason = string|nil }
//
// On error returns (nil, error_string). If the decryptor is not configured,
// returns no values (no-op, same as other unconfigured hostfuncs).
func decryptOwnAuditRowsImpl(ls *lua.LState, pluginName string, dec AuditDecryptor) int {
	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if dec == nil {
		slog.WarnContext(ctx, "holomush.decrypt_own_audit_rows: audit decryptor not initialized")
		return 0
	}

	rowsTbl := ls.CheckTable(1)

	// A malformed row entry (anything that is not a table) is rejected — the
	// whole call fails rather than silently skipping the entry. Silent skips
	// would misalign results with input indices and break INV-CRYPTO-37 positional
	// correlation; a malformed batch is a plugin bug, so failing closed is the
	// safe behavior.
	rows := make([]*pluginv1.AuditRow, 0, rowsTbl.Len())
	var malformedKey lua.LValue
	rowsTbl.ForEach(func(k, v lua.LValue) {
		if malformedKey != nil {
			return
		}
		tbl, ok := v.(*lua.LTable)
		if !ok {
			malformedKey = k
			return
		}
		row := &pluginv1.AuditRow{
			Id:      []byte(lua.LVAsString(tbl.RawGetString("id"))),
			Subject: lua.LVAsString(tbl.RawGetString("subject")),
			Type:    lua.LVAsString(tbl.RawGetString("type")),
			Payload: []byte(lua.LVAsString(tbl.RawGetString("payload"))),
			Codec:   lua.LVAsString(tbl.RawGetString("codec")),
		}
		if dekRef := tbl.RawGetString("dek_ref"); dekRef != lua.LNil {
			v := uint64(lua.LVAsNumber(dekRef))
			row.DekRef = &v
		}
		if dekVer := tbl.RawGetString("dek_version"); dekVer != lua.LNil {
			v := uint32(lua.LVAsNumber(dekVer))
			row.DekVersion = &v
		}
		rows = append(rows, row)
	})
	if malformedKey != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString("decrypt_own_audit_rows: rows entry " + malformedKey.String() + " is not a table"))
		return 2
	}

	ctx, cancel := context.WithTimeout(ctx, defaultPluginQueryTimeout)
	defer cancel()

	resp, err := dec.DecryptOwnAuditRows(ctx, pluginName, rows)
	if err != nil {
		slog.WarnContext(ctx, "holomush.decrypt_own_audit_rows failed",
			"plugin", pluginName, "row_count", len(rows), "error", err)
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}

	resultsTbl := ls.NewTable()
	for i, res := range resp.GetResults() {
		entry := ls.NewTable()
		if len(res.GetPlaintext()) > 0 {
			ls.SetField(entry, "plaintext", lua.LString(string(res.GetPlaintext())))
		} else {
			ls.SetField(entry, "plaintext", lua.LNil)
		}
		if res.GetNoPlaintextReason() != "" {
			ls.SetField(entry, "no_plaintext_reason", lua.LString(res.GetNoPlaintextReason()))
		} else {
			ls.SetField(entry, "no_plaintext_reason", lua.LNil)
		}
		resultsTbl.RawSetInt(i+1, entry)
	}
	ls.Push(resultsTbl)
	return 1
}
