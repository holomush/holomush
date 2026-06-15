-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- core-communication: provides say, pose, ooc, emit, page, whisper, pemit, wall.

-- Host-brokered capability tables (holomush-eykuh.4). The session lookups live
-- on the brokered `session` global (PascalCase proto-RPC methods); broadcast is
-- on the separate `session.admin` global. Both are accessed via _G[...]; methods
-- take a single proto-request table and return (proto_response_table, err).
--
-- session_caps.FindByName{name=...} returns {session = {…}} (the entity nested
-- under `session`, absent when no active session matches), unlike the legacy
-- session.find_by_name which returned the entity directly.
local session_caps = _G["session"]
local session_admin = _G["session.admin"]

-- INV-PLUGIN-32: register the 8 event types this plugin can emit.
-- These MUST match plugin.yaml's crypto.emits block exactly.
holomush.register_emit_type("say")
holomush.register_emit_type("pose")
holomush.register_emit_type("ooc")
holomush.register_emit_type("emit")
holomush.register_emit_type("page")
holomush.register_emit_type("whisper")
holomush.register_emit_type("pemit")
holomush.register_emit_type("whisper_notice")

-- ---------------------------------------------------------------------------
-- Helpers
-- ---------------------------------------------------------------------------

-- trim removes leading and trailing whitespace.
local function trim(s)
    return s:match("^%s*(.-)%s*$")
end

-- json_escape escapes a string for use inside a JSON string value.
-- Escapes backslashes, double-quotes, and control characters.
local function json_escape(s)
    s = s:gsub("\\", "\\\\")
    s = s:gsub('"', '\\"')
    s = s:gsub("\n", "\\n")
    s = s:gsub("\r", "\\r")
    s = s:gsub("\t", "\\t")
    return s
end

-- json_string wraps a value as a quoted JSON string.
local function json_string(s)
    return '"' .. json_escape(s) .. '"'
end

-- json_bool encodes a Lua boolean as a JSON boolean.
local function json_bool(b)
    if b then return "true" else return "false" end
end

-- error_response returns a table representing an error response.
local function error_response(msg)
    return {status = 1, output = msg}
end

-- failure_response returns a table representing a transient failure response.
local function failure_response(msg)
    return {status = 2, output = msg}
end

-- ok_events returns a table with events and optional output.
local function ok_events(events, output)
    return {status = 0, output = output or "", events = events}
end

-- ---------------------------------------------------------------------------
-- say
-- ---------------------------------------------------------------------------

local function handle_say(ctx)
    local msg = ctx.args
    if trim(msg) == "" then
        return error_response("What do you want to say?")
    end

    local payload = '{"character_name":' .. json_string(ctx.character_name) ..
                    ',"message":' .. json_string(msg) .. '}'

    return ok_events({
        {subject ="location." .. ctx.location_id, type = "core-communication:say", payload = payload}
    })
end

-- ---------------------------------------------------------------------------
-- pose
-- ---------------------------------------------------------------------------

local function handle_pose(ctx)
    local args = trim(ctx.args or "")

    -- Check for pose-prefix variants embedded in args (:action or ;action).
    -- The invoked_as field tells us which prefix was used by the alias.
    local action = args
    local no_space = false

    if ctx.invoked_as == ";" then
        -- Prefix alias ';' sets no_space; args holds the raw action (no prefix).
        no_space = true
    elseif ctx.invoked_as == ":" then
        -- Prefix alias ':' is regular pose; args holds the raw action.
        no_space = false
    else
        -- Not invoked via prefix alias — check if args itself starts with : or ;.
        if args:sub(1, 1) == ";" then
            no_space = true
            action = trim(args:sub(2))
        elseif args:sub(1, 1) == ":" then
            no_space = false
            action = trim(args:sub(2))
        end
    end

    if action == "" then
        return error_response("What do you want to pose?")
    end

    local payload = '{"character_name":' .. json_string(ctx.character_name) ..
                    ',"action":' .. json_string(action)
    if no_space then
        payload = payload .. ',"no_space":true'
    end
    payload = payload .. '}'

    return ok_events({
        {subject ="location." .. ctx.location_id, type = "core-communication:pose", payload = payload}
    })
end

-- ---------------------------------------------------------------------------
-- ooc
-- ---------------------------------------------------------------------------

