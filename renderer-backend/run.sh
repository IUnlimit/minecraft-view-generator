#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
JAR="$ROOT/build/minecraft-renderer.jar"
if [ ! -f "$JAR" ]; then
  "$ROOT/build.sh" >/dev/null
fi
exec java -Djava.awt.headless=true -jar "$JAR"
