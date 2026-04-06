-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- core-aliases: provides alias, unalias, aliases, sysalias, sysunsalias, sysaliases.

-- ---------------------------------------------------------------------------
-- Helpers
-- ---------------------------------------------------------------------------

-- trim removes leading and trailing whitespace.
local function trim(s)
    return s:match("^%s*(.-)%s*$")
end

-- error_response returns a table representing an error response.
local function error_response(msg)
    return {status = 1, output = msg}
end

-- failure_response returns a table representing a transient failure response.
local function failure_response(msg)
    return {status = 2, output = msg}
end

-- ok_response returns a successful response with text output.
local function ok_response(msg)
    return {status = 0, output = msg}
end

-- alias_available checks whether the alias capability is wired in.
local function alias_available()
    return alias ~= nil
end

-- parse_alias_definition parses "name=command" format.
-- Returns alias, command on success, or nil, nil, errmsg on failure.
local function parse_alias_definition(args)
    args = trim(args)
    local eq = args:find("=", 1, true)
    if not eq then
        return nil, nil, "usage: <alias>=<command>"
    end
    local name = trim(args:sub(1, eq - 1))
    local cmd  = trim(args:sub(eq + 1))
    if name == "" then
        return nil, nil, "alias name cannot be empty"
    end
    if cmd == "" then
        return nil, nil, "command cannot be empty"
    end
    return name, cmd, nil
end

-- valid alias name pattern: letters, digits, hyphens, underscores, plus signs.
local alias_name_pattern = "^[%a%d_%-+]+$"

-- validate_alias_name returns an error message if the name is invalid, or nil.
local function validate_alias_name(name)
    if not name:match(alias_name_pattern) then
        return 'invalid alias name "' .. name .. '": must contain only letters, digits, hyphens, underscores, or plus signs'
    end
    return nil
end

-- find_alias searches a list of {alias, command} tables for a matching name.
-- Returns command string and true on match, or nil, false if not found.
local function find_alias(entries, name)
    for _, e in ipairs(entries) do
        if e.alias == name then
            return e.command, true
        end
    end
    return nil, false
end

-- sort_aliases sorts an array of {alias, command} tables by alias name (in-place).
local function sort_aliases(entries)
    table.sort(entries, function(a, b) return a.alias < b.alias end)
end

-- ---------------------------------------------------------------------------
-- alias — create or update a player alias
-- ---------------------------------------------------------------------------

