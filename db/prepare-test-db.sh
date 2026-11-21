#!/bin/sh
set -eu

: "${POSTGRES_HOST:?POSTGRES_HOST is required}"
: "${POSTGRES_USER:?POSTGRES_USER is required}"
: "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}"
: "${POSTGRES_DB:?POSTGRES_DB is required}"
: "${POSTGRES_TEST_DB:?POSTGRES_TEST_DB is required}"

if [ "$POSTGRES_TEST_DB" = "$POSTGRES_DB" ]; then
  echo 'POSTGRES_TEST_DB must differ from POSTGRES_DB' >&2
  exit 1
fi

export PGPASSWORD="$POSTGRES_PASSWORD"

echo 'Ensuring test database exists'
psql -X \
  --host="$POSTGRES_HOST" \
  --username="$POSTGRES_USER" \
  --dbname="$POSTGRES_DB" \
  --set=ON_ERROR_STOP=1 \
  --set=test_db="$POSTGRES_TEST_DB" \
  --set=db_owner="$POSTGRES_USER" <<'SQL'
SELECT format('CREATE DATABASE %I OWNER %I', :'test_db', :'db_owner')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'test_db')
\gexec
SQL
