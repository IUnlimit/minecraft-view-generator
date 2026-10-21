#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
OUT="$ROOT/build"
rm -rf "$OUT/classes"
mkdir -p "$OUT/classes"
javac --release 21 -encoding UTF-8 -d "$OUT/classes" "$ROOT"/src/main/java/com/illtamer/minecraft/*.java
jar --create --file "$OUT/minecraft-renderer.jar" --main-class com.illtamer.minecraft.Main -C "$OUT/classes" .
printf '%s\n' "$OUT/minecraft-renderer.jar"
