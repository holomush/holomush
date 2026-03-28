-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Communication plugin: handles say, pose, emit, whisper commands
-- Uses the holo stdlib for typed context and event emission.

function on_command(ctx)
    if ctx.name == "say" then
        return handle_say(ctx)
    elseif ctx.name == "pose" then
        return handle_pose(ctx)
    elseif ctx.name == "emit" then
        return handle_emit(ctx)
    elseif ctx.name == "whisper" or ctx.name == "w" then
        return handle_whisper(ctx)
    end
    return nil
end

function handle_say(ctx)
    if ctx.args == "" then
        holo.emit.character(ctx.character_id, "error", {
            message = "What do you want to say?"
        })
        return holo.emit.flush()
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
        holo.emit.character(ctx.character_id, "error", {
            message = "What do you want to do?"
        })
        return holo.emit.flush()
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
        holo.emit.character(ctx.character_id, "error", {
            message = "What do you want to do?"
        })
        return holo.emit.flush()
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
        holo.emit.character(ctx.character_id, "error", {
            message = "What do you want to emit?"
        })
        return holo.emit.flush()
    end
    holo.emit.location(ctx.location_id, "emit", {
        message = ctx.args
    })
    return holo.emit.flush()
end

function handle_whisper(ctx)
    -- Parse target and message from args.
    local target_name
    local message
    local eq_pos = ctx.args:find("=", 1, true)
    if eq_pos then
        target_name = ctx.args:sub(1, eq_pos - 1)
        message = ctx.args:sub(eq_pos + 1)
    elseif ctx.invoked_as == "w" then
        -- Short form: use last whispered target.
        if not ctx.last_whispered or ctx.last_whispered == "" then
            holo.emit.character(ctx.character_id, "error", {
                message = "Whisper to whom? Use: whisper <name>=<message>"
            })
            return holo.emit.flush()
        end
        target_name = ctx.last_whispered
        message = ctx.args
    else
        holo.emit.character(ctx.character_id, "error", {
            message = "Usage: whisper <name>=<message> or w <message>"
        })
        return holo.emit.flush()
    end

    if target_name == "" then
        holo.emit.character(ctx.character_id, "error", {
            message = "Whisper to whom? Use: whisper <name>=<message>"
        })
        return holo.emit.flush()
    end

    if message == "" then
        holo.emit.character(ctx.character_id, "error", {
            message = "What do you want to whisper?"
        })
        return holo.emit.flush()
    end

    -- Find target session.
    local target, find_err = holo.session.find_by_name(target_name)
    if find_err then
        holo.emit.character(ctx.character_id, "error", {
            message = "An internal error occurred. Please try again."
        })
        return holo.emit.flush()
    end
    if target == nil then
        holo.emit.character(ctx.character_id, "error", {
            message = 'No one named "' .. target_name .. '" is connected.'
        })
        return holo.emit.flush()
    end

    -- Same-location check.
    if target.location_id ~= ctx.location_id then
        holo.emit.character(ctx.character_id, "error", {
            message = 'You don\'t see anyone named "' .. target_name .. '" here.'
        })
        return holo.emit.flush()
    end

    -- Detect pose mode.
    local is_pose = false
    local pose_space = " "
    local first_char = message:sub(1, 1)
    if first_char == ":" then
        is_pose = true
        pose_space = " "
        message = message:sub(2)
    elseif first_char == ";" then
        is_pose = true
        pose_space = ""
        message = message:sub(2)
    end

    if message == "" then
        holo.emit.character(ctx.character_id, "error", {
            message = "What do you want to whisper?"
        })
        return holo.emit.flush()
    end

    -- Emit location notice (no content revealed).
    holo.emit.location(ctx.location_id, "whisper", {
        sender_name = ctx.character_name,
        target_name = target.character_name,
        notice = ctx.character_name .. " whispers to " .. target.character_name .. "."
    })

    -- Emit to target.
    local target_msg
    if is_pose then
        target_msg = "From nearby, " .. ctx.character_name .. pose_space .. message
    else
        target_msg = ctx.character_name .. ' whispers, "' .. message .. '"'
    end
    holo.emit.character(target.character_id, "whisper", {
        sender_id = ctx.character_id,
        sender_name = ctx.character_name,
        message = target_msg,
        is_pose = is_pose
    })

    -- Emit confirmation to sender.
    local sender_msg
    if is_pose then
        sender_msg = "You whisper-pose to " .. target.character_name .. ": " .. message
    else
        sender_msg = "You whisper to " .. target.character_name .. ": " .. message
    end
    holo.emit.character(ctx.character_id, "command_response", {
        text = sender_msg
    })

    -- Record last whispered target.
    holo.session.set_last_whispered(ctx.session_id, target.character_name)

    return holo.emit.flush()
end
