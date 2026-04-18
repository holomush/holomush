-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Deeply-recursive function with many local variables to stress the
-- Lua value stack (registry). Reliably triggers RegistryMaxSize
-- overflow; a simple table-allocation loop does NOT (gopher-lua
-- normalizes RegistryMaxSize < RegistrySize to zero).
function bomb(depth)
    local a, b, c, d, e, f, g, h = 1, 2, 3, 4, 5, 6, 7, 8
    if depth > 0 then
        bomb(depth - 1)
    end
    return a + b + c + d + e + f + g + h
end

function on_event(event)
    bomb(100000)
end
