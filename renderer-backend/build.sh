#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
OUT="$ROOT/build"
REGULAR="$ROOT/fonts/MinecraftDefault-Regular.ttf"
UNIFORM="$ROOT/fonts/MinecraftDefaultUniformBMP-Regular.ttf"
[ -f "$REGULAR" ] || { printf '%s\n' "missing font: $REGULAR" >&2; exit 1; }
[ -f "$UNIFORM" ] || { printf '%s\n' "missing font: $UNIFORM" >&2; exit 1; }
[ "$(sha256sum "$REGULAR" | cut -d' ' -f1)" = "7ed7b7fe0c1967ef69b6142833d1855e3882a325a02b90fc0107f6c4a9513894" ] || { printf '%s\n' "invalid MinecraftDefault-Regular.ttf SHA-256" >&2; exit 1; }
[ "$(sha256sum "$UNIFORM" | cut -d' ' -f1)" = "fba3d09d92b7d241803467028cf6ccce0597a00df98bb8433aecf9587fcf8d12" ] || { printf '%s\n' "invalid MinecraftDefaultUniformBMP-Regular.ttf SHA-256" >&2; exit 1; }
rm -rf "$OUT/classes"
mkdir -p "$OUT/classes"
javac --release 21 -encoding UTF-8 -d "$OUT/classes" "$ROOT"/src/main/java/com/illtamer/minecraft/*.java
jar --create --file "$OUT/minecraft-renderer.jar" --main-class com.illtamer.minecraft.Main -C "$OUT/classes" . -C "$ROOT/fonts" MinecraftDefault-Regular.ttf -C "$ROOT/fonts" MinecraftDefaultUniformBMP-Regular.ttf
printf '%s\n' "$OUT/minecraft-renderer.jar"
