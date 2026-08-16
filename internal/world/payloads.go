// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"encoding/json"
	"slices"

	"github.com/oklog/ulid/v2"

	"github.com/samber/oops"
)

// EntityType represents the type of entity being moved.
type EntityType string

// Valid entity types for move payloads.
const (
	EntityTypeCharacter EntityType = "character"
	EntityTypeObject    EntityType = "object"
)

// IsValid returns true if the entity type is a valid move entity.
func (e EntityType) IsValid() bool {
	return e == EntityTypeCharacter || e == EntityTypeObject
}

// ContainmentType represents where an entity is contained.
type ContainmentType string

// Valid containment types for move payloads.
const (
	ContainmentTypeLocation  ContainmentType = "location"
	ContainmentTypeCharacter ContainmentType = "character"
	ContainmentTypeObject    ContainmentType = "object"
	ContainmentTypeNone      ContainmentType = "none" // Used for first-time placements (no prior containment)
)

// IsValid returns true if the containment type is a valid destination (not "none").
func (c ContainmentType) IsValid() bool {
	return c == ContainmentTypeLocation || c == ContainmentTypeCharacter || c == ContainmentTypeObject
}

// IsValidOrNone returns true if the containment type is valid or "none" (for sources).
func (c ContainmentType) IsValidOrNone() bool {
	return c.IsValid() || c == ContainmentTypeNone
}

// MovePayload represents a move event for characters or objects.
type MovePayload struct {
	EntityType EntityType      `json:"entity_type"` // "character" | "object"
	EntityID   ulid.ULID       `json:"entity_id"`
	FromType   ContainmentType `json:"from_type"` // Source containment type; "none" for first-time placements
	FromID     *ulid.ULID      `json:"from_id,omitempty"`
	ToType     ContainmentType `json:"to_type"` // "location" | "character" | "object"
	ToID       ulid.ULID       `json:"to_id"`
	ExitID     *ulid.ULID      `json:"exit_id,omitempty"`
	ExitName   string          `json:"exit_name,omitempty"`
}

// Validate checks that the MovePayload has all required fields and valid values.
func (p *MovePayload) Validate() error {
	if p.EntityType == "" {
		return &ValidationError{Field: "entity_type", Message: "cannot be empty"}
	}
	if !p.EntityType.IsValid() {
		return &ValidationError{Field: "entity_type", Message: "must be 'character' or 'object'"}
	}
	if p.EntityID.IsZero() {
		return &ValidationError{Field: "entity_id", Message: "cannot be zero"}
	}
	if p.FromType == "" {
		return &ValidationError{Field: "from_type", Message: "cannot be empty"}
	}
	if !p.FromType.IsValidOrNone() {
		return &ValidationError{Field: "from_type", Message: "must be 'location', 'character', 'object', or 'none'"}
	}
	// FromID must be nil for first-time placements (FromType == "none")
	// and must be non-nil otherwise
	if p.FromType == ContainmentTypeNone {
		if p.FromID != nil {
			return &ValidationError{Field: "from_id", Message: "must be nil when from_type is 'none'"}
		}
	} else if p.FromID == nil {
		return &ValidationError{Field: "from_id", Message: "cannot be nil"}
	}
	if p.ToType == "" {
		return &ValidationError{Field: "to_type", Message: "cannot be empty"}
	}
	if !p.ToType.IsValid() {
		return &ValidationError{Field: "to_type", Message: "must be 'location', 'character', or 'object'"}
	}
	if p.ToID.IsZero() {
		return &ValidationError{Field: "to_id", Message: "cannot be zero"}
	}
	return nil
}

