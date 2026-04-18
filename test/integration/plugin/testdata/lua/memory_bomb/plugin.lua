-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Deeply-recursive function with many local variables per frame.
-- Each recursive call pushes 8 locals onto the fixed-size Lua value
-- stack (registry); recursion depth x locals-per-frame exceeds
-- RegistrySize, triggering gopher-lua's "registry overflow" panic
-- which CallByParam(Protect=true) converts to an error.
--
-- Note: a simple `local t={}; for i=1,N do t[i]={...} end` bomb does
-- NOT trigger RegistryMaxSize in gopher-lua v1.1.1, because the
-- library silently zeroes RegistryMaxSize when it is smaller than
-- RegistrySize (default 5120). Table growth happens on the heap, not
-- the registry, so a table bomb exhausts Go heap rather than the Lua
-- stack.
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
