-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Communication plugin: handles say, pose, emit commands
-- Uses the holo stdlib for typed context and event emission.

function on_command(ctx)
    if ctx.name == "say" then
        return handle_say(ctx)
    elseif ctx.name == "pose" then
        return handle_pose(ctx)
    elseif ctx.name == "emit" then
        return handle_emit(ctx)
    end
    return nil
end

function handle_say(ctx)
    if ctx.args == "" then
        return nil
    end
    local msg = ctx.character_name .. ' says, "' .. ctx.args .. '"'
    holo.emit.location(ctx.location_id, "say", {
        message = msg,
        speaker = ctx.character_name
    })
    return holo.emit.flush()
end

function handle_pose(ctx)
    if ctx.args == "" then
        return nil
    end
    local separator = " "
    if ctx.invoked_as == ";" then
        separator = ""
    end
    -- Handle fallback for : or ; prefix in args
    local action = ctx.args
    local first_char = action:sub(1, 1)
    if ctx.invoked_as ~= ":" and ctx.invoked_as ~= ";" then
        if first_char == ":" then
            separator = " "
            action = action:sub(2)
        elseif first_char == ";" then
            separator = ""
            action = action:sub(2)
        end
    end
    if action == "" then
        return nil
    end
    local msg = ctx.character_name .. separator .. action
    holo.emit.location(ctx.location_id, "pose", {
        message = msg,
        actor = ctx.character_name
    })
    return holo.emit.flush()
end

function handle_emit(ctx)
    if ctx.args == "" then
        return nil
    end
    holo.emit.location(ctx.location_id, "emit", {
        message = ctx.args
    })
    return holo.emit.flush()
end
