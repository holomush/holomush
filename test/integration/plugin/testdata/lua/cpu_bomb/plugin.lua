-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Deliberately tight-loops in on_event to test the CPU deadline.
function on_event(event)
    while true do end
end
