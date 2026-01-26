-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Enable pg_stat_statements for query performance monitoring
-- Note: This extension requires shared_preload_libraries='pg_stat_statements'
-- in postgresql.conf. Without it, CREATE EXTENSION will fail.

-- Try to create the extension. In environments where shared_preload_libraries
-- isn't configured (like some test environments), this is a no-op.
DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
EXCEPTION
    WHEN undefined_file THEN
        -- Extension module not available (shared_preload_libraries not configured)
        RAISE NOTICE 'pg_stat_statements not available (shared_preload_libraries not configured)';
    WHEN OTHERS THEN
        -- Log but don't fail - this is optional for development
        RAISE NOTICE 'Could not enable pg_stat_statements: %', SQLERRM;
END
$$;
