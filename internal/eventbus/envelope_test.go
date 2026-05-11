// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
)

func TestEnvelope_AccessorsAndDefensiveCopy(t *testing.T) {
	t.Parallel()

	eventID := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	subject := "events.test.subject"
	evType := "test.event.type"
	codecName := codec.Name("aead-aes256-gcm")
	keyID := codec.KeyID(42)
	keyVersion := uint32(7)
	timestamp := time.Date(2026, 5, 11, 12, 34, 56, 0, time.UTC)
	payload := []byte{0x01, 0x02, 0x03, 0x04}

	type ctor struct {
		name  string
		build func(p []byte) eventbus.Envelope
	}
	ctors := []ctor{
		{
			name: "NewEnvelopeFromColdRow",
			build: func(p []byte) eventbus.Envelope {
				return eventbus.NewEnvelopeFromColdRow(eventbus.ColdRow{
					EventID:    eventID,
					Subject:    subject,
					Type:       evType,
					Payload:    p,
					Codec:      string(codecName),
					KeyID:      keyID,
					KeyVersion: keyVersion,
					Timestamp:  timestamp,
				})
			},
		},
		{
			name: "NewEnvelopeFromFields",
			build: func(p []byte) eventbus.Envelope {
				return eventbus.NewEnvelopeFromFields(eventbus.EnvelopeFields{
					EventID:    eventID,
					Subject:    subject,
					Type:       evType,
					Timestamp:  timestamp,
					Codec:      codecName,
					KeyID:      keyID,
					KeyVersion: keyVersion,
					Payload:    p,
				})
			},
		},
	}

	for _, c := range ctors {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			// Construct with a mutable input slice.
			input := make([]byte, len(payload))
			copy(input, payload)
			env := c.build(input)

			// Accessors return populated values.
			require.Equal(t, eventID, env.EventID())
			require.Equal(t, subject, env.Subject())
			require.Equal(t, evType, env.Type())
			require.Equal(t, codecName, env.Codec())
			require.Equal(t, keyID, env.KeyID())
			require.Equal(t, keyVersion, env.KeyVersion())
			require.True(t, timestamp.Equal(env.Timestamp()))
			require.Equal(t, payload, env.Payload())

			// Mutating input slice after construction MUST NOT affect envelope.
			for i := range input {
				input[i] = 0xFF
			}
			require.Equal(t, payload, env.Payload(),
				"mutating input slice after construction must not affect envelope payload")

			// Mutating a returned Payload() copy MUST NOT affect subsequent reads.
			got := env.Payload()
			for i := range got {
				got[i] = 0xAA
			}
			require.Equal(t, payload, env.Payload(),
				"mutating returned Payload() slice must not affect subsequent Payload() reads")
		})
	}

	// copyBytes nil/empty short-circuit: both nil and []byte{} payloads return nil.
	emptyCases := []struct {
		name    string
		payload []byte
	}{
		{name: "nil payload", payload: nil},
		{name: "empty payload", payload: []byte{}},
	}
	for _, ec := range emptyCases {
		t.Run("FromFields/"+ec.name, func(t *testing.T) {
			t.Parallel()
			env := eventbus.NewEnvelopeFromFields(eventbus.EnvelopeFields{
				EventID: eventID,
				Payload: ec.payload,
			})
			require.Nil(t, env.Payload(), "empty/nil payload must return nil from Payload()")
		})
		t.Run("FromColdRow/"+ec.name, func(t *testing.T) {
			t.Parallel()
			env := eventbus.NewEnvelopeFromColdRow(eventbus.ColdRow{
				EventID: eventID,
				Payload: ec.payload,
			})
			require.Nil(t, env.Payload(), "empty/nil payload must return nil from Payload()")
		})
	}
}
