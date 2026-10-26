#!/bin/sh
set -eu
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres -v password="$APP_DB_PASSWORD" <<'SQL'
CREATE ROLE pcshop LOGIN PASSWORD :'password';
CREATE DATABASE pcshop OWNER pcshop;
SQL
