#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if [ -f "$root/.env.test" ]; then
	set -a
	. "$root/.env.test"
	set +a
fi

: "${TEST_DATABASE_URL:=${DATABASE_URL:-}}"
if [ -z "$TEST_DATABASE_URL" ]; then
	echo "Set TEST_DATABASE_URL or DATABASE_URL in .env.test" >&2
	exit 2
fi
export TEST_DATABASE_URL
cd "$root"
exec go test -race ./...
