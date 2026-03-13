"""Malformed JSON plugin - returns invalid JSON for testing error handling."""

import extism


@extism.plugin_fn
def handle_event():
    """Return malformed JSON to test error handling in host."""
    # Return invalid JSON (missing closing brace)
    extism.output_str('{"events": [{"stream": "test"')
