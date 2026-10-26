#!/bin/sh
# Всё для Mac одной командой: служба → приложение → .pkg (подпись, нотаризация) → тесты.
#   sh packaging/macos/release.sh <версия X.Y.Z> [номер сборки приложения]
# Выход: build-out/macos/JetBox-<версия>.pkg и JetBox.pkg + эталон в releases/ (JetBox-<версия>-b<сборка>.pkg,
# строка в SHA256SUMS). releases/ — в корне основной папки репо (вне git, общий для Mac и Windows);
# другой — JETBOX_RELEASES=<каталог>. Публикация — не здесь.
set -eu
VER=${1:?версия X.Y.Z}
BUILD=${2:-1}
REPO=$(cd "$(dirname "$0")/../.." && pwd)
cd "$REPO"
step() { printf '\n== %s\n' "$*"; }

step "служба (daemon/)"
sh daemon/scripts/build.sh darwin
step "тесты службы"
sh daemon/scripts/test.sh
step "приложение (app/), версия $VER сборка $BUILD"
sh app/build.sh darwin "$VER" "$BUILD"
step "тесты core/ и app/"
go test ./core/... ./app/...
step "установщик"
sh packaging/macos/build-pkg.sh --version "$VER"
step "проверка установщика"
sh packaging/macos/test.sh "build-out/macos/JetBox-$VER.pkg"

step "эталон в releases/"
REL=${JETBOX_RELEASES:-$(cd "$(git rev-parse --git-common-dir)/.." && pwd)/releases}
mkdir -p "$REL"
NAME=JetBox-$VER-b$BUILD.pkg
SUM=$(shasum -a 256 "build-out/macos/JetBox-$VER.pkg" | cut -d' ' -f1)
if [ -f "$REL/$NAME" ]; then
  OLD=$(shasum -a 256 "$REL/$NAME" | cut -d' ' -f1)
  [ "$OLD" = "$SUM" ] || { echo "в $REL уже есть $NAME с другим sha256 — эталон не перезаписываю; подними номер сборки"; exit 1; }
  echo "  $NAME уже в releases/ (тот же sha256)"
else
  cp "build-out/macos/JetBox-$VER.pkg" "$REL/$NAME"
  printf '%s  %s\n' "$SUM" "$NAME" >> "$REL/SHA256SUMS"
  echo "  $REL/$NAME"
fi
echo "  sha256 $SUM"
