-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Echo bot: repeats messages back to the room
-- Demonstrates basic event handling and response
--
-- NOTE: This is an example plugin using simple pattern matching for JSON.
-- For production plugins requiring complex JSON handling, use a proper JSON library.
-- Limitations: does not handle escaped quotes or special characters in messages.

function on_event(event)
    -- Only respond to say events
    if event.type ~= "say" then
        return nil
    end

    -- Don't echo plugin messages (prevents loops)
    if event.actor_kind == "plugin" then
        return nil
    end

    -- Parse payload to get message
    -- Payload format: {"message":"..."}
    local payload = event.payload
    if not payload then
        return nil
    end

    -- Extract message from JSON payload using simple pattern matching
    -- Note: This pattern does not handle escaped quotes in messages
    local msg = payload:match('"message":"([^"]*)"')
    if not msg or msg == "" then
        return nil
    end

    -- Return echo event
    return {
        {
            stream = event.stream,
            type = "say",
            payload = '{"message":"Echo: ' .. msg .. '"}'
        }
    }
end
