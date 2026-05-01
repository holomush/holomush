# core-communication — events

_Auto-generated from `plugins/core-communication/plugin.yaml` by `task docs:gen-events`. Do not edit._

| Event type | Sensitivity | Description |
| --- | --- | --- |
| `core-communication:emit` | never | Generic emit to the current location. |
| `core-communication:ooc` | never | Out-of-character speech in the current location. |
| `core-communication:page` | always | OOC private message between two characters; participants only. |
| `core-communication:pemit` | always | Storyteller-issued private narration to a single character. |
| `core-communication:pose` | never | Action description in the current location. |
| `core-communication:say` | never | Speech in the current location, visible to everyone present. |
| `core-communication:whisper` | always | In-character private message between two characters in the same location. |
| `core-communication:whisper_notice` | never | Public notice that a whisper occurred (no content), visible in the location. |