local function handle_alias(ctx)
    if not alias_available() then
        return error_response("Alias management requires the alias service which is not yet available.")
    end

    local name, cmd, parse_err = parse_alias_definition(ctx.args or "")
    if parse_err then
        return error_response(parse_err)
    end

    local name_err = validate_alias_name(name)
    if name_err then
        return error_response(name_err)
    end

    local warnings = {}

    -- Check if alias shadows an existing command.
    local shadow_info, shadow_err = alias.check_shadow(name)
    if shadow_err then
        holomush.log("error", "alias: failed to check shadow for " .. name .. ": " .. shadow_err)
        return failure_response("Unable to create alias right now. Please try again.")
    end
    if shadow_info and shadow_info.shadows then
        warnings[#warnings + 1] = "Warning: '" .. name .. "' is an existing command. Your alias will override it."
    end

    -- Check if alias shadows a system alias.
    local sys_aliases, sys_err = alias.list_system()
    if sys_err then
        holomush.log("error", "alias: failed to list system aliases: " .. sys_err)
        return failure_response("Unable to create alias right now. Please try again.")
    end
    local sys_cmd, found_sys = find_alias(sys_aliases, name)
    if found_sys then
        warnings[#warnings + 1] = "Warning: '" .. name .. "' is a system alias for '" .. sys_cmd .. "'. Your alias will take precedence."
    end

    -- Check if replacing an existing player alias.
    local player_aliases, player_err = alias.list_player(ctx.player_id)
    if player_err then
        holomush.log("error", "alias: failed to list player aliases: " .. player_err)
        return failure_response("Unable to create alias right now. Please try again.")
    end
    local existing_cmd, found_existing = find_alias(player_aliases, name)
    if found_existing then
        warnings[#warnings + 1] = "Warning: Replacing existing alias '" .. name .. "' (was: '" .. existing_cmd .. "')."
    end

    -- Set the alias.
    local _, set_err = alias.set_player(ctx.player_id, name, cmd)
    if set_err then
        holomush.log("error", "alias: failed to set alias " .. name .. ": " .. set_err)
        return failure_response("Unable to create alias right now. Please try again.")
    end

    local out = ""
    for _, w in ipairs(warnings) do
        out = out .. w .. "\n"
    end
    out = out .. "Alias '" .. name .. "' added: " .. cmd .. "\n"
    return ok_response(out)
end

-- ---------------------------------------------------------------------------
-- unalias — remove a player alias
-- ---------------------------------------------------------------------------

local function handle_unalias(ctx)
    if not alias_available() then
        return error_response("Alias management requires the alias service which is not yet available.")
    end

    local name = trim(ctx.args or "")
    if name == "" then
        return error_response("Usage: unalias <alias>")
    end

    -- Check if alias exists before removing.
    local player_aliases, list_err = alias.list_player(ctx.player_id)
    if list_err then
        holomush.log("error", "unalias: failed to list player aliases: " .. list_err)
        return failure_response("Unable to remove alias right now. Please try again.")
    end
    local _, found = find_alias(player_aliases, name)
    if not found then
        return error_response("No alias '" .. name .. "' found.")
    end

    local _, del_err = alias.delete_player(ctx.player_id, name)
    if del_err then
        holomush.log("error", "unalias: failed to delete alias " .. name .. ": " .. del_err)
        return failure_response("Unable to remove alias right now. Please try again.")
    end

    return ok_response("Alias '" .. name .. "' removed.\n")
end

-- ---------------------------------------------------------------------------
-- aliases — list all player aliases
-- ---------------------------------------------------------------------------

local function handle_aliases(ctx)
    if not alias_available() then
        return error_response("Alias management requires the alias service which is not yet available.")
    end

    local entries, list_err = alias.list_player(ctx.player_id)
    if list_err then
        holomush.log("error", "aliases: failed to list player aliases: " .. list_err)
        return failure_response("Unable to list aliases right now. Please try again.")
    end

    if #entries == 0 then
        return ok_response("You have no aliases defined.")
    end

    sort_aliases(entries)

    local out = "Your aliases:\n"
    for _, e in ipairs(entries) do
        out = out .. "  " .. e.alias .. " = " .. e.command .. "\n"
    end
    return ok_response(out)
end

-- ---------------------------------------------------------------------------
-- sysalias — create or update a system alias
-- ---------------------------------------------------------------------------

local function handle_sysalias(ctx)
    if not alias_available() then
        return error_response("Alias management requires the alias service which is not yet available.")
    end

    local name, cmd, parse_err = parse_alias_definition(ctx.args or "")
    if parse_err then
        return error_response(parse_err)
    end

    local name_err = validate_alias_name(name)
    if name_err then
        return error_response(name_err)
    end

    -- Block if shadowing an existing system alias.
    local sys_aliases, sys_err = alias.list_system()
    if sys_err then
        holomush.log("error", "sysalias: failed to list system aliases: " .. sys_err)
        return failure_response("Unable to create system alias right now. Please try again.")
    end
    local existing_cmd, found_sys = find_alias(sys_aliases, name)
    if found_sys then
        return error_response("'" .. name .. "' shadows existing system alias for '" .. existing_cmd .. "'. Use 'sysunsalias " .. name .. "' first.")
    end

    local warnings = {}

    -- Check if alias shadows a registered command.
    local shadow_info, shadow_err = alias.check_shadow(name)
    if shadow_err then
        holomush.log("error", "sysalias: failed to check shadow for " .. name .. ": " .. shadow_err)
        return failure_response("Unable to create system alias right now. Please try again.")
    end
    if shadow_info and shadow_info.shadows then
        warnings[#warnings + 1] = "Warning: '" .. name .. "' is an existing command. System alias will override it."
    end

    -- Re-check to guard against TOCTOU races.
    local sys_aliases2, sys_err2 = alias.list_system()
    if sys_err2 then
        holomush.log("error", "sysalias: failed to re-check system aliases: " .. sys_err2)
        return failure_response("Unable to create system alias right now. Please try again.")
    end
    local existing_cmd2, found_sys2 = find_alias(sys_aliases2, name)
    if found_sys2 then
        return error_response("'" .. name .. "' shadows existing system alias for '" .. existing_cmd2 .. "'. Use 'sysunsalias " .. name .. "' first.")
    end

    -- Set the system alias.
    local _, set_err = alias.set_system(name, cmd, ctx.character_id)
    if set_err then
        holomush.log("error", "sysalias: failed to set system alias " .. name .. ": " .. set_err)
        return failure_response("Unable to create system alias right now. Please try again.")
    end

    local out = ""
    for _, w in ipairs(warnings) do
        out = out .. w .. "\n"
    end
    out = out .. "System alias '" .. name .. "' added: " .. cmd .. "\n"
    return ok_response(out)
end

-- ---------------------------------------------------------------------------
-- sysunsalias — remove a system alias
-- ---------------------------------------------------------------------------

local function handle_sysunsalias(ctx)
    if not alias_available() then
        return error_response("Alias management requires the alias service which is not yet available.")
    end

    local name = trim(ctx.args or "")
    if name == "" then
        return error_response("Usage: sysunsalias <alias>")
    end

    -- Check if alias exists before removing.
    local sys_aliases, list_err = alias.list_system()
    if list_err then
        holomush.log("error", "sysunsalias: failed to list system aliases: " .. list_err)
        return failure_response("Unable to remove system alias right now. Please try again.")
    end
    local _, found = find_alias(sys_aliases, name)
    if not found then
        return error_response("No system alias '" .. name .. "' found.")
    end

    local _, del_err = alias.delete_system(name)
    if del_err then
        holomush.log("error", "sysunsalias: failed to delete system alias " .. name .. ": " .. del_err)
        return failure_response("Unable to remove system alias right now. Please try again.")
    end

    return ok_response("System alias '" .. name .. "' removed.\n")
end

-- ---------------------------------------------------------------------------
-- sysaliases — list all system aliases
-- ---------------------------------------------------------------------------

local function handle_sysaliases(_ctx)
    if not alias_available() then
        return error_response("Alias management requires the alias service which is not yet available.")
    end

    local entries, list_err = alias.list_system()
    if list_err then
        holomush.log("error", "sysaliases: failed to list system aliases: " .. list_err)
        return failure_response("Unable to list system aliases right now. Please try again.")
    end

    if #entries == 0 then
        return ok_response("No system aliases defined.")
    end

    sort_aliases(entries)

    local out = "System aliases:\n"
    for _, e in ipairs(entries) do
        out = out .. "  " .. e.alias .. " = " .. e.command .. "\n"
    end
    return ok_response(out)
end

-- ---------------------------------------------------------------------------
-- Dispatcher
-- ---------------------------------------------------------------------------

function on_command(ctx)
    local cmd = ctx.command
    if cmd == "alias" then
        return handle_alias(ctx)
    elseif cmd == "unalias" then
        return handle_unalias(ctx)
    elseif cmd == "aliases" then
        return handle_aliases(ctx)
    elseif cmd == "sysalias" then
        return handle_sysalias(ctx)
    elseif cmd == "sysunsalias" then
        return handle_sysunsalias(ctx)
    elseif cmd == "sysaliases" then
        return handle_sysaliases(ctx)
    else
        return failure_response("Unknown alias command: " .. (cmd or ""))
    end
end
