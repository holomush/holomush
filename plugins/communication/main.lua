-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Communication plugin: handles say, pose, emit commands
--
-- This plugin processes command events and emits communication events to the
-- location stream. It returns emit events in the format expected by the Lua host.
--
-- Command payload format: {"name":"say|pose|emit","args":"message","character_name":"Name","location_id":"ulid"}
-- Return format: Array of emit events [{stream, type, payload}, ...]
-- Events go to stream "location:<location_id>"
--
-- NOTE: This plugin uses simple pattern matching for JSON parsing.
-- It does not handle escaped quotes or special characters in messages.

-- on_event handles incoming events.
-- For command events, it processes say, pose, and emit commands.
function on_event(event)
    -- Only handle command events
    if event.type ~= "command" then
        return nil
    end

    -- Parse the command payload
    local payload = event.payload
    if not payload or payload == "" then
        return nil
    end

    -- Extract command fields using pattern matching
    -- Payload format: {"name":"cmd","args":"msg","character_name":"Name","location_id":"id"}
    local cmd_name = payload:match('"name":"([^"]*)"')
    local args = payload:match('"args":"([^"]*)"')
    local character_name = payload:match('"character_name":"([^"]*)"')
    local location_id = payload:match('"location_id":"([^"]*)"')

    if not cmd_name then
        return nil
    end

    -- Handle empty args gracefully
    args = args or ""
    character_name = character_name or "Someone"
    location_id = location_id or ""

    local stream = "location:" .. location_id

    if cmd_name == "say" then
        return handle_say(stream, character_name, args)
    elseif cmd_name == "pose" then
        return handle_pose(stream, character_name, args)
    elseif cmd_name == "emit" then
        return handle_emit(stream, args)
    end

    return nil
end

-- handle_say processes the "say" command.
-- Emits a say event to the location stream.
-- Returns: array of emit events
function handle_say(stream, character_name, message)
    if message == "" then
        -- No message to say - return empty (no events emitted)
        return nil
    end

    -- Build the message others will see
    local others_message = character_name .. ' says, "' .. message .. '"'

    -- Emit event to location for other players
    -- The dispatcher will format output for the speaker separately
    return {
        {
            stream = stream,
            type = "say",
            payload = '{"message":"' .. escape_json(others_message) .. '","speaker":"' .. escape_json(character_name) .. '"}'
        }
    }
end

-- handle_pose processes the "pose" command.
-- Emits a pose event to the location stream.
-- Returns: array of emit events
function handle_pose(stream, character_name, action)
    if action == "" then
        -- No action to pose - return empty (no events emitted)
        return nil
    end

    -- Build the posed action (character name prepended)
    local posed_message = character_name .. " " .. action

    -- Emit event to location for other players
    return {
        {
            stream = stream,
            type = "pose",
            payload = '{"message":"' .. escape_json(posed_message) .. '","actor":"' .. escape_json(character_name) .. '"}'
        }
    }
end

-- handle_emit processes the "emit" command.
-- Emits an emit event to the location stream.
-- Returns: array of emit events
function handle_emit(stream, text)
    if text == "" then
        -- No text to emit - return empty (no events emitted)
        return nil
    end

    -- Emit raw text to location (no prefix)
    return {
        {
            stream = stream,
            type = "emit",
            payload = '{"message":"' .. escape_json(text) .. '"}'
        }
    }
end

-- escape_json escapes special characters for JSON string values.
-- Handles backslash, double quote, and control characters.
function escape_json(str)
    if not str then
        return ""
    end
    -- Escape backslashes first, then quotes
    str = str:gsub("\\", "\\\\")
    str = str:gsub('"', '\\"')
    -- Escape control characters
    str = str:gsub("\n", "\\n")
    str = str:gsub("\r", "\\r")
    str = str:gsub("\t", "\\t")
    return str
end