local function handle_ooc(ctx)
    local msg = trim(ctx.args or "")
    if msg == "" then
        return error_response("Usage: ooc <message>")
    end

    local style, text
    if msg:sub(1, 1) == ":" then
        style = "pose"
        text = trim(msg:sub(2))
    elseif msg:sub(1, 1) == ";" then
        style = "semipose"
        text = trim(msg:sub(2))
    else
        style = "say"
        text = msg
    end

    if text == "" then
        return error_response("Usage: ooc <message>")
    end

    local payload = '{"character_name":' .. json_string(ctx.character_name) ..
                    ',"message":' .. json_string(text) ..
                    ',"style":' .. json_string(style) .. '}'

    return ok_events({
        {subject ="location." .. ctx.location_id, type = "core-communication:ooc", payload = payload}
    })
end

-- ---------------------------------------------------------------------------
-- emit
-- ---------------------------------------------------------------------------

local function handle_emit(ctx)
    local msg = trim(ctx.args or "")
    if msg == "" then
        return error_response("What do you want to emit?")
    end

    local loc = ctx.location_id or ""
    if loc == "" or loc == "00000000000000000000000000" then
        return error_response("You must be in a location to emit.")
    end

    local payload = '{"message":' .. json_string(msg) .. '}'

    return ok_events({
        {subject ="location." .. loc, type = "core-communication:emit", payload = payload}
    })
end

-- ---------------------------------------------------------------------------
-- page
-- ---------------------------------------------------------------------------

local function handle_page(ctx)
    local args = trim(ctx.args or "")
    if args == "" then
        return error_response("Usage: page <name>=<message>")
    end

    if not session_caps then
        return error_response("This command requires session access which is not yet available.")
    end

    local target_name, raw_message
    local use_last_paged = false

    local eq = args:find("=", 1, true)
    if eq and eq > 1 then
        target_name = trim(args:sub(1, eq - 1))
        raw_message = args:sub(eq + 1) -- do NOT trim: leading : or ; is meaningful
    elseif eq == 1 then
        return error_response("Usage: page <name>=<message>")
    else
        raw_message = args
        use_last_paged = true
    end

    if trim(raw_message or "") == "" then
        return error_response("Usage: page <name>=<message>")
    end

    -- Resolve target name from last-paged if needed.
    if use_last_paged then
        local sender_resp, err = session_caps.FindByName({name = ctx.character_name})
        if err then
            holomush.log("error", "page: failed to find sender session: " .. err)
            return failure_response("Unable to page right now. Please try again.")
        end
        local sender_session = sender_resp and sender_resp.session
        if not sender_session or sender_session.last_whispered == "" then
            return error_response("You have no last-paged character. Use: page <name>=<message>")
        end
        target_name = sender_session.last_whispered
    end

    -- Look up target session.
    local target_resp, target_err = session_caps.FindByName({name = target_name})
    if target_err then
        holomush.log("error", "page: failed to find session for " .. target_name .. ": " .. target_err)
        return failure_response('Unable to reach "' .. target_name .. '" right now. Please try again.')
    end
    local target_session = target_resp and target_resp.session
    if not target_session then
        return error_response('No one named "' .. target_name .. '" is connected.')
    end

    -- Determine pose vs. normal message.
    local is_pose = false
    local formatted_for_target, formatted_for_sender

    if raw_message:sub(1, 1) == ":" then
        local action = trim(raw_message:sub(2))
        if action == "" then
            return error_response("Usage: page <name>=:<action>")
        end
        is_pose = true
        formatted_for_target = "From afar, " .. ctx.character_name .. " " .. action
        formatted_for_sender = "Long distance to " .. target_session.character_name .. ": " .. ctx.character_name .. " " .. action

    elseif raw_message:sub(1, 1) == ";" then
        local action = trim(raw_message:sub(2))
        if action == "" then
            return error_response("Usage: page <name>=;<action>")
        end
        is_pose = true
        formatted_for_target = "From afar, " .. ctx.character_name .. action
        formatted_for_sender = "Long distance to " .. target_session.character_name .. ": " .. ctx.character_name .. action

    else
        formatted_for_target = ctx.character_name .. " pages: " .. raw_message
        formatted_for_sender = "You paged " .. target_session.character_name .. ": " .. raw_message
    end

    -- Build page event payload for target's character stream.
    local payload = '{"sender_id":' .. json_string(ctx.character_id) ..
                    ',"sender_name":' .. json_string(ctx.character_name) ..
                    ',"message":' .. json_string(formatted_for_target) ..
                    ',"is_pose":' .. json_bool(is_pose) .. '}'

    -- Update last-paged on the sender's session.
    if ctx.session_id and ctx.session_id ~= "" then
        local _, set_err = session_caps.SetLastWhispered({session_id = ctx.session_id, name = target_session.character_name})
        if set_err then
            holomush.log("warn", "page: failed to update last-whispered: " .. set_err)
        end
    end

    return ok_events(
        {{subject ="character." .. target_session.character_id, type = "core-communication:page", payload = payload, sensitive = true}},
        formatted_for_sender
    )
