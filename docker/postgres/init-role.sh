#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Creates the holomush application role with CREATEROLE privilege.
# Runs as the postgres superuser during container first start only.
# Re-runs require: docker compose down -v

set -euo pipefail

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<EOSQL
-- Application role: not superuser, but can create roles for plugin isolation
CREATE ROLE holomush LOGIN PASSWORD 'holomush' CREATEROLE;

-- Transfer database and schema ownership to application role
ALTER DATABASE $POSTGRES_DB OWNER TO holomush;
ALTER SCHEMA public OWNER TO holomush;
EOSQL
