#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
BINARY="$ROOT/dist/minecraft-view-generator2"

# Rebuild when the binary is missing or older than the sources. Without the age check this script
# happily served the code from before the last edit, which looks like a rendering bug rather than a
# stale build.
needs_build() {
	if [ ! -x "$BINARY" ]; then
		return 0
	fi
	[ -n "$(find "$ROOT/cmd" "$ROOT/internal" "$ROOT/go.mod" -newer "$BINARY" -print -quit 2>/dev/null)" ]
}

if [ "${BUILD:-0}" = "1" ] || needs_build; then
	make -C "$ROOT" build
fi

: "${LISTEN_ADDR:=0.0.0.0:4666}"
: "${DATA_DIR:=$ROOT/data}"
: "${RENDERER_COMMAND:=$ROOT/renderer-backend/run.sh}"
export LISTEN_ADDR DATA_DIR RENDERER_COMMAND

exec "$BINARY" "$@"
