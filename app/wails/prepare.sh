#!/bin/sh
# Исправленная копия Wails (форк netbirdio из go.mod) для сборки приложения:
#   build-out/wails-v3 = исходник модуля + app/wails/patches/*.patch,
#   build-out/go.work  = этот репозиторий + replace на исправленную копию.
# Печатает путь к go.work — сборки и тесты берут его так: GOWORK=$(sh app/wails/prepare.sh).
# Идемпотентно: при тех же патчах и той же версии модуля копия не пересобирается.
set -eu
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
OUT=$ROOT/build-out
DEST=$OUT/wails-v3
WORK=$OUT/go.work
cd "$ROOT"
export GOWORK=off
REPL=$(go list -m -f '{{with .Replace}}{{.Path}}@{{.Version}}{{end}}' github.com/wailsapp/wails/v3)
[ -n "$REPL" ] || { echo "в go.mod нет replace для github.com/wailsapp/wails/v3" >&2; exit 1; }
go mod download "$REPL"
SRC=$(go list -m -f '{{with .Replace}}{{.Dir}}{{end}}' github.com/wailsapp/wails/v3)
[ -d "$SRC" ] || { echo "не скачался $REPL" >&2; exit 1; }
sum() { if command -v sha256sum >/dev/null 2>&1; then sha256sum; else shasum -a 256; fi; }
STAMP=$( (echo "$REPL"; cat "$ROOT"/app/wails/patches/*.patch) | sum | cut -d' ' -f1)
if [ ! -f "$DEST/.jetbox-stamp" ] || [ "$(cat "$DEST/.jetbox-stamp")" != "$STAMP" ]; then
  rm -rf "$DEST"
  mkdir -p "$DEST"
  # examples не нужны сборке и весят больше половины модуля
  (cd "$SRC" && tar --exclude=./examples -cf - .) | (cd "$DEST" && tar -xf -)
  chmod -R u+w "$DEST"
  # Свой временный git у копии: иначе git apply внутри build-out считал бы пути от корня этого
  # репозитория и молча пропускал файлы.
  git -C "$DEST" init -q
  for p in "$ROOT"/app/wails/patches/*.patch; do
    git -C "$DEST" apply -p1 --whitespace=nowarn "$p" || { echo "патч Wails не лёг: $(basename "$p")" >&2; exit 1; }
  done
  rm -rf "$DEST/.git"
  echo "$STAMP" > "$DEST/.jetbox-stamp"
fi
GOVER=$(sed -n 's/^go \([0-9.]*\)$/\1/p' "$ROOT/go.mod")
printf 'go %s\n\nuse ..\n\nreplace github.com/wailsapp/wails/v3 => ./wails-v3\n' "$GOVER" > "$WORK"
# Git Bash на Windows (CI): родному go — путь вида C:/…
if command -v cygpath >/dev/null 2>&1; then cygpath -m "$WORK"; else echo "$WORK"; fi
