#!/bin/sh
# Готовит дерево исходников службы: daemon/upstream/netbird-<тег> = зип тега + daemon/patches/*.patch.
# Дерево — свой git (коммит «upstream <тег>» + по коммиту на патч), чтобы правки патчей делать
# обычными коммитами и выгружать обратно `scripts/export-patches.sh`.
# Идемпотентно: если дерево уже собрано из тех же патчей — ничего не делает. --force — пересобрать
# (откажется, если в дереве есть невыгруженные коммиты или правки — чтобы не стереть работу).
# DEST=<каталог> — собрать в другое место (test.sh собирает так чистую копию).
set -eu
# sha256: shasum (macOS) или sha256sum (Linux, Git Bash).
sha256_sum() { if command -v sha256sum >/dev/null 2>&1; then sha256sum; else shasum -a 256; fi; }
sha256_check() { if command -v sha256sum >/dev/null 2>&1; then sha256sum -c -; else shasum -a 256 -c -; fi; }
DAEMON=$(cd "$(dirname "$0")/.." && pwd)
. "$DAEMON/upstream.env"
VER=${TAG#v}
CACHE=$DAEMON/upstream
SRC=${DEST:-$CACHE/netbird-$VER}
ZIP=$CACHE/netbird-$TAG.zip
STAMP=$SRC.patches.sha256

mkdir -p "$CACHE"
if [ ! -f "$ZIP" ]; then
  curl -fsSL -o "$ZIP.part" "$ZIP_URL"
  mv "$ZIP.part" "$ZIP"
fi
echo "$ZIP_SHA256  $ZIP" | sha256_check >/dev/null || { echo "sha256 зипа не сходится — стоп"; exit 1; }

PATCH_SUM=$(cat "$DAEMON"/patches/*.patch 2>/dev/null | sha256_sum | cut -d' ' -f1)
if [ "${1:-}" != --force ] && [ -f "$STAMP" ] && [ "$(cat "$STAMP")" = "$PATCH_SUM" ]; then
  echo "дерево $SRC актуально"
  exit 0
fi

if [ -d "$SRC/.git" ] && [ -z "${DEST:-}" ]; then
  if [ -n "$(git -C "$SRC" status --porcelain)" ]; then
    echo "в $SRC незакоммиченные правки — закоммить и выгрузи (export-patches.sh) или убери сам"; exit 1
  fi
  CMP=$(mktemp -d)
  git -C "$SRC" format-patch -q --no-signature --zero-commit --no-numbered -N -o "$CMP" upstream..HEAD
  if ! diff -rq "$CMP" "$DAEMON/patches" >/dev/null; then
    rm -rf "$CMP"
    echo "в $SRC коммиты, которых нет в daemon/patches — сначала export-patches.sh (или удали дерево сам)"; exit 1
  fi
  rm -rf "$CMP"
fi

UNZ=$(mktemp -d "$CACHE/.unzip.XXXXXX")
rm -rf "$SRC"
unzip -q "$ZIP" -d "$UNZ"
mv "$UNZ/github.com/netbirdio/netbird@$TAG" "$SRC"
rm -rf "$UNZ"
chmod -R u+w "$SRC"

cd "$SRC"
git init -q
git config user.name "JetBox build"
git config user.email "build@jetbox.invalid"
git config core.autocrlf false
git add -A
git commit -q -m "upstream $TAG"
git tag upstream
for p in "$DAEMON"/patches/*.patch; do
  [ -f "$p" ] || continue
  if ! git am -q --keep-cr "$p"; then
    echo "патч не лёг: $(basename "$p") — дерево оставлено в состоянии git am:"
    echo "  поправь файлы в $SRC, git add, git am --continue; затем export-patches.sh"
    exit 1
  fi
  echo "  + $(basename "$p")"
done
echo "$PATCH_SUM" > "$STAMP"
echo "готово: $SRC ($(git rev-list --count upstream..HEAD) патч(ей))"
