// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

// Sensitivity classifies an event type's payload protection contract.
//
//   - SensitivityAlways: every event of this type MUST be emitted with
//     Sensitive=true. Emit-time enforcement lands in Phase 3.
//   - SensitivityMay: the emit-site decides per-event via the Sensitive
//     flag. The plugin's emit code carries the runtime decision.
//   - SensitivityNever: the event type is never sensitive. Emit-time
//     attempts to set Sensitive=true on this type are rejected.
type Sensitivity string

// Sensitivity contract values declared in the manifest's crypto.emits block.
const (
	SensitivityAlways Sensitivity = "always"
	SensitivityMay    Sensitivity = "may"
	SensitivityNever  Sensitivity = "never"
)

// CryptoSection is the manifest's `crypto:` block. Optional; absence
// means the plugin emits no sensitive events and consumes no sensitive
// subjects (every emit is treated as if declared SensitivityNever).
type CryptoSection struct {
	Emits    []CryptoEmit    `yaml:"emits,omitempty" json:"emits,omitempty"`
	Consumes []CryptoConsume `yaml:"consumes,omitempty" json:"consumes,omitempty"`
}

// CryptoEmit declares one event type this plugin emits, plus its
// sensitivity contract.
type CryptoEmit struct {
	EventType   string      `yaml:"event_type" json:"event_type"`
	Sensitivity Sensitivity `yaml:"sensitivity" json:"sensitivity"`
	Description string      `yaml:"description,omitempty" json:"description,omitempty"`
}

// CryptoConsume declares a set of subjects the plugin subscribes to and,
// per-event-type, whether the plugin requests plaintext (decryption) for
// sensitive events of those types.
//
// Phase 1 records this declaration but does not enforce it at runtime.
// Phase 3's AuthGuard reads it.
type CryptoConsume struct {
	Subjects           []string `yaml:"subjects" json:"subjects"`
	RequestsDecryption []string `yaml:"requests_decryption,omitempty" json:"requests_decryption,omitempty"`
}
