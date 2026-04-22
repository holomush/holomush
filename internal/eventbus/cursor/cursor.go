// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package cursor owns the host-internal opaque cursor token used by
// QueryStreamHistory and Subscribe. Wire format is the proto-marshaled
// bytes of cursor.proto's Cursor message.
//
// See docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §4.5.
package cursor

import (
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	cursorv1 "github.com/holomush/holomush/internal/eventbus/cursor/cursorv1"
)

// CurrentVersion is the only cursor format version this build emits.
// Decoders accept this AND any earlier versions (none today).
const CurrentVersion uint32 = 1

// OwnerKind identifies who owns the subject a cursor names.
type OwnerKind uint8

const (
	// OwnerUnspecified is the zero value; callers should not produce it.
	OwnerUnspecified OwnerKind = 0
	// OwnerHost identifies the host event bus as the cursor owner.
	OwnerHost OwnerKind = 1
	// OwnerPlugin identifies a named plugin as the cursor owner.
	OwnerPlugin OwnerKind = 2
)

// String returns a stable label for logging.
func (o OwnerKind) String() string {
	switch o {
	case OwnerHost:
		return "host"
	case OwnerPlugin:
		return "plugin"
	default:
		return "unspecified"
	}
}

// Owner is the typed discriminator for cursor body type.
type Owner struct {
	Kind       OwnerKind
	PluginName string // populated iff Kind == OwnerPlugin
}

// HostCursor is the body of a host-owned cursor.
type HostCursor struct {
	Seq uint64
	ID  ulid.ULID
}

// Cursor is the host-internal cursor representation. Encode/Decode marshal
// to/from the wire bytes.
type Cursor struct {
	Version uint32
	Epoch   uint64
	Owner   Owner
	Host    *HostCursor // populated when Owner.Kind == OwnerHost
	Plugin  []byte      // populated when Owner.Kind == OwnerPlugin
}

// CurrentEpoch returns the host's current epoch. Today: always 0. The
// rebuild tool (holomush-6nds) will set this from a stored sentinel when
// it lands.
func CurrentEpoch() uint64 { return 0 }

// Encode serializes a Cursor to opaque bytes. Returns EVENTBUS_CURSOR_INVALID
// on validation failure (mismatched body for owner, missing host body, etc.).
func Encode(c Cursor) ([]byte, error) {
	if c.Version == 0 {
		c.Version = CurrentVersion
	}
	pb := &cursorv1.Cursor{
		Version: c.Version,
		Epoch:   c.Epoch,
		Owner:   ownerToProto(c.Owner),
	}
	switch c.Owner.Kind {
	case OwnerHost:
		if c.Host == nil {
			return nil, oops.Code("EVENTBUS_CURSOR_INVALID").
				Errorf("host owner requires non-nil HostCursor body")
		}
		pb.Body = &cursorv1.Cursor_Host{
			Host: &cursorv1.HostCursor{
				Seq: c.Host.Seq,
				Id:  c.Host.ID[:],
			},
		}
	case OwnerPlugin:
		if c.Owner.PluginName == "" {
			return nil, oops.Code("EVENTBUS_CURSOR_INVALID").
				Errorf("plugin owner requires non-empty PluginName")
		}
		pb.Body = &cursorv1.Cursor_PluginInner{PluginInner: c.Plugin}
	default:
		return nil, oops.Code("EVENTBUS_CURSOR_INVALID").
			With("owner_kind", uint8(c.Owner.Kind)).
			Errorf("unknown owner kind")
	}
	out, err := proto.Marshal(pb)
	if err != nil {
		return nil, oops.Code("EVENTBUS_CURSOR_INVALID").Wrap(err)
	}
	return out, nil
}

// Decode parses opaque cursor bytes. Returns EVENTBUS_CURSOR_INVALID on
// any parse / version / discriminator failure.
func Decode(b []byte) (Cursor, error) {
	if len(b) == 0 {
		return Cursor{}, oops.Code("EVENTBUS_CURSOR_INVALID").
			Errorf("empty cursor bytes")
	}
	var pb cursorv1.Cursor
	if err := proto.Unmarshal(b, &pb); err != nil {
		return Cursor{}, oops.Code("EVENTBUS_CURSOR_INVALID").Wrap(err)
	}
	if pb.GetVersion() != CurrentVersion {
		return Cursor{}, oops.Code("EVENTBUS_CURSOR_INVALID").
			With("version", pb.GetVersion()).
			With("current", CurrentVersion).
			Errorf("unsupported cursor version")
	}
	out := Cursor{
		Version: pb.GetVersion(),
		Epoch:   pb.GetEpoch(),
		Owner:   ownerFromProto(pb.GetOwner()),
	}
	switch out.Owner.Kind {
	case OwnerHost:
		hc := pb.GetHost()
		if hc == nil {
			return Cursor{}, oops.Code("EVENTBUS_CURSOR_INVALID").
				Errorf("host owner missing HostCursor body")
		}
		if len(hc.GetId()) != 16 {
			return Cursor{}, oops.Code("EVENTBUS_CURSOR_INVALID").
				With("id_len", len(hc.GetId())).
				Errorf("HostCursor.id must be 16 bytes")
		}
		var id ulid.ULID
		copy(id[:], hc.GetId())
		out.Host = &HostCursor{Seq: hc.GetSeq(), ID: id}
	case OwnerPlugin:
		if out.Owner.PluginName == "" {
			return Cursor{}, oops.Code("EVENTBUS_CURSOR_INVALID").
				Errorf("plugin owner missing plugin_name")
		}
		out.Plugin = pb.GetPluginInner()
	default:
		return Cursor{}, oops.Code("EVENTBUS_CURSOR_INVALID").
			With("owner_kind", uint8(out.Owner.Kind)).
			Errorf("unknown owner kind")
	}
	return out, nil
}

func ownerToProto(o Owner) *cursorv1.Owner {
	pb := &cursorv1.Owner{PluginName: o.PluginName}
	switch o.Kind {
	case OwnerHost:
		pb.Kind = cursorv1.OwnerKind_OWNER_KIND_HOST
	case OwnerPlugin:
		pb.Kind = cursorv1.OwnerKind_OWNER_KIND_PLUGIN
	default:
		pb.Kind = cursorv1.OwnerKind_OWNER_KIND_UNSPECIFIED
	}
	return pb
}

func ownerFromProto(pb *cursorv1.Owner) Owner {
	if pb == nil {
		return Owner{Kind: OwnerUnspecified}
	}
	o := Owner{PluginName: pb.GetPluginName()}
	switch pb.GetKind() {
	case cursorv1.OwnerKind_OWNER_KIND_HOST:
		o.Kind = OwnerHost
	case cursorv1.OwnerKind_OWNER_KIND_PLUGIN:
		o.Kind = OwnerPlugin
	default:
		o.Kind = OwnerUnspecified
	}
	return o
}
