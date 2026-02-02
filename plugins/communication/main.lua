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
    -- Payload format: {"name":"cmd","args":"msg","character_name":"Name","location_id":"id","invoked_as":"alias"}
    local cmd_name = payload:match('"name":"([^"]*)"')
    local args = payload:match('"args":"([^"]*)"')
    local character_name = payload:match('"character_name":"([^"]*)"')
    local location_id = payload:match('"location_id":"([^"]*)"')
    local invoked_as = payload:match('"invoked_as":"([^"]*)"')  -- Optional: original command before alias

    if not cmd_name then
        return nil
    end

    -- Validate required fields - these indicate a bug in the dispatcher if missing
    if not character_name or character_name == "" then
        -- Missing character_name is a fatal error - command context is invalid
        return nil
    end

    if not location_id or location_id == "" then
        -- Missing location_id is a fatal error - cannot emit to a stream
        return nil
    end

    -- Handle empty args gracefully (empty message is valid, handled per-command)
    args = args or ""

    local stream = "location:" .. location_id

    if cmd_name == "say" then
        return handle_say(stream, character_name, args)
    elseif cmd_name == "pose" then
        return handle_pose(stream, character_name, args, invoked_as)
    elseif cmd_name == "emit" then
        return handle_emit(stream, args)
    end

    return nil
end

-- handle_say processes the "say" command.
-- Output format: CharacterName says, "message"
-- Returns: array of emit events
function handle_say(stream, character_name, message)
    if message == "" then
        -- No message to say - return empty (no events emitted)
        return nil
    end

    -- Build the message others will see (always uses double quotes)
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
-- Supports pose variants:
--   pose waves     -> CharacterName waves (space before action)
--   :waves         -> CharacterName waves (: alias, still space)
--   ;'s eyes       -> CharacterName's eyes (; alias, no space for possessives)
--   pose :waves    -> CharacterName waves (: in args, still space)
--   pose ;'s eyes  -> CharacterName's eyes (; in args, no space)
-- The invoked_as parameter contains the original command before alias resolution.
-- Returns: array of emit events
function handle_pose(stream, character_name, action, invoked_as)
    if action == "" then
        -- No action to pose - return empty (no events emitted)
        return nil
    end

    -- Detect pose variant from invoked_as (prefix alias) or first char of args
    local separator = " "  -- Default: space before action

    -- Check if invoked via prefix alias (: or ;)
    if invoked_as == ";" then
        -- ; alias - no space (for possessives like 's)
        separator = ""
    elseif invoked_as == ":" then
        -- : alias - same as regular pose (space)
        separator = " "
    else
        -- Fallback: check first character of args for backwards compatibility
        local first_char = action:sub(1, 1)
        if first_char == ":" then
            -- : variant in args - same as regular pose (convenience)
            separator = " "
            action = action:sub(2)  -- Strip leading :
        elseif first_char == ";" then
            -- ; variant in args - no space (for possessives)
            separator = ""
            action = action:sub(2)  -- Strip leading ;
        end
    end

    -- Handle empty action after stripping variant indicator
    if action == "" then
        return nil
    end

    -- Build the posed action (character name prepended with appropriate separator)
    local posed_message = character_name .. separator .. action

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
