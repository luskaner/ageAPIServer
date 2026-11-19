#!/bin/sh
set -e

# AGEAPISERVER_* carries the configuration; SERVER_ARGS appends flags
# (for example: SERVER_ARGS="--log.level=debug --matchcontent.ratelimitperminute=60").
# The expansion is intentionally unquoted so it splits into argv.
# shellcheck disable=SC2086
exec /app/ageAPIServer $SERVER_ARGS