end

-- ---------------------------------------------------------------------------
-- whisper
-- ---------------------------------------------------------------------------

local function handle_whisper(ctx)
    local args = trim(ctx.args or "")
    if args == "" then
        return error_response("Usage: whisper <name>=<message>")
    end

    if not session_caps then
        return error_response("This command requires session access which is not yet available.")
    end

    local target_name, message

    local eq = args:find("=", 1, true)
    if eq and eq > 1 then
        target_name = trim(args:sub(1, eq - 1))
        message = args:sub(eq + 1)
    elseif ctx.invoked_as == "w" then
        -- Short form: use last whispered target.
        local sender_resp, err = session_caps.FindByName({name = ctx.character_name})
        if err then
            holomush.log("error", "whisper: failed to find sender session: " .. err)
            return failure_response("Unable to whisper right now. Please try again.")
        end
        local sender_session = sender_resp and sender_resp.session
        if not sender_session or sender_session.last_whispered == "" then
            return error_response("Whisper to whom? Use: whisper <name>=<message>")
        end
        target_name = sender_session.last_whispered
        message = args
    else
        return error_response("Usage: whisper <name>=<message> or w <message>")
    end

    if not target_name or target_name == "" then
        return error_response("Whisper to whom? Use: whisper <name>=<message>")
    end
    if not message or message == "" then
        return error_response("What do you want to whisper?")
    end

    -- Find target session.
    local target_resp, target_err = session_caps.FindByName({name = target_name})
    if target_err then
        holomush.log("error", "whisper: failed to find session for " .. target_name .. ": " .. target_err)
        return failure_response('Unable to reach "' .. target_name .. '" right now. Please try again.')
    end
    local target = target_resp and target_resp.session
    if not target then
        return error_response('No one named "' .. target_name .. '" is connected.')
    end

    -- Reject location-less whispers.
    local loc = ctx.location_id or ""
    if loc == "" or loc == "00000000000000000000000000" then
        return error_response("You must be in a location to whisper.")
    end

    -- Same-location check.
    if target.location_id ~= loc then
        return error_response('You don\'t see anyone named "' .. target_name .. '" here.')
    end

    -- Detect pose mode.
    local is_pose = false
    local pose_space = " "
    local first = message:sub(1, 1)
    if first == ":" then
        is_pose = true
        pose_space = " "
        message = message:sub(2)
    elseif first == ";" then
        is_pose = true
        pose_space = ""
        message = message:sub(2)
    end

    if message == "" then
        return error_response("What do you want to whisper?")
    end

    -- Build target message.
    local target_msg
    if is_pose then
        target_msg = "From nearby, " .. ctx.character_name .. pose_space .. message
    else
        target_msg = ctx.character_name .. ' whispers, "' .. message .. '"'
    end

    -- Build sender confirmation.
    local sender_msg
    if is_pose then
        sender_msg = "You whisper-pose to " .. target.character_name .. ": " .. message
    else
        sender_msg = "You whisper to " .. target.character_name .. ": " .. message
    end

    -- Build notice payload for location (content not revealed).
    local notice_payload = '{"sender_name":' .. json_string(ctx.character_name) ..
                           ',"target_name":' .. json_string(target.character_name) ..
                           ',"notice":' .. json_string(ctx.character_name .. " whispers to " .. target.character_name .. ".") .. '}'

    -- Build whisper payload for target.
    local whisper_payload = '{"sender_id":' .. json_string(ctx.character_id) ..
                            ',"sender_name":' .. json_string(ctx.character_name) ..
                            ',"message":' .. json_string(target_msg) ..
                            ',"is_pose":' .. json_bool(is_pose) .. '}'

    -- Record last whispered target.
    if ctx.session_id and ctx.session_id ~= "" then
        local _, set_err = session_caps.SetLastWhispered({session_id = ctx.session_id, name = target.character_name})
        if set_err then
            holomush.log("warn", "whisper: failed to update last-whispered: " .. set_err)
        end
    end

    return ok_events(
        {
            {subject ="location." .. loc, type = "core-communication:whisper_notice", payload = notice_payload},
            {subject ="character." .. target.character_id, type = "core-communication:whisper", payload = whisper_payload, sensitive = true},
        },
        sender_msg
    )
end

-- ---------------------------------------------------------------------------
-- pemit
-- ---------------------------------------------------------------------------

