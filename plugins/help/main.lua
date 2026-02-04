-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Help plugin: provides help command for listing and viewing command documentation
-- Uses holomush.list_commands() and holomush.get_command_help() host functions.

function on_command(ctx)
    if ctx.name == "help" then
        return handle_help(ctx)
    end
    return nil
end

function handle_help(ctx)
    local args = trim(ctx.args)

    if args == "" then
        -- List all commands
        return list_commands(ctx)
    end

    -- Check for search subcommand
    local search_term = args:match("^search%s+(.+)$")
    if search_term then
        return search_commands(ctx, search_term)
    end

    -- Show help for specific command
    return show_command_help(ctx, args)
end

-- List all available commands (filtered by character capabilities)
function list_commands(ctx)
    local commands, err = holomush.list_commands(ctx.character_id)
    if err then
        holo.emit.character(ctx.character_id, "error", {
            message = "Error listing commands: " .. err
        })
        return holo.emit.flush()
    end

    if not commands or #commands == 0 then
        holo.emit.character(ctx.character_id, "info", {
            message = "No commands available."
        })
        return holo.emit.flush()
    end

    -- Group commands by source
    local by_source = {}
    for _, cmd in ipairs(commands) do
        local source = cmd.source or "other"
        if not by_source[source] then
            by_source[source] = {}
        end
        table.insert(by_source[source], cmd)
    end

    -- Build output
    local output = holo.fmt.header("Available Commands") .. "\n\n"

    -- Sort sources for consistent display
    local sources = {}
    for source, _ in pairs(by_source) do
        table.insert(sources, source)
    end
    table.sort(sources)

    for _, source in ipairs(sources) do
        local cmds = by_source[source]
        output = output .. holo.fmt.bold(capitalize(source)) .. "\n"

        -- Build rows for table
        local rows = {}
        for _, cmd in ipairs(cmds) do
            table.insert(rows, {cmd.name, cmd.help or ""})
        end

        output = output .. holo.fmt.table({
            headers = {"Command", "Description"},
            rows = rows
        }) .. "\n\n"
    end

    output = output .. holo.fmt.dim("Type 'help <command>' for detailed help.")

    holo.emit.character(ctx.character_id, "help", {
        message = output
    })
    return holo.emit.flush()
end

-- Show detailed help for a specific command
function show_command_help(ctx, command_name)
    local info, err = holomush.get_command_help(command_name)
    if err then
        if err:match("command not found") then
            holo.emit.character(ctx.character_id, "error", {
                message = "Unknown command: " .. command_name .. "\nType 'help' to see available commands."
            })
        else
            holo.emit.character(ctx.character_id, "error", {
                message = "Error getting help: " .. err
            })
        end
        return holo.emit.flush()
    end

    -- Build output
    local output = holo.fmt.header(info.name) .. "\n\n"

    -- Short description
    if info.help and info.help ~= "" then
        output = output .. info.help .. "\n\n"
    end

    -- Usage
    if info.usage and info.usage ~= "" then
        output = output .. holo.fmt.bold("Usage: ") .. info.usage .. "\n\n"
    end

    -- Detailed help text
    if info.help_text and info.help_text ~= "" then
        output = output .. info.help_text .. "\n"
    end

    -- Source
    if info.source and info.source ~= "" then
        output = output .. holo.fmt.dim("Source: " .. info.source)
    end

    holo.emit.character(ctx.character_id, "help", {
        message = output
    })
    return holo.emit.flush()
end

-- Search commands by keyword (filtered by character capabilities)
function search_commands(ctx, term)
    local commands, err = holomush.list_commands(ctx.character_id)
    if err then
        holo.emit.character(ctx.character_id, "error", {
            message = "Error searching commands: " .. err
        })
        return holo.emit.flush()
    end

    -- Filter commands matching the search term (searches name, help, and usage fields)
    local matches = {}
    local lower_term = term:lower()
    for _, cmd in ipairs(commands or {}) do
        local name_match = cmd.name and cmd.name:lower():find(lower_term, 1, true)
        local help_match = cmd.help and cmd.help:lower():find(lower_term, 1, true)
        local usage_match = cmd.usage and cmd.usage:lower():find(lower_term, 1, true)
        if name_match or help_match or usage_match then
            table.insert(matches, cmd)
        end
    end

    if #matches == 0 then
        holo.emit.character(ctx.character_id, "info", {
            message = "No commands found matching '" .. term .. "'."
        })
        return holo.emit.flush()
    end

    -- Build output
    local output = holo.fmt.header("Search Results for '" .. term .. "'") .. "\n\n"

    local rows = {}
    for _, cmd in ipairs(matches) do
        table.insert(rows, {cmd.name, cmd.help or ""})
    end

    output = output .. holo.fmt.table({
        headers = {"Command", "Description"},
        rows = rows
    }) .. "\n\n"

    output = output .. holo.fmt.dim("Found " .. #matches .. " command(s).")

    holo.emit.character(ctx.character_id, "help", {
        message = output
    })
    return holo.emit.flush()
end

-- Utility: trim whitespace
function trim(s)
    if not s then return "" end
    return s:match("^%s*(.-)%s*$") or ""
end

-- Utility: capitalize first letter
function capitalize(s)
    if not s or s == "" then return s end
    return s:sub(1, 1):upper() .. s:sub(2)
end
