#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
BINARY="$ROOT/dist/minecraft-view-generator2"

if [ "${BUILD:-0}" = "1" ] || [ ! -x "$BINARY" ]; then
	make -C "$ROOT" build
fi

: "${LISTEN_ADDR:=0.0.0.0:4666}"
: "${DATA_DIR:=$ROOT/data}"
: "${RENDERER_COMMAND:=$ROOT/renderer-backend/run.sh}"
export LISTEN_ADDR DATA_DIR RENDERER_COMMAND

exec "$BINARY" "$@"
