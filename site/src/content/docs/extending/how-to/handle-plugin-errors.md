---
title: "Handle plugin errors"
---

Host functions return errors as a second return value. This guide shows how to
branch on the error type and surface server correlation IDs. For the full list
of error messages and their causes, see
[Host function error types](/extending/reference/plugin-api/#host-function-error-types).

## Branch on the error type

Inspect the returned error and respond appropriately — handle missing entities
gracefully, log permission errors, and surface correlation IDs:

```lua
function on_event(event)
-- location_id comes from your event handling (e.g. parsed from event.payload)
local location, err = holomush.query_location(location_id)
if err then
    if err:match("not found") then
        -- Handle missing entity gracefully
        holomush.log("debug", "Location not found: " .. location_id)
        return nil
    elseif err:match("access denied") then
        -- Permission error - likely missing ABAC policy
        holomush.log("warn", "Permission denied for location query")
        return nil
    elseif err:match("internal error") then
        -- Surface correlation ID to user for debugging
        holomush.log("error", "Server error - " .. err)
        return {
            {
                stream = event.stream,
                type = "system",
                payload = '{"message":"An error occurred. Reference: ' ..
                    err:match("ref: ([^)]+)") .. '"}'
            }
        }
    end
    return nil
end
-- Use location data safely
holomush.log("debug", "Found location: " .. location.name)
return nil
end
```

## Surface correlation IDs

When a host function returns an error like `"internal error (ref: 01JCXYZ...)"`:

1. The reference ID is a ULID that links to the server's error log.
2. Surface this ID to users so they can report it to operators.
3. Operators search logs for the ID to find the full stack trace and context.

This pattern enables efficient debugging of production issues without exposing
internal details to end users.

## Look up a correlation ID (operators)

When users report correlation IDs, search server logs to find the full error:

```bash
# Plain text logs
grep "error_id=01JCXYZ" /var/log/holomush/server.log

# Structured JSON logs
jq 'select(.error_id == "01JCXYZ...")' /var/log/holomush/server.json

# With journald
journalctl -u holomush | grep "error_id=01JCXYZ"
```

The log entry contains the full stack trace, original error message, and
additional context like the plugin name and operation being performed.
