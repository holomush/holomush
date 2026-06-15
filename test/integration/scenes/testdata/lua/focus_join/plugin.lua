-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- test-focus-join: registers a command that auto-focuses the issuing
-- character's connections onto a scene, exercising the real gopher-lua VM
-- path into the brokered focus capability (focus.AutoFocusOnJoin) — the
-- Lua-runtime half of the focus runtime-symmetry test (INV-SCENE-40).
--
-- Command form: luafocusjoin <character_id_ulid> <scene_id_ulid>
--
-- on_command(ctx) receives a Lua table with fields:
--   ctx.command      (string) command name
--   ctx.args         (string) raw argument string
--   ctx.character_id (string) ULID of the issuing character
--   ctx.location_id  (string) ULID of the issuing character's location
--   ctx.session_id   (string) session ULID
--   ctx.player_id    (string) player ULID
--   ctx.connection_id (string) connection ULID (empty for non-connection paths)
--
-- After the atomic capability cutover (holomush-eykuh.4) the legacy
-- `holomush.auto_focus_on_join(char_str, scene_str)` hostfunc is retired; the
-- focus capability flows through the host-brokered surface as the `focus`
-- global (luabridge.RegisterHostCaps -> registerFocusService), gated on the
-- `- capability: focus` requires entry in plugin.yaml.
--
-- focus.AutoFocusOnJoin({character_id = <16-byte ULID>, scene_id = <16-byte
-- ULID>}):
--   The request fields are proto `bytes` carrying the 16-byte BINARY ULID form
--   (not the 26-char Crockford string) — focusServer.bytesToULID rejects any
--   other length. The marshal passes a Lua string verbatim as the byte field,
--   so the caller MUST decode the ULID string to its 16 raw bytes first.
--   Returns a result table on success:
--     { focused_connection_ids, skipped_connection_ids,
--       failed_connection_ids, total_connection_count }
--   Each *_connection_ids entry is itself a 16-byte ULID string. The fixture
--   only counts focused_connection_ids, so it does not re-encode them.
--   Returns (nil, error_string) on failure.

-- crockford maps each base32 character byte to its 0..31 value (Crockford
-- alphabet, with I/L -> 1 and O -> 0 case-insensitive aliases).
local crockford = {}
do
    local alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
    for i = 1, #alphabet do
        crockford[alphabet:byte(i)] = i - 1
    end
    crockford[("I"):byte()] = 1
    crockford[("L"):byte()] = 1
    crockford[("O"):byte()] = 0
end

-- ulid_to_bytes decodes a 26-char Crockford-base32 ULID string into its 16-byte
-- binary form (the proto `bytes` representation the brokered focus service
-- expects). Returns nil, err on malformed input.
local function ulid_to_bytes(s)
    if type(s) ~= "string" or #s ~= 26 then
        return nil, "ulid must be a 26-character string"
    end
    s = s:upper()
    -- A ULID is 128 bits encoded as 26 base32 chars. Accumulate into a
    -- big-endian 16-byte value: shift left 5 bits per char, OR in the digit.
    local bytes = {}
    for i = 1, 16 do
        bytes[i] = 0
    end
    for i = 1, 26 do
        local v = crockford[s:byte(i)]
        if v == nil then
            return nil, "invalid ulid character at position " .. i
        end
        -- A valid ULID is 128 bits in 26*5=130 base32 bits, so the first digit
        -- carries only 3 significant bits (must be <= 7); a larger leading digit
        -- — or any carry out of the 16-byte accumulator — is out of ULID range.
        if i == 1 and v > 7 then
            return nil, "ulid out of range"
        end
        local carry = v
        for j = 16, 1, -1 do
            local acc = bytes[j] * 32 + carry
            bytes[j] = acc % 256
            carry = math.floor(acc / 256)
        end
        if carry ~= 0 then
            return nil, "ulid out of range"
        end
    end
    local out = {}
    for i = 1, 16 do
        out[i] = string.char(bytes[i])
    end
    return table.concat(out)
end

function on_command(ctx)
    -- The command keeps its 2-arg form (luafocusjoin <character_id> <scene_id>)
    -- for caller compatibility, but the args-supplied character id is IGNORED:
    -- we always focus the ISSUING character (ctx.character_id), never an
    -- arbitrary one. This enforces own-character-only focus — a character cannot
    -- focus another character's connections through this fixture command.
    local _, scene_id = ctx.args:match("(%S+)%s+(%S+)")
    if not scene_id then
        return {status = 1, output = "usage: luafocusjoin <character_id> <scene_id>"}
    end

    -- The brokered focus capability must be wired (the `- capability: focus`
    -- requires entry grants and injects the `focus` global). Without it the
    -- plugin degrades rather than nil-indexing.
    if not focus then
        return {status = 2, output = "focus capability not available"}
    end

    local char_bytes, char_err = ulid_to_bytes(ctx.character_id)
    if not char_bytes then
        return {status = 1, output = "invalid character id: " .. char_err}
    end
    local scene_bytes, scene_err = ulid_to_bytes(scene_id)
    if not scene_bytes then
        return {status = 1, output = "invalid scene id: " .. scene_err}
    end

    local result, err = focus.AutoFocusOnJoin({
        character_id = char_bytes,
        scene_id = scene_bytes,
    })
    if err then
        return {status = 2, output = "auto_focus_on_join failed: " .. err}
    end

    local focused_count = 0
    if result and result.focused_connection_ids then
        focused_count = #result.focused_connection_ids
    end

    return "focused:" .. tostring(focused_count)
end
