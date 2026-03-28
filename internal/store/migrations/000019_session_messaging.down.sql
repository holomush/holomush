ALTER TABLE sessions
  DROP COLUMN IF EXISTS last_paged,
  DROP COLUMN IF EXISTS last_whispered;