local function handle_pemit(ctx)
    local args = trim(ctx.args or "")

    if not session_caps then
        return error_response("This command requires session access which is not yet available.")
    end

    local eq = args:find("=", 1, true)
    if not eq or eq <= 1 then
        return error_response("Usage: pemit <character>=<message>")
    end

    local target_name = trim(args:sub(1, eq - 1))
    local message = args:sub(eq + 1)

    if trim(message) == "" then
        return error_response("Usage: pemit <character>=<message>")
    end

    -- Resolve target session by character name.
    local target_resp, err = session_caps.FindByName({name = target_name})
    if err then
        holomush.log("error", "pemit: failed to find session for " .. target_name .. ": " .. err)
        return failure_response('Unable to reach "' .. target_name .. '" right now. Please try again.')
    end
    local target_session = target_resp and target_resp.session
    if not target_session then
        return error_response('No character found named "' .. target_name .. '".')
    end

    local payload = '{"sender_id":' .. json_string(ctx.character_id) ..
                    ',"sender_name":' .. json_string(ctx.character_name) ..
                    ',"target_id":' .. json_string(target_session.character_id) ..
                    ',"message":' .. json_string(message) .. '}'

    return ok_events(
        {{subject ="character." .. target_session.character_id, type = "core-communication:pemit", payload = payload, sensitive = true}},
        "Pemit sent to " .. target_session.character_name .. "."
    )
end

-- ---------------------------------------------------------------------------
-- wall
-- ---------------------------------------------------------------------------

local urgency_prefixes = {
    info     = "[ADMIN ANNOUNCEMENT]",
    warning  = "[ADMIN WARNING]",
    critical = "[ADMIN CRITICAL]",
}

local function parse_wall_args(args)
    local first_word, rest = args:match("^(%S+)%s+(.+)$")
    if first_word then
        local lower_word = first_word:lower()
        if lower_word == "info" then
            return "info", rest
        elseif lower_word == "warning" or lower_word == "warn" then
            return "warning", rest
        elseif lower_word == "critical" or lower_word == "crit" then
            return "critical", rest
        end
    end

    -- Check single word case (only urgency keyword with no message).
    local single = args:lower()
    if single == "info" then
        return "info", ""
    elseif single == "warning" or single == "warn" then
        return "warning", ""
    elseif single == "critical" or single == "crit" then
        return "critical", ""
    end

    return "info", args
end

local function handle_wall(ctx)
    local args = trim(ctx.args or "")
    if args == "" then
        return error_response("Usage: wall [info|warning|critical] <message>")
    end

    if not session_admin then
        return error_response("This command requires session access which is not yet available.")
    end

    local urgency, message = parse_wall_args(args)
    if message == "" then
        return error_response("Usage: wall [info|warning|critical] <message>")
    end

    -- ListActive returns {sessions = {…}}; extract the array for the count below.
    local sessions
    if session_caps then
        local list_resp, list_err = session_caps.ListActive({})
        if list_err then
            holomush.log("warn", "wall: failed to list sessions: " .. list_err)
        else
            sessions = list_resp and list_resp.sessions
        end
    end

    local prefix = urgency_prefixes[urgency]
    local announcement = prefix .. " " .. ctx.character_name .. ": " .. message

    holomush.log("info", "admin wall: admin=" .. ctx.character_name ..
                          " urgency=" .. urgency ..
                          " sessions=" .. (sessions and #sessions or 0) ..
                          " message=" .. message)

    local _, bc_err = session_admin.Broadcast({message = announcement})
    if bc_err then
        holomush.log("error", "wall: failed to broadcast: " .. bc_err)
        return failure_response("Unable to broadcast announcement right now. Please try again.")
    end

    local output
    if not sessions then
        output = "Announcement broadcast."
    else
        local count = #sessions
        local word = "sessions"
        if count == 1 then word = "session" end
        output = "Announcement sent to " .. count .. " " .. word .. "."
    end

    return {status = 0, output = output}
end

-- ---------------------------------------------------------------------------
-- Dispatcher
-- ---------------------------------------------------------------------------

function on_command(ctx)
    local cmd = ctx.command
    if cmd == "say" then
        return handle_say(ctx)
    elseif cmd == "pose" then
        return handle_pose(ctx)
    elseif cmd == "ooc" then
        return handle_ooc(ctx)
    elseif cmd == "emit" then
        return handle_emit(ctx)
    elseif cmd == "page" then
        return handle_page(ctx)
    elseif cmd == "whisper" then
        return handle_whisper(ctx)
    elseif cmd == "pemit" then
        return handle_pemit(ctx)
    elseif cmd == "wall" then
        return handle_wall(ctx)
    else
        return error_response("Unknown communication command: " .. (cmd or ""))
    end
end
