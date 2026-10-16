#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
OUT="$ROOT/build/test-classes"
rm -rf "$OUT"
mkdir -p "$OUT"
javac --release 21 -encoding UTF-8 -cp "$ROOT/build/classes" -d "$OUT" "$ROOT"/src/test/java/com/illtamer/minecraft/*.java
java -cp "$OUT:$ROOT/build/classes:$ROOT/build/minecraft-renderer.jar" com.illtamer.minecraft.RendererSmokeTest "$ROOT/fixture"
printf '%s\n' "renderer tests passed"
