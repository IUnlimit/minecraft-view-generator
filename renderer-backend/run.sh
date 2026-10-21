#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
JAR="$ROOT/build/minecraft-renderer.jar"
# Rebuild when the jar is missing or older than the Java sources, so the renderer never runs a class
# file from before the last edit.
needs_build() {
	if [ ! -f "$JAR" ]; then
		return 0
	fi
	[ -n "$(find "$ROOT/src" "$ROOT/build.sh" -newer "$JAR" -print -quit 2>/dev/null)" ]
}
if [ "${BUILD:-0}" = "1" ] || needs_build; then
  "$ROOT/build.sh" >/dev/null
fi
exec java -Djava.awt.headless=true -jar "$JAR"