// NewMovePayload creates a validated MovePayload for entity movement.
// Use NewFirstPlacement instead when the entity has no prior containment (from_type="none").
func NewMovePayload(
	entityType EntityType,
	entityID ulid.ULID,
	fromType ContainmentType, fromID *ulid.ULID,
	toType ContainmentType, toID ulid.ULID,
) (*MovePayload, error) {
	p := &MovePayload{
		EntityType: entityType,
		EntityID:   entityID,
		FromType:   fromType,
		FromID:     fromID,
		ToType:     toType,
		ToID:       toID,
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// NewFirstPlacement creates a validated MovePayload for first-time entity placement.
// Sets FromType to "none" and FromID to nil.
func NewFirstPlacement(
	entityType EntityType,
	entityID ulid.ULID,
	toType ContainmentType, toID ulid.ULID,
) (*MovePayload, error) {
	return NewMovePayload(entityType, entityID, ContainmentTypeNone, nil, toType, toID)
}

// ObjectGivePayload represents an object transfer between characters.
type ObjectGivePayload struct {
	ObjectID        ulid.ULID `json:"object_id"`
	ObjectName      string    `json:"object_name"`
	FromCharacterID ulid.ULID `json:"from_character_id"`
	ToCharacterID   ulid.ULID `json:"to_character_id"`
}

// TargetType represents the type of entity being examined.
type TargetType string

// Valid target types for examine payloads.
const (
	TargetTypeLocation  TargetType = "location"
	TargetTypeObject    TargetType = "object"
	TargetTypeCharacter TargetType = "character"
)

// IsValid returns true if the target type is valid.
func (t TargetType) IsValid() bool {
	return t == TargetTypeLocation || t == TargetTypeObject || t == TargetTypeCharacter
}

// ExaminePayload represents an examine/look event.
type ExaminePayload struct {
	CharacterID ulid.ULID  `json:"character_id"` // Who is looking
	TargetType  TargetType `json:"target_type"`  // "location" | "object" | "character"
	TargetID    ulid.ULID  `json:"target_id"`    // What is being looked at
	TargetName  string     `json:"target_name"`  // Name of target (for logging)
	LocationID  ulid.ULID  `json:"location_id"`  // Where the look is happening
}

// Validate checks that the ExaminePayload has all required fields and valid values.
func (p *ExaminePayload) Validate() error {
	if p.CharacterID.IsZero() {
		return &ValidationError{Field: "character_id", Message: "cannot be zero"}
	}
	if p.TargetType == "" {
		return &ValidationError{Field: "target_type", Message: "cannot be empty"}
	}
	if !p.TargetType.IsValid() {
		return &ValidationError{Field: "target_type", Message: "must be 'location', 'object', or 'character'"}
	}
	if p.TargetID.IsZero() {
		return &ValidationError{Field: "target_id", Message: "cannot be zero"}
	}
	if p.LocationID.IsZero() {
		return &ValidationError{Field: "location_id", Message: "cannot be zero"}
	}
	return nil
}

// NewExaminePayload creates a validated ExaminePayload.
//
// Parameters:
//   - characterID: The character performing the examine action (who is looking)
//   - targetType: What type of thing is being examined (location, object, or character)
//   - targetID: The ID of the thing being examined
//   - locationID: Where the examine action is taking place (for event routing)
func NewExaminePayload(
	characterID ulid.ULID,
	targetType TargetType,
	targetID, locationID ulid.ULID,
) (*ExaminePayload, error) {
	p := &ExaminePayload{
		CharacterID: characterID,
		TargetType:  targetType,
		TargetID:    targetID,
		LocationID:  locationID,
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// Validate checks that the ObjectGivePayload has all required fields and valid values.
func (p *ObjectGivePayload) Validate() error {
	if p.ObjectID.IsZero() {
		return &ValidationError{Field: "object_id", Message: "cannot be zero"}
	}
	if p.ObjectName == "" {
		return &ValidationError{Field: "object_name", Message: "cannot be empty"}
	}
	if p.FromCharacterID.IsZero() {
		return &ValidationError{Field: "from_character_id", Message: "cannot be zero"}
	}
	if p.ToCharacterID.IsZero() {
		return &ValidationError{Field: "to_character_id", Message: "cannot be zero"}
	}
	if p.FromCharacterID == p.ToCharacterID {
		return &ValidationError{Field: "to_character_id", Message: "cannot give object to self"}
	}
	return nil
}

// NewObjectGivePayload creates a validated ObjectGivePayload.
//
// Parameters:
//   - objectID: The object being transferred
//   - fromCharacterID: The character giving the object (must own/hold it)
//   - toCharacterID: The character receiving the object (must not equal fromCharacterID)
//   - objectName: Display name of the object (for event messages)
//
// Note: Take care not to swap fromCharacterID and toCharacterID - the validation
// checks that they differ but cannot detect if they are reversed.
func NewObjectGivePayload(
	objectID, fromCharacterID, toCharacterID ulid.ULID,
	objectName string,
) (*ObjectGivePayload, error) {
	p := &ObjectGivePayload{
		ObjectID:        objectID,
		ObjectName:      objectName,
		FromCharacterID: fromCharacterID,
		ToCharacterID:   toCharacterID,
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// World-change envelope payloads (MODEL-04 mechanical rollout, 05-10).
//
// These are the intent-level, NEW-VALUES-ONLY payloads each location/exit/object
// write command persists in its outbox envelope. They are erasure-safe (no
// secrets) and carry only the committed new state (or, for a tombstone, the
// deleted id). The before/after versions and cascade IDs live in the envelope's
// affected-aggregates manifest — built by the writer from the repo's returned
// MutationDelta (finding 7), NOT from these payloads.

// LocationChangePayload is the new-values-only payload for a location create or
// update envelope.
type LocationChangePayload struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ExitChangePayload is the new-values-only payload for an exit create or update
// envelope. It carries the endpoints (from/to location) that define the exit.
type ExitChangePayload struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	FromLocationID string `json:"from_location_id"`
	ToLocationID   string `json:"to_location_id"`
}

// ObjectChangePayload is the new-values-only payload for an object create or
// update envelope.
type ObjectChangePayload struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ObjectMoveChangePayload is the new-values-only payload for an object-move
// envelope: the object and its destination containment, plus the source
// containment read before the move (omitted for a first-time placement).
type ObjectMoveChangePayload struct {
	ObjectID string  `json:"object_id"`
	ToType   string  `json:"to_type"`
	ToID     string  `json:"to_id"`
	FromType string  `json:"from_type,omitempty"`
	FromID   *string `json:"from_id,omitempty"`
}

// CharacterUpdateChangePayload is the new-values-only payload for a
// character_updated envelope (the character-description write). It carries the
// character id and the committed new description.
type CharacterUpdateChangePayload struct {
	CharacterID string `json:"character_id"`
	Description string `json:"description"`
}

// AuditContext is the evaluated admin section and action a world write was
// authorized under (01-SPEC §10.7), carried into the write's envelope payload so
// an auditor learns not only WHAT changed but through which admin surface.
//
// The ZERO value is the PLAYER path: a player-initiated write supplies no admin
// context, so both fields are empty. That is correct rather than a gap — the
// envelope Actor already distinguishes who acted, and one payload shape for "a
// character changed" is a better audit model than two split by actor flavour.
//
// It carries no player id and no acting-character id. Caller.subject already
// carries `player:<id>` verbatim into the envelope Actor (D-104), and recording
// the acting character would put a durable player-to-alt linkage into the
// RETAINED events_audit table, which D-104 exists to prevent.
type AuditContext struct {
	// Section is the admin section id the call was gated under (e.g.
	// "characters"), or empty for a player-initiated write.
	Section string
	// Action is the admin action evaluated for it ("read" / "write"), or empty
	// for a player-initiated write.
	Action string
}

// CharacterLifecycleChangePayload is the payload for the character lifecycle
// envelopes (character_retired / character_unretired). It carries the character
// id, the COMMITTED new status, the status the character LEFT, and the admin
// section/action the transition was evaluated under.
//
// BEFORE-STATUS IS A DELIBERATE EXCEPTION to the registry's new-values-only
// rule, and the exception is exactly as wide as its justification (D-103). A
// lifecycle status is a closed enum the server assigns — not player-authored
// content — so recording the value it left is not a durable copy of anything a
// player wrote, and an auditor reviewing a retire needs to know what state it
// interrupted. The PROSE half of the same decision goes the other way: see
// CharacterProfileUpdateChangePayload, which records names and never values.
//
// It is a distinct shape from CharacterUpdateChangePayload rather than a reuse:
// that payload declares a description field a lifecycle change never carries.
type CharacterLifecycleChangePayload struct {
	CharacterID string `json:"character_id"`
	Status      string `json:"status"`
	// BeforeStatus is the status the character held before the transition. It
	// always DIFFERS from Status in an emitted payload: the lifecycle guard
	// refuses an already-in-that-state transition before any payload is built,
	// and BuildCharacterLifecyclePayload refuses to describe one anyway.
	BeforeStatus string `json:"before_status"`
	// Section and Action are the evaluated admin context (§10.7). Both are
	// EMPTY for a player-initiated transition — see AuditContext.
	Section string `json:"section"`
	Action  string `json:"action"`
}

// CharacterProfileUpdateChangePayload is the new-values-only payload for a
// character_profile_update envelope. It carries the character id, the SORTED
// names of the profile attributes the write changed — creates, updates and
// clears alike — and the admin section/action the write was evaluated under.
// Never the values themselves.
//
// The four fields below are the WHOLE shape, and
// TestCharacterProfileUpdatePayloadExposesNoValueBearingField asserts both the
// marshalled key set and these reflected json tags against exactly that list. A
// value-bearing field added here fails that test immediately, which is the point.
type CharacterProfileUpdateChangePayload struct {
	CharacterID       string   `json:"character_id"`
	ChangedAttributes []string `json:"changed_attributes"`
	// Section and Action are the evaluated admin context (§10.7). Both are
	// EMPTY for a player-initiated write — see AuditContext.
	Section string `json:"section"`
	Action  string `json:"action"`
}

// TombstonePayload is the payload for a delete envelope: only the id of the
// deleted aggregate. Cascaded aggregates (a location's exits, a bidirectional
// exit's reverse) are represented in the envelope's affected-aggregates manifest
// (from the repo delta), not here — one envelope per command, not per cascaded row.
type TombstonePayload struct {
	ID string `json:"id"`
}

// BuildLocationPayload marshals the new-values-only location payload for a
// create/update envelope.
func BuildLocationPayload(loc *Location) ([]byte, error) {
	payload, err := json.Marshal(LocationChangePayload{
		ID:          loc.ID.String(),
		Name:        loc.Name,
		Description: loc.Description,
	})
	if err != nil {
		return nil, oops.Wrapf(err, "marshal location payload")
	}
	return payload, nil
}

// BuildExitPayload marshals the new-values-only exit payload for a create/update
// envelope.
func BuildExitPayload(exit *Exit) ([]byte, error) {
	payload, err := json.Marshal(ExitChangePayload{
		ID:             exit.ID.String(),
		Name:           exit.Name,
		FromLocationID: exit.FromLocationID.String(),
		ToLocationID:   exit.ToLocationID.String(),
	})
	if err != nil {
		return nil, oops.Wrapf(err, "marshal exit payload")
	}
	return payload, nil
}

// BuildObjectPayload marshals the new-values-only object payload for a
// create/update envelope.
func BuildObjectPayload(obj *Object) ([]byte, error) {
	payload, err := json.Marshal(ObjectChangePayload{
		ID:          obj.ID.String(),
		Name:        obj.Name,
		Description: obj.Description,
	})
	if err != nil {
		return nil, oops.Wrapf(err, "marshal object payload")
	}
	return payload, nil
}

// BuildObjectMovePayload marshals the new-values-only object-move payload from the
// object's pre-move containment (from) and the destination containment (to).
func BuildObjectMovePayload(obj *Object, to Containment) ([]byte, error) {
	var toID string
	if id := to.ID(); id != nil {
		toID = id.String()
	}
	p := ObjectMoveChangePayload{
		ObjectID: obj.ID.String(),
		ToType:   string(to.Type()),
		ToID:     toID,
	}
	if fromType, fromID := currentContainment(obj); fromType != ContainmentTypeNone {
		p.FromType = string(fromType)
		fromStr := fromID.String()
		p.FromID = &fromStr
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return nil, oops.Wrapf(err, "marshal object move payload")
	}
	return payload, nil
}

// currentContainment returns the object's current containment type and id, or
// (ContainmentTypeNone, zero) when the object has no prior containment.
func currentContainment(obj *Object) (ContainmentType, ulid.ULID) {
	switch {
	case obj.LocationID() != nil:
		return ContainmentTypeLocation, *obj.LocationID()
	case obj.HeldByCharacterID() != nil:
		return ContainmentTypeCharacter, *obj.HeldByCharacterID()
	case obj.ContainedInObjectID() != nil:
		return ContainmentTypeObject, *obj.ContainedInObjectID()
	default:
		return ContainmentTypeNone, ulid.ULID{}
	}
}

// BuildCharacterUpdatePayload marshals the new-values-only character-update
// payload (character id + committed description) for a character_updated envelope.
func BuildCharacterUpdatePayload(characterID ulid.ULID, description string) ([]byte, error) {
	payload, err := json.Marshal(CharacterUpdateChangePayload{
		CharacterID: characterID.String(),
		Description: description,
	})
	if err != nil {
		return nil, oops.Wrapf(err, "marshal character update payload")
	}
	return payload, nil
}

// BuildCharacterLifecyclePayload marshals the character lifecycle payload
// (character id + the status left + the committed new status + the evaluated
// admin context) for a character_retired / character_unretired envelope.
//
// auditCtx is the ZERO AuditContext for a player-initiated transition, which
// yields empty section and action.
//
// IT REFUSES AN EQUAL-VALUED TRANSITION. The lifecycle guard in
// RetireCharacter / UnretireCharacter already refuses a character that is
// ALREADY in the requested state, so reaching here with beforeStatus == status
// means that guard was bypassed or a caller passed the wrong read. Emitting the
// payload anyway would write a durable audit row into a RETAINED table claiming
// a change nobody made, and a retained row cannot be recalled — so the builder
// refuses rather than describing a transition that did not happen.
func BuildCharacterLifecyclePayload(characterID ulid.ULID, beforeStatus, status Status, auditCtx AuditContext) ([]byte, error) {
	if beforeStatus == status {
		return nil, oops.Code("CHARACTER_LIFECYCLE_PAYLOAD_NO_TRANSITION").
			With("character_id", characterID.String()).
			With("status", string(status)).
			Errorf("refusing to build a lifecycle payload whose before-status equals its status")
	}
	payload, err := json.Marshal(CharacterLifecycleChangePayload{
		CharacterID:  characterID.String(),
		Status:       string(status),
		BeforeStatus: string(beforeStatus),
		Section:      auditCtx.Section,
		Action:       auditCtx.Action,
	})
	if err != nil {
		return nil, oops.Wrapf(err, "marshal character lifecycle payload")
	}
	return payload, nil
}

// BuildCharacterProfileUpdatePayload marshals the new-values-only character
// profile payload (character id + the NAMES of the profile attributes the write
// changed + the evaluated admin context) for a character_profile_update
// envelope.
//
// The VALUES are deliberately absent. Profile prose is player-authored personal
// content and the taxonomy's payload rule is new-values-only AND erasure-safe: a
// consumer needs to know that a profile changed and which fields did, and reads
// the current values through the authorized read path where the per-attribute
// tier floor (01-SPEC §8.6) still applies.
//
// # Why not encrypt the values instead (D-103)
//
// It was considered and rejected. events_audit is a RETAINED table: encrypting a
// row does not make it erasable, because retained-is-still-retained and
// encryption is not erasure. Crypto-shredding — dropping the key so the
// ciphertext becomes unreadable — IS a real answer to that, and it is a separate
// mechanism this milestone does not ship. Widening this payload later is
// additive and cheap; NARROWING it is not, because prose already written into a
// retained partition cannot be recalled. An auditor still learns who, when,
// which character and which fields.
//
// # description travels here as a NAME
//
// When an admin edits the in-world `characters.description` alongside the twelve
// `profile.*` fields, "description" appears in changedAttributes as a bare NAME
// like any other. Its VALUE reaches no field of this payload. That is why the
// admin write routes through UpdateCharacterProfileAttributes rather than
// UpdateCharacterDescription, whose kindCharacterUpdated payload declares a
// `description` STRING — the prose value itself.
//
// auditCtx is the ZERO AuditContext for a player-initiated write, which yields
// empty section and action.
//
// AN EMPTY changedAttributes IS ACCEPTED, deliberately. The shipped player path
// emits one on an all-identical resubmit and documents it as both representable
// and honest; erroring here would convert a live player success into a failure.
func BuildCharacterProfileUpdatePayload(characterID ulid.ULID, changedAttributes []string, auditCtx AuditContext) ([]byte, error) {
	names := make([]string, len(changedAttributes))
	copy(names, changedAttributes)
	slices.Sort(names)
	payload, err := json.Marshal(CharacterProfileUpdateChangePayload{
		CharacterID:       characterID.String(),
		ChangedAttributes: names,
		Section:           auditCtx.Section,
		Action:            auditCtx.Action,
	})
	if err != nil {
		return nil, oops.Wrapf(err, "marshal character profile update payload")
	}
	return payload, nil
}

// BuildTombstonePayload marshals the tombstone payload (the deleted id) for a
// delete envelope.
func BuildTombstonePayload(id ulid.ULID) ([]byte, error) {
	payload, err := json.Marshal(TombstonePayload{ID: id.String()})
	if err != nil {
		return nil, oops.Wrapf(err, "marshal tombstone payload")
	}
	return payload, nil
}
