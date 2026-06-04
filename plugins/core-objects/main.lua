-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- core-objects: provides describe, examine, create, and set commands.

-- INV-PLUGIN-32: register the 5 event types this plugin can emit.
-- These MUST match plugin.yaml's crypto.emits block exactly.
holomush.register_emit_type("object_create")
holomush.register_emit_type("object_destroy")
holomush.register_emit_type("object_use")
holomush.register_emit_type("object_examine")
holomush.register_emit_type("object_give")

-- trim removes leading and trailing whitespace.
local function trim(s)
    return s:match("^%s*(.-)%s*$")
end

-- lower converts a string to lowercase.
local function lower(s)
    return s:lower()
end

-- has_prefix returns true if s starts with prefix.
local function has_prefix(s, prefix)
    return s:sub(1, #prefix) == prefix
end

-- ---------------------------------------------------------------------------
-- resolve_target maps a target keyword to {entity_type, entity_id}.
-- Returns entity_type, entity_id or nil, nil.
-- ---------------------------------------------------------------------------
local function resolve_target(ctx, target)
    if target == "here" then
        return "location", ctx.location_id
    elseif target == "me" then
        return "character", ctx.character_id
    elseif has_prefix(target, "#") then
        local id = trim(target:sub(2))
        if id == "" then
            return nil, nil
        end
        return "object", id
    end
    return nil, nil
end

-- ---------------------------------------------------------------------------
-- describe command
-- Syntax:
--   describe me <text>        -- set own character description
--   describe here <text>      -- set current location description
--   describe <target>=<text>  -- set named target description
-- ---------------------------------------------------------------------------
local DESCRIBE_USAGE = "Usage: describe me <text> | describe here <text> | describe <target>=<text>"

local function parse_describe_args(args)
    if has_prefix(args, "me ") then
        local text = trim(args:sub(4))
        if text == "" then
            return nil, nil, "usage: describe me <text>"
        end
        return "me", text, nil
    end

    if has_prefix(args, "here ") then
        local text = trim(args:sub(6))
        if text == "" then
            return nil, nil, "usage: describe here <text>"
        end
        return "here", text, nil
    end

    local eq = args:find("=", 1, true)
    if eq and eq > 1 then
        local tgt = trim(args:sub(1, eq - 1))
        local txt = trim(args:sub(eq + 1))
        if tgt ~= "" and txt ~= "" then
            return tgt, txt, nil
        end
        return nil, nil, "usage: describe <target>=<text>"
    end

    return nil, nil, DESCRIBE_USAGE
end

local function handle_describe(ctx)
    local args = trim(ctx.args or "")
    if args == "" then
        return {status = 1, output = DESCRIBE_USAGE}
    end

    local target, text, parse_err = parse_describe_args(args)
    if not target then
        return {status = 1, output = parse_err}
    end

    if target == "me" then
        -- Use property capability if available, otherwise base hostfunc.
        if property then
            local _, err = property.update_character_description(ctx.character_id, ctx.character_id, text)
            if err then
                holomush.log("error", "describe: failed to update character description: " .. err)
                return {status = 2, output = "Unable to set description right now. Please try again."}
            end
        else
            -- Fallback: set description property via base hostfunc.
            local _, err = holomush.set_property("character", ctx.character_id, "description", text)
            if err then
                holomush.log("error", "describe: failed to set character description: " .. err)
                return {status = 2, output = "Unable to set description right now. Please try again."}
            end
        end
        return "Description set.\n"
    end

    -- For here or #id targets, look up the description property and set it.
    local props, find_err
    if property then
        props, find_err = property.find_by_prefix("description")
    end

    local prop_name = "description"
    if find_err then
        holomush.log("error", "describe: failed to find property: " .. find_err)
        return {status = 2, output = "Unable to set description right now. Please try again."}
    end
    if props then
        if #props == 0 then
            return {status = 1, output = "Unknown property: description"}
        end
        prop_name = props[1].name
    end

    local entity_type, entity_id = resolve_target(ctx, target)
    if not entity_type then
        return {status = 1, output = "Could not find target: " .. target}
    end

    local _, set_err = holomush.set_property(entity_type, entity_id, prop_name, text)
    if set_err then
        holomush.log("error", "describe: failed to set property: " .. set_err)
        return {status = 2, output = "Unable to set description right now. Please try again."}
    end

    return "Description set.\n"
end

-- ---------------------------------------------------------------------------
-- examine command
-- Syntax:
--   examine        -- examine current location
--   examine here   -- examine current location
--   examine <name> -- examine named target in current location
-- ---------------------------------------------------------------------------

local function write_properties(parts, props)
    if not props or #props == 0 then
        return
    end

    -- Filter to public visibility only.
    local visible = {}
    for _, p in ipairs(props) do
        if p.visibility == "public" then
            visible[#visible + 1] = p
        end
    end

    if #visible == 0 then
        return
    end

    -- Sort by name.
    table.sort(visible, function(a, b) return a.name < b.name end)

    parts[#parts + 1] = "\nProperties:\n"
    for _, p in ipairs(visible) do
        parts[#parts + 1] = "  " .. p.name .. ": " .. p.value .. "\n"
    end
end

local function examine_location(ctx)
    local loc, err = holomush.query_location(ctx.location_id)
    if err then
        holomush.log("error", "examine: failed to query location " .. ctx.location_id .. ": " .. err)
        return {status = 2, output = "Unable to examine this location right now. Please try again."}
    end

    local props
    if property then
        props = property.list_by_parent(ctx.character_id, "location", ctx.location_id)
    end

    local parts = {}
    parts[#parts + 1] = "=== " .. loc.name .. " ===\n"
    parts[#parts + 1] = "Name: " .. loc.name .. "\n"
    if loc.description and loc.description ~= "" then
        parts[#parts + 1] = "Description:\n  " .. loc.description .. "\n"
    end
    write_properties(parts, props)

    return table.concat(parts)
end

local function examine_character_by_result(ctx, c)
    local props
    if property then
        props = property.list_by_parent(ctx.character_id, "character", c.id)
    end

    local parts = {}
    parts[#parts + 1] = "=== " .. c.name .. " ===\n"
    parts[#parts + 1] = "Name: " .. c.name .. "\n"
    if c.description and c.description ~= "" then
        parts[#parts + 1] = "Description:\n  " .. c.description .. "\n"
    end
    write_properties(parts, props)

    return table.concat(parts)
end

local function examine_object_by_result(ctx, o)
    local props
    if property then
        props = property.list_by_parent(ctx.character_id, "object", o.id)
    end

    local parts = {}
    parts[#parts + 1] = "=== " .. o.name .. " ===\n"
    parts[#parts + 1] = "Name: " .. o.name .. "\n"
    if o.description and o.description ~= "" then
        parts[#parts + 1] = "Description:\n  " .. o.description .. "\n"
    end
    write_properties(parts, props)

    return table.concat(parts)
end

local function disambiguate(matches)
    local parts = {"Which one? I see multiple matches:\n"}
    for _, m in ipairs(matches) do
        parts[#parts + 1] = "  " .. m.name .. " (" .. m.kind .. ")\n"
    end
    return {status = 1, output = table.concat(parts)}
end

local function examine_target(ctx, name)
    local chars, chars_err
    local objs, objs_err

    if world_ext then
        chars, chars_err = world_ext.get_characters_by_location(ctx.character_id, ctx.location_id)
        objs, objs_err = world_ext.get_objects_by_location(ctx.character_id, ctx.location_id)
    else
        -- Fallback: use base query_location_characters and empty objects.
        chars = holomush.query_location_characters(ctx.location_id)
        objs = {}
    end

    if chars_err then
        holomush.log("error", "examine: failed to query characters at " .. ctx.location_id .. ": " .. chars_err)
        return {status = 2, output = "Unable to look around right now. Please try again."}
    end
    if objs_err then
        holomush.log("error", "examine: failed to query objects at " .. ctx.location_id .. ": " .. objs_err)
        return {status = 2, output = "Unable to look around right now. Please try again."}
    end

    chars = chars or {}
    objs = objs or {}

    local lower_name = lower(name)

    -- Exact match pass.
    local exact = {}
    for _, c in ipairs(chars) do
        if lower(c.name) == lower_name then
            exact[#exact + 1] = {name = c.name, kind = "character", ref = c}
        end
    end
    for _, o in ipairs(objs) do
        if lower(o.name) == lower_name then
            exact[#exact + 1] = {name = o.name, kind = "object", ref = o}
        end
    end

    if #exact == 1 then
        if exact[1].kind == "character" then
            return examine_character_by_result(ctx, exact[1].ref)
        end
        return examine_object_by_result(ctx, exact[1].ref)
    end
    if #exact > 1 then
        return disambiguate(exact)
    end

    -- Prefix match pass.
    local prefix = {}
    for _, c in ipairs(chars) do
        if has_prefix(lower(c.name), lower_name) then
            prefix[#prefix + 1] = {name = c.name, kind = "character", ref = c}
        end
    end
    for _, o in ipairs(objs) do
        if has_prefix(lower(o.name), lower_name) then
            prefix[#prefix + 1] = {name = o.name, kind = "object", ref = o}
        end
    end

    if #prefix == 1 then
        if prefix[1].kind == "character" then
            return examine_character_by_result(ctx, prefix[1].ref)
        end
        return examine_object_by_result(ctx, prefix[1].ref)
    end
    if #prefix > 1 then
        return disambiguate(prefix)
    end

    return {status = 1, output = 'I don\'t see "' .. name .. '" here.'}
end

local function handle_examine(ctx)
    local args = trim(ctx.args or "")

    if args == "" or lower(args) == "here" then
        return examine_location(ctx)
    end
    return examine_target(ctx, args)
end

-- ---------------------------------------------------------------------------
-- create command
-- Syntax: create <type> "<name>"
-- Types: object, location
-- ---------------------------------------------------------------------------
local CREATE_USAGE = 'Usage: create <type> "<name>"'

local function parse_create_args(args)
    -- Match: <word> "<quoted string>"
    local entity_type, name = args:match('^(%w+)%s+"([^"]+)"%s*$')
    if not entity_type then
        return nil, nil
    end
    return entity_type:lower(), name
end

local function handle_create(ctx)
    local args = trim(ctx.args or "")
    if args == "" then
        return {status = 1, output = CREATE_USAGE}
    end

    local entity_type, name = parse_create_args(args)
    if not entity_type then
        return {status = 1, output = CREATE_USAGE}
    end

    if entity_type == "object" then
        local result, err = holomush.create_object(name, {location_id = ctx.location_id})
        if err then
            holomush.log("error", 'create: failed to create object "' .. name .. '": ' .. err)
            return {status = 2, output = "Unable to create object right now. Please try again."}
        end
        return 'Created object "' .. name .. '" (#' .. result.id .. ')\n'

    elseif entity_type == "location" then
        local result, err = holomush.create_location(name, "", "persistent")
        if err then
            holomush.log("error", 'create: failed to create location "' .. name .. '": ' .. err)
            return {status = 2, output = "Unable to create location right now. Please try again."}
        end
        return 'Created location "' .. name .. '" (#' .. result.id .. ')\n'

    else
        return {status = 1, output = CREATE_USAGE .. " (valid types: object, location)"}
    end
end

-- ---------------------------------------------------------------------------
-- set command
-- Syntax: set <property> of <target> to <value>
-- ---------------------------------------------------------------------------
local SET_USAGE = "Usage: set <property> of <target> to <value>"

local function parse_set_args(args)
    -- Match: <word> of <non-space> to <rest>
    local prop, target, value = args:match("^(%w+)%s+of%s+(%S+)%s+to%s+(.+)$")
    if not prop then
        return nil, nil, nil
    end
    return prop, target, trim(value)
end

local function handle_set(ctx)
    local args = trim(ctx.args or "")
    if args == "" then
        return {status = 1, output = SET_USAGE}
    end

    local prop_prefix, target, value = parse_set_args(args)
    if not prop_prefix then
        return {status = 1, output = SET_USAGE}
    end

    -- Find property by prefix.
    local props, find_err
    if property then
        props, find_err = property.find_by_prefix(prop_prefix)
    end

    if find_err then
        holomush.log("error", 'set: failed to find property "' .. prop_prefix .. '": ' .. find_err)
        return {status = 2, output = "Unable to set property right now. Please try again."}
    end

    if not props or #props == 0 then
        return {status = 1, output = "Unknown property: " .. prop_prefix}
    end

    if #props > 1 then
        local names = {}
        for _, p in ipairs(props) do
            names[#names + 1] = p.name
        end
        return {status = 1, output = 'Ambiguous property "' .. prop_prefix .. '"; matches: ' .. table.concat(names, ", ")}
    end

    local prop_name = props[1].name

    local entity_type, entity_id = resolve_target(ctx, target)
    if not entity_type then
        return {status = 1, output = "Could not find target: " .. target}
    end

    local _, set_err = holomush.set_property(entity_type, entity_id, prop_name, value)
    if set_err then
        holomush.log("error", 'set: failed to set ' .. prop_name .. ' on ' .. target .. ': ' .. set_err)
        return {status = 2, output = "Unable to set " .. prop_name .. " right now. Please try again."}
    end

    return "Set " .. prop_name .. " of " .. target .. ".\n"
end

-- ---------------------------------------------------------------------------
-- Command dispatcher
-- ---------------------------------------------------------------------------
function on_command(ctx)
    local cmd = ctx.command
    if cmd == "describe" then
        return handle_describe(ctx)
    elseif cmd == "examine" then
        return handle_examine(ctx)
    elseif cmd == "create" then
        return handle_create(ctx)
    elseif cmd == "set" then
        return handle_set(ctx)
    else
        return {status = 1, output = "Unknown command: " .. (cmd or "")}
    end
end
