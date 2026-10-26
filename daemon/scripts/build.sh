#!/bin/sh
# Собирает службу JetBox (jetboxd) из тега апстрима + daemon/patches под обе ОС.
#   build-out/darwin-amd64/jetboxd, build-out/darwin-arm64/jetboxd,
#   build-out/darwin-universal/jetboxd (lipo), build-out/windows-amd64/jetboxd.exe
# Без cgo (как апстрим). Подпись — не здесь: packaging/macos (Developer ID), Windows — позже.
# Запуск: sh daemon/scripts/build.sh [darwin|windows]   (по умолчанию — обе)
set -eu
. "$(dirname "$0")/lib.sh"
WHAT=${1:-all}

sh "$DAEMON/scripts/prepare.sh"
cd "$SRC"

build() { # goos goarch out
  mkdir -p "$(dirname "$3")"
  GOOS=$1 GOARCH=$2 CGO_ENABLED=0 go build -trimpath -tags load_wgnt_from_rsrc \
    -ldflags "$(ldflags "$1")" -o "$3" ./client
  echo "  $3"
}

if [ "$WHAT" = all ] || [ "$WHAT" = darwin ]; then
  build darwin amd64 "$OUT/darwin-amd64/jetboxd"
  build darwin arm64 "$OUT/darwin-arm64/jetboxd"
  mkdir -p "$OUT/darwin-universal"
  lipo -create -output "$OUT/darwin-universal/jetboxd" "$OUT/darwin-amd64/jetboxd" "$OUT/darwin-arm64/jetboxd"
  echo "  $OUT/darwin-universal/jetboxd ($(lipo -archs "$OUT/darwin-universal/jetboxd"))"
fi

if [ "$WHAT" = all ] || [ "$WHAT" = windows ]; then
  # Ресурсы exe: имя продукта и описание «JetBox» (их показывает Windows и диалоги браузера),
  # манифест апстрима. goversioninfo — локально в daemon/upstream/bin (go install @v1.7.0).
  GVI=$DAEMON/upstream/bin/goversioninfo
  [ -x "$GVI" ] || GOBIN=$DAEMON/upstream/bin go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.7.0
  MAJ=$(echo "${TAG#v}" | cut -d. -f1); MIN=$(echo "${TAG#v}" | cut -d. -f2); PAT=$(echo "${TAG#v}" | cut -d. -f3)
  # Иконка — из app/ (генератор app/tools/icons.py), если она уже в репо.
  ICON=""; [ -f "$REPO/app/build/windows/icon.ico" ] && ICON="-icon $REPO/app/build/windows/icon.ico"
  # shellcheck disable=SC2086
  # Версия продукта — общая для всего выпуска JetBox (SignPath: одна версия во всех файлах):
  # JETBOX_PRODUCT_VERSION=X.Y.Z.B; без неё — версия службы.
  PV=${JETBOX_PRODUCT_VERSION:-${TAG#v}.$JETBOX_N}
  PMAJ=$(echo "$PV" | cut -d. -f1); PMIN=$(echo "$PV" | cut -d. -f2); PPAT=$(echo "$PV" | cut -d. -f3); PBLD=$(echo "$PV" | cut -d. -f4)
  "$GVI" -64 $ICON -manifest client/manifest.xml -product-name JetBox -description "JetBox service" \
    -company JetNet -copyright "Copyright (c) 2022 NetBird GmbH & AUTHORS; JetBox build by JetNet. BSD-3-Clause" \
    -ver-major "$MAJ" -ver-minor "$MIN" -ver-patch "$PAT" -ver-build "$JETBOX_N" \
    -product-ver-major "$PMAJ" -product-ver-minor "$PMIN" -product-ver-patch "$PPAT" -product-ver-build "${PBLD:-0}" \
    -file-version "${TAG#v}.$JETBOX_N" -product-version "$PV" \
    -o client/resources_windows_amd64.syso versioninfo.json
  build windows amd64 "$OUT/windows-amd64/jetboxd.exe"
  rm -f client/resources_windows_amd64.syso
fi

printf '%s\n' "$VERSION" > "$OUT/jetboxd.version"
echo "версия службы: $VERSION"
