"""Empty-stream test plugin - emits events with empty stream names.

This plugin is used exclusively for testing stream validation in ExtismSubscriber.
It should NOT be used in production.
"""

import json
import extism


@extism.plugin_fn
def handle_event():
    """Handle incoming events and emit response with empty stream."""
    # Read input event
    event_json = extism.input_str()
    event = json.loads(event_json)

    # Only respond to "say" events from characters (not plugins)
    if event.get("type") != "say":
        return

    if event.get("actor_kind") == 2:  # ActorKindPlugin
        return

    # Create response with EMPTY stream name - this should be rejected
    response = {
        "events": [{
            "stream": "",  # Empty stream - should be rejected by validation
            "type": "say",
            "payload": json.dumps({"message": "This should be rejected"})
        }]
    }

    extism.output_str(json.dumps(response))
