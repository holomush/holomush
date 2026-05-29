-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- core-help: provides the help command for listing and describing commands.

-- capitalize upper-cases the first rune of a string.
local function capitalize(s)
    if not s or s == "" then return s end
    return s:sub(1, 1):upper() .. s:sub(2)
end

-- trim removes leading and trailing whitespace.
local function trim(s)
    return s:match("^%s*(.-)%s*$")
end

-- table_sort_by_key sorts an array table in place by a string field.
local function table_sort_by_key(t, key)
    table.sort(t, function(a, b) return a[key] < b[key] end)
end

-- list_all_commands handles "help" with no arguments.
--
-- holomush.list_commands has a two-tier failure contract (see
-- internal/plugin/hostfunc/commands.go): a HARD failure (command registry or
-- access engine unavailable) returns a nil result plus an error; a SOFT failure
-- returns a fully-populated command list with result.incomplete=true plus an
-- error, because an ABAC engine error hid some capability-gated commands.
-- No-capability commands (help itself included) are always present, so the soft
-- list is usable. We therefore branch on result, not on err: only a nil result
-- warrants the blanket "unavailable" message; an incomplete list is rendered
-- with a warning so the user still gets the commands they can run.
local function list_all_commands(ctx)
    local result, err = holomush.list_commands(ctx.character_id)

    if result == nil then
        if err then
            holomush.log("error", "help: failed to list commands for " .. ctx.character_id .. ": " .. err)
        end
        return {status = 2, output = "Help is temporarily unavailable. Please try again later."}
    end

    local incomplete = err ~= nil or result.incomplete == true
    if incomplete then
        holomush.log("warn", "help: command list incomplete for " .. ctx.character_id ..
            (err and (": " .. err) or ""))
    end

    if #result.commands == 0 then
        return "No commands available."
    end

    -- Group by source.
    local by_source = {}
    for _, cmd in ipairs(result.commands) do
        local src = cmd.source
        if not src or src == "" then src = "other" end
        if not by_source[src] then
            by_source[src] = {}
        end
        table.insert(by_source[src], cmd)
    end

    -- Collect and sort source names for stable output.
    local sources = {}
    for src in pairs(by_source) do
        table.insert(sources, src)
    end
    table.sort(sources)

    -- Build output.
    local out = holo.fmt.header("Available Commands") .. "\n\n"

    for _, src in ipairs(sources) do
        local cmds = by_source[src]
        table_sort_by_key(cmds, "name")
        out = out .. holo.fmt.bold(capitalize(src)) .. "\n"

        local rows = {}
        for _, cmd in ipairs(cmds) do
            table.insert(rows, {cmd.name, cmd.help or ""})
        end
        out = out .. holo.fmt.table({headers = {"Command", "Description"}, rows = rows}) .. "\n\n"
    end

    out = out .. holo.fmt.dim("Type 'help <command>' for detailed help.")
    if incomplete then
        out = out .. "\n" .. holo.fmt.dim(
            "⚠ This command list may be incomplete due to a temporary system error. Try 'help' again shortly.")
    end
    return out
end

-- show_command_help handles "help <command>".
local function show_command_help(ctx, name)
    local info, err = holomush.get_command_help(name, ctx.character_id)
    if err then
        -- Distinguish "not found" from real errors.
        -- get_command_help returns ("command not found: <name>", err) when missing.
        if err:find("command not found") or err:find("access denied") then
            return {status = 1, output = "Unknown command: " .. name .. "\nType 'help' to see available commands."}
        end
        holomush.log("error", "help: failed to get help for " .. name .. ": " .. err)
        return {status = 2, output = "Help is temporarily unavailable. Please try again later."}
    end
    if info == nil then
        return {status = 1, output = "Unknown command: " .. name .. "\nType 'help' to see available commands."}
    end

    local out = holo.fmt.header(info.name) .. "\n\n"

    if info.help and info.help ~= "" then
        out = out .. info.help .. "\n\n"
    end

    if info.usage and info.usage ~= "" then
        out = out .. holo.fmt.bold("Usage: ") .. info.usage .. "\n\n"
    end

    if info.help_text and info.help_text ~= "" then
        out = out .. info.help_text .. "\n"
    end

    if info.source and info.source ~= "" then
        out = out .. holo.fmt.dim("Source: " .. info.source)
    end

    return out
end

-- search_commands handles "help search <term>".
--
-- Shares list_commands' two-tier failure contract with list_all_commands: a nil
-- result is a hard failure (blanket message), while a populated result with a
-- non-nil err / incomplete=true is a soft failure whose usable subset we still
-- search and render, appending an incompleteness indicator.
local function search_commands(ctx, term)
    local result, err = holomush.list_commands(ctx.character_id)

    if result == nil then
        if err then
            holomush.log("error", "help: failed to search commands for " .. term .. ": " .. err)
        end
        return {status = 2, output = "Search is temporarily unavailable. Please try again later."}
    end

    local incomplete = err ~= nil or result.incomplete == true
    if incomplete then
        holomush.log("warn", "help: search command list incomplete for " .. term ..
            (err and (": " .. err) or ""))
    end

    local lower_term = term:lower()
    local matches = {}
    for _, cmd in ipairs(result.commands) do
        local name_lower = (cmd.name or ""):lower()
        local help_lower = (cmd.help or ""):lower()
        if name_lower:find(lower_term, 1, true) or help_lower:find(lower_term, 1, true) then
            table.insert(matches, cmd)
        end
    end

    table_sort_by_key(matches, "name")

    if #matches == 0 then
        return {status = 1, output = "No commands found matching '" .. term .. "'."}
    end

    local out = holo.fmt.header("Search Results for '" .. term .. "'") .. "\n\n"

    local rows = {}
    for _, cmd in ipairs(matches) do
        table.insert(rows, {cmd.name, cmd.help or ""})
    end
    out = out .. holo.fmt.table({headers = {"Command", "Description"}, rows = rows}) .. "\n\n"
    out = out .. holo.fmt.dim("Found " .. #matches .. " command(s).")
    if incomplete then
        out = out .. "\n" .. holo.fmt.dim(
            "⚠ Searchable commands may be incomplete due to a temporary system error. Try again shortly.")
    end

    return out
end

function on_command(ctx)
    local args = trim(ctx.args or "")

    if args == "" then
        return list_all_commands(ctx)
    end

    -- Check for "search <term>" prefix (case-insensitive).
    local search_term = args:match("^[Ss][Ee][Aa][Rr][Cc][Hh]%s+(.+)$")
    if search_term then
        search_term = trim(search_term)
        if search_term ~= "" then
            return search_commands(ctx, search_term)
        end
    end

    return show_command_help(ctx, args)
end
