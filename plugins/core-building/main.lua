-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- core-building: provides the dig and link commands for world building.

-- Host-brokered capability tables (holomush-eykuh.4). Dotted globals are
-- accessed via _G[...] (the name "world.query" is NOT a field of a "world"
-- global). Methods take a single proto-request table (snake_case proto field
-- keys) and return (proto_response_table, err_or_nil).
local world_query = _G["world.query"]
local world_mutation = _G["world.mutation"]

local DIG_USAGE = 'Usage: dig <exit> to "<location>" [return <exit>]'
local LINK_USAGE = "Usage: link <exit> to <target>"

-- trim removes leading and trailing whitespace.
local function trim(s)
    return s:match("^%s*(.-)%s*$")
end

-- parse_dig parses: <exit> to "<location>" [return <exit>]
-- Returns exitName, locationName, returnExit (may be nil) or nil, err.
local function parse_dig(args)
    -- With return exit
    local exit_name, loc_name, return_exit = args:match('^(%S+)%s+to%s+"([^"]+)"%s+return%s+(%S+)$')
    if exit_name then
        return exit_name, loc_name, return_exit
    end
    -- Without return exit
    exit_name, loc_name = args:match('^(%S+)%s+to%s+"([^"]+)"%s*$')
    if exit_name then
        return exit_name, loc_name, nil
    end
    return nil, nil, DIG_USAGE
end

-- parse_link parses: <exit> to <target>
-- Returns exitName, target or nil, err.
local function parse_link(args)
    local exit_name, target = args:match("^(%S+)%s+to%s+(.+)$")
    if not exit_name then
        return nil, nil, LINK_USAGE
    end
    target = trim(target)
    -- Strip surrounding quotes if present.
    if #target >= 2 and target:sub(1,1) == '"' and target:sub(-1) == '"' then
        target = target:sub(2, -2)
    end
    return exit_name, target, nil
end

-- resolve_location looks up a location by ID (prefixed with #) or by name.
-- Returns loc table or nil, err_message.
local function resolve_location(target)
    if target == "#" then
        return nil, "missing location ID after #"
    end
    if target:sub(1, 1) == "#" then
        local id = target:sub(2)
        local loc, err = world_query.QueryLocation({location_id = id})
        if err then
            holomush.log("error", "link: failed to query location " .. id .. ": " .. err)
            return nil, 'unable to find location "' .. id .. '"'
        end
        return loc, nil
    end

    local loc, err = world_query.FindLocation({name = target})
    if err then
        holomush.log("error", "link: failed to find location " .. target .. ": " .. err)
        return nil, 'unable to find location "' .. target .. '"'
    end
    return loc, nil
end

-- handle_dig implements the dig command.
local function handle_dig(ctx)
    local args = trim(ctx.args or "")
    if args == "" then
        return {status = 1, output = DIG_USAGE}
    end

    local exit_name, loc_name, return_exit, parse_err = parse_dig(args)
    if not exit_name then
        return {status = 1, output = parse_err}
    end

    local loc, err = world_mutation.CreateLocation({name = loc_name, description = "", type = "persistent"})
    if err then
        holomush.log("error", 'dig: failed to create location "' .. loc_name .. '": ' .. err)
        return {status = 2, output = "Unable to create location right now. Please try again."}
    end

    local exit_req = {from_id = ctx.location_id, to_id = loc.id, name = exit_name}
    if return_exit then
        exit_req.bidirectional = true
        exit_req.return_name = return_exit
    end

    local _, exit_err = world_mutation.CreateExit(exit_req)
    if exit_err then
        holomush.log("error", 'dig: location created but exit "' .. exit_name .. '" failed: ' .. exit_err)
        return {status = 2, output = "Location created but exit failed. Please try again."}
    end

    local msg = 'Created "' .. loc_name .. '" with exit "' .. exit_name .. '"'
    if return_exit then
        msg = msg .. ' and return exit "' .. return_exit .. '"'
    end
    msg = msg .. "."

    return msg
end

-- handle_link implements the link command.
local function handle_link(ctx)
    local args = trim(ctx.args or "")
    if args == "" then
        return {status = 1, output = LINK_USAGE}
    end

    local exit_name, target, parse_err = parse_link(args)
    if not exit_name then
        return {status = 1, output = parse_err}
    end

    local target_loc, resolve_err = resolve_location(target)
    if not target_loc then
        return {status = 1, output = resolve_err}
    end

    local _, exit_err = world_mutation.CreateExit({from_id = ctx.location_id, to_id = target_loc.id, name = exit_name})
    if exit_err then
        holomush.log("error", 'link: failed to create exit "' .. exit_name .. '": ' .. exit_err)
        return {status = 2, output = "Unable to create exit right now. Please try again."}
    end

    return 'Linked "' .. exit_name .. '" to "' .. target_loc.name .. '".'
end

function on_command(ctx)
    local cmd = ctx.command
    if cmd == "dig" then
        return handle_dig(ctx)
    elseif cmd == "link" then
        return handle_link(ctx)
    else
        return {status = 1, output = "Unknown building command: " .. (cmd or "")}
    end
end
