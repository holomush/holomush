"""Echo plugin - responds to say events with echoed message."""

import json
import extism


@extism.plugin_fn
def handle_event():
    """Handle incoming events and emit echo responses."""
    # Read input event
    event_json = extism.input_str()
    event = json.loads(event_json)

    # Only respond to "say" events from characters (not plugins)
    if event.get("type") != "say":
        return

    if event.get("actor_kind") == 2:  # ActorKindPlugin
        return

    # Extract message from payload
    payload = json.loads(event.get("payload", "{}"))
    message = payload.get("message", "")

    if not message:
        return

    # Create echo response
    response = {
        "events": [{
            "stream": event.get("stream"),
            "type": "say",
            "payload": json.dumps({"message": f"Echo: {message}"})
        }]
    }

    extism.output_str(json.dumps(response))
