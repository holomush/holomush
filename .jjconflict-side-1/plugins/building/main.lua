-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Building plugin: handles dig, link commands for world topology
-- Uses holo stdlib for event emission and holomush.* for world mutations.

function on_command(ctx)
    if ctx.name == "dig" then
        return handle_dig(ctx)
    elseif ctx.name == "link" then
        return handle_link(ctx)
    end
    return nil
end

-- Parse: dig <exit> to "<location>" [return <exit>]
function parse_dig_args(args)
    -- Pattern: exit_name to "location_name" [return return_exit]
    local exit_name, location_name = args:match('^(%S+)%s+to%s+"([^"]+)"')
    if not exit_name or not location_name then
        return nil, 'Usage: dig <exit> to "<location>" [return <exit>]'
    end

    -- Check for optional return clause
    local return_exit = nil
    local remaining = args:match('^%S+%s+to%s+"[^"]+"(.*)$')
    if remaining then
        return_exit = remaining:match('%s+return%s+(%S+)')
    end

    return {
        exit_name = exit_name,
        location_name = location_name,
        return_exit = return_exit
    }, nil
end

function handle_dig(ctx)
    if ctx.args == "" then
        holo.emit.character(ctx.character_id, "error", {
            message = 'Usage: dig <exit> to "<location>" [return <exit>]'
        })
        return holo.emit.flush()
    end

    local parsed, err = parse_dig_args(ctx.args)
    if not parsed then
        holo.emit.character(ctx.character_id, "error", { message = err })
        return holo.emit.flush()
    end

    -- Create the new location
    local loc, loc_err = holomush.create_location(
        parsed.location_name,
        "", -- empty description initially
        "persistent"
    )
    if not loc then
        holo.emit.character(ctx.character_id, "error", {
            message = "Failed to create location: " .. (loc_err or "unknown error")
        })
        return holo.emit.flush()
    end

    -- Create exit from current location to new location
    local exit_opts = {}
    if parsed.return_exit then
        exit_opts.bidirectional = true
        exit_opts.return_name = parsed.return_exit
    end

    local exit, exit_err = holomush.create_exit(
        ctx.location_id,
        loc.id,
        parsed.exit_name,
        exit_opts
    )
    if not exit then
        holo.emit.character(ctx.character_id, "error", {
            message = "Location created but exit failed: " .. (exit_err or "unknown error")
        })
        return holo.emit.flush()
    end

    -- Success message
    local msg = string.format('Created "%s" with exit "%s"', parsed.location_name, parsed.exit_name)
    if parsed.return_exit then
        msg = msg .. string.format(' and return exit "%s"', parsed.return_exit)
    end
    msg = msg .. "."

    holo.emit.character(ctx.character_id, "system", { message = msg })
    return holo.emit.flush()
end

-- Parse: link <exit> to <target>
function parse_link_args(args)
    local exit_name, target = args:match('^(%S+)%s+to%s+(.+)$')
    if not exit_name or not target then
        return nil, "Usage: link <exit> to <target>"
    end

    -- Trim whitespace and handle quoted targets
    target = target:match('^%s*(.-)%s*$')
    -- Remove surrounding quotes if present
    local unquoted = target:match('^"([^"]+)"$')
    if unquoted then
        target = unquoted
    end

    return {
        exit_name = exit_name,
        target = target
    }, nil
end

function resolve_location(target)
    -- If starts with #, treat as ID
    if target:sub(1, 1) == "#" then
        local id = target:sub(2)
        local loc, err = holomush.query_room(id)
        if not loc then
            return nil, "Location not found: " .. id
        end
        return loc, nil
    end

    -- Otherwise, search by name
    local loc, err = holomush.find_location(target)
    if not loc then
        return nil, "Location not found: " .. target
    end
    return loc, nil
end

function handle_link(ctx)
    if ctx.args == "" then
        holo.emit.character(ctx.character_id, "error", {
            message = "Usage: link <exit> to <target>"
        })
        return holo.emit.flush()
    end

    local parsed, err = parse_link_args(ctx.args)
    if not parsed then
        holo.emit.character(ctx.character_id, "error", { message = err })
        return holo.emit.flush()
    end

    -- Resolve target location
    local target_loc, resolve_err = resolve_location(parsed.target)
    if not target_loc then
        holo.emit.character(ctx.character_id, "error", { message = resolve_err })
        return holo.emit.flush()
    end

    -- Create exit
    local exit, exit_err = holomush.create_exit(
        ctx.location_id,
        target_loc.id,
        parsed.exit_name,
        {}
    )
    if not exit then
        holo.emit.character(ctx.character_id, "error", {
            message = "Failed to create exit: " .. (exit_err or "unknown error")
        })
        return holo.emit.flush()
    end

    local msg = string.format('Linked "%s" to "%s".', parsed.exit_name, target_loc.name)
    holo.emit.character(ctx.character_id, "system", { message = msg })
    return holo.emit.flush()
end
