-- Echo bot: repeats messages back to the room
-- Demonstrates basic event handling and response

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

    -- Extract message from JSON payload
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
