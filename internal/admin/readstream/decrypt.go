// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// decrypt.go implements per-row decryption for the AdminReadStream operator-read
// path (sub-epic F). The decrypt flow mirrors dispatcher.go::DispatchFor but
// omits AuthGuard, SessionAuditEmitter, and plugin-identity branching — F has
// no per-event auth and no per-decrypt audit.
//
// INV-CRYPTO-62: the 6-branch classifier matrix maps decrypt errors to
// NoPlaintextReason values. Row-level failures produce metadata-only frames;
// context cancellation/deadline is fatal and bails the stream.

package readstream

import (
	"context"
	"errors"

	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/internal/eventbus/history/source"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// DEKResolver resolves a DEK by (keyID, version) to raw key material.
// The production implementation is dek.Manager; tests supply a fake.
type DEKResolver interface {
	Resolve(ctx context.Context, keyID codec.KeyID, version uint32) (codec.Key, error)
}

// CodecResolver resolves a codec by name.
// The production implementation wraps codec.Resolve; tests supply a fake.
type CodecResolver interface {
	Resolve(name codec.Name) (codec.Codec, error)
}

// DecryptRow performs per-row decryption for the operator-read path.
//
// Return contract:
//   - Success:          plaintext non-nil, reason=UNSPECIFIED, fatal=false, err=nil
//   - Row-level error:  plaintext=nil, reason=classified, fatal=false, err non-nil
//   - Stream-fatal:     plaintext=nil, reason=UNSPECIFIED, fatal=true, err non-nil
//
// Identity-codec rows are returned with the Envelope.Payload bytes as plaintext
// (pass-through); the caller should check row.Codec before calling if it wants
// to skip the decrypt path entirely.
func DecryptRow(
	ctx context.Context,
	row ColdRow,
	dek DEKResolver,
	codecs CodecResolver,
) (plaintext []byte, reason eventbus.NoPlaintextReason, fatal bool, err error) {
	// Step 1: unmarshal the envelope proto to extract payload + AAD fields.
	var envProto eventbusv1.Event
	if unmarshalErr := proto.Unmarshal(row.Envelope, &envProto); unmarshalErr != nil {
		return nil, eventbus.NoPlaintextReasonInternal, false,
			oops.Code("ADMIN_READSTREAM_ENVELOPE_UNMARSHAL_FAILED").Wrap(unmarshalErr)
	}

	// Identity codec: pass payload through without DEK resolution.
	if row.Codec == codec.NameIdentity {
		return envProto.GetPayload(), eventbus.NoPlaintextReasonUnspecified, false, nil
	}

	// Step 2: resolve the DEK.
	key, dekErr := dek.Resolve(ctx, row.KeyID, row.KeyVersion)
	if dekErr != nil {
		r, f := classifyDecryptErr(dekErr)
		return nil, r, f, oops.Wrap(dekErr)
	}

	// Step 3: resolve the codec.
	c, codecErr := codecs.Resolve(row.Codec)
	if codecErr != nil {
		r, f := classifyDecryptErr(codecErr)
		return nil, r, f, oops.Wrap(codecErr)
	}

	// Step 4: build AAD — mirrors dispatcher.go decodeAuthorizeAndDispatch.
	// aad.Build takes (event, codecName string, dekRef uint64, dekVersion uint32).
	aadBytes, aadErr := aad.Build(&envProto, string(row.Codec), uint64(row.KeyID), row.KeyVersion)
	if aadErr != nil {
		r, f := classifyDecryptErr(aadErr)
		return nil, r, f, oops.Code("ADMIN_READSTREAM_AAD_BUILD_FAILED").Wrap(aadErr)
	}

	// Step 5: decrypt.
	plaintext, decErr := c.Decode(ctx, envProto.GetPayload(), key, aadBytes)
	if decErr != nil {
		r, f := classifyDecryptErr(decErr)
		return nil, r, f, oops.Code("ADMIN_READSTREAM_CODEC_DECODE_FAILED").Wrap(decErr)
	}

	// Step 6: success.
	return plaintext, eventbus.NoPlaintextReasonUnspecified, false, nil
}

// classifyDecryptErr maps an error to a NoPlaintextReason + fatal flag per
// INV-CRYPTO-62's 6-branch matrix.
//
// Fatal=true means the entire stream should bail (ctx canceled/deadline).
// Fatal=false means the row becomes a metadata-only frame and streaming continues.
func classifyDecryptErr(err error) (eventbus.NoPlaintextReason, bool) {
	if err == nil {
		return eventbus.NoPlaintextReasonUnspecified, false
	}
	// Branch 1: context cancellation / deadline — fatal, bail stream.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return eventbus.NoPlaintextReasonUnspecified, true
	}
	// Branch 2: hot+cold double miss sentinel from FallbackResolver.
	if errors.Is(err, source.ErrMetadataOnly) {
		return eventbus.NoPlaintextReasonStaleDEK, false
	}
	// Branch 3/4: typed DEK missing — stale DEK, row-level, continue stream.
	if isOopsCode(err, "DEK_NOT_FOUND") || isOopsCode(err, "DEK_DESTROYED") {
		return eventbus.NoPlaintextReasonStaleDEK, false
	}
	// Branch 5: DEK row columns malformed — dek_ref present but dek_version NULL
	// (INV-CRYPTO-25 violation) or zero KeyID. Classified as DEKBadColumns so operators
	// see a structured reason rather than a misleading STALE_DEK.
	if isOopsCode(err, "ADMIN_READSTREAM_COLD_DEK_VERSION_NULL") || isOopsCode(err, "ADMIN_READSTREAM_COLD_NO_DEK") {
		return eventbus.NoPlaintextReasonDEKBadColumns, false
	}
	// Branch 6: catch-all — AAD mismatch, codec failure, unmarshal error, etc.
	return eventbus.NoPlaintextReasonInternal, false
}

// isOopsCode reports whether err carries the given oops error code.
func isOopsCode(err error, code string) bool {
	var oe oops.OopsError
	if !errors.As(err, &oe) {
		return false
	}
	return oe.Code() == code
}
