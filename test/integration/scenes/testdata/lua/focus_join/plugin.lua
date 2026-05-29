-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- test-focus-join: registers a command that auto-focuses the issuing
-- character's connections onto a scene, exercising the real gopher-lua VM
-- path into holomush.auto_focus_on_join (focus runtime-symmetry test).
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
-- holomush.auto_focus_on_join(char_id_str, scene_id_str):
--   Both arguments are ULID strings.
--   Returns a result table on success:
--     { focused_connection_ids, skipped_connection_ids,
--       failed_connection_ids, total_connection_count }
--   Returns (nil, error_string) on failure.
--   Registered at stdlib_focus.go:70 via ls.SetField(mod, "auto_focus_on_join", ...).

function on_command(ctx)
    local char_id, scene_id = ctx.args:match("(%S+)%s+(%S+)")
    if not char_id or not scene_id then
        return {status = 1, output = "usage: luafocusjoin <character_id> <scene_id>"}
    end

    local result, err = holomush.auto_focus_on_join(char_id, scene_id)
    if err then
        return {status = 2, output = "auto_focus_on_join failed: " .. err}
    end

    local focused_count = 0
    if result and result.focused_connection_ids then
        focused_count = #result.focused_connection_ids
    end

    return "focused:" .. tostring(focused_count)
end
