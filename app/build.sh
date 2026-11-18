#!/bin/sh
# Сборка приложения JetBox: build-out/windows-amd64/JetBox.exe и build-out/darwin-universal/JetBox.app.
#   app/build.sh [windows|darwin|all] [версия X.Y.Z] [номер сборки]
# Служба (jetboxd) собирается отдельно (daemon/) и кладётся рядом в build-out/<os>-<arch>/.
# Windows собирается с любого хоста (без CGO); macOS — только на маке (CGO, Xcode).
set -eu
TARGET=${1:-all}
VERSION=${2:-1.0.0}
BUILD=${3:-1}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
APP="$ROOT/app"
OUT="$ROOT/build-out"
# Воспроизводимость (локально = CI, одинаковые sha256): -trimpath и -buildvcs=false убирают пути и метку
# коммита, -buildid= — идентификатор сборки, Go — ровно версия из строки toolchain в go.mod.
LDFLAGS="-s -w -buildid= -X main.version=$VERSION"
GOTOOLCHAIN=$(sed -n 's/^toolchain \(go[0-9.]*\)$/\1/p' "$ROOT/go.mod")
[ -n "$GOTOOLCHAIN" ] || { echo "в go.mod нет строки toolchain goX.Y.Z" >&2; exit 1; }
export GOTOOLCHAIN
# Минимальная macOS (LSMinimumSystemVersion и -mmacosx-version-min). 13.0 — не выбор,
# а нижняя граница тулчейна: рантайм Go 1.27 собран под macOS 13. Переопределяется
# окружением (MACOS_MIN=14.0 app/build.sh darwin …), но ниже 13 бинарь не запустится.
MACOS_MIN=${MACOS_MIN:-13.0}

# Строго ^[0-9]+\.[0-9]+\.[0-9]+$: case отсекает любые символы, кроме цифр и точек
# (в том числе перевод строки — grep проверял бы строки по отдельности), grep — форму.
case "$VERSION" in *[!0-9.]* | '') echo "версия должна быть вида X.Y.Z: $VERSION" >&2; exit 2 ;; esac
if ! printf '%s\n' "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "версия должна быть вида X.Y.Z: $VERSION" >&2; exit 2
fi
case "$BUILD" in *[!0-9]* | '') echo "номер сборки должен быть числом: $BUILD" >&2; exit 2 ;; esac
case "$TARGET" in
  windows | darwin | all) ;;
  *) echo "app/build.sh [windows|darwin|all] [версия X.Y.Z] [сборка]" >&2; exit 2 ;;
esac
VMAJ=$(echo "$VERSION" | cut -d. -f1)
VMIN=$(echo "$VERSION" | cut -d. -f2)
VPAT=$(echo "$VERSION" | cut -d. -f3)

# Временные файлы сборки Windows убираются при любом выходе, в том числе по Ctrl+C:
# брошенный rsrc_windows_amd64.syso попал бы в следующую сборку под другую ОС/версию.
SYSO="$APP/rsrc_windows_amd64.syso"
TMP=""
cleanup() {
  rm -f "$SYSO"
  if [ -n "$TMP" ]; then rm -rf "$TMP"; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Wails — форк из go.mod с нашими исправлениями (app/wails/patches): go.work с replace на копию.
# Только для сборки приложения: служба собирается в своём модуле.
WAILS_WORK=$(sh "$APP/wails/prepare.sh")

(cd "$APP/frontend" && pnpm install --frozen-lockfile --ignore-scripts >/dev/null && pnpm build >/dev/null)

sha256() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }
# <файл>.buildinfo — чем собран файл: packaging/windows/compare.sh сверяет их у двух сборок.
buildinfo() { # out-файл
  {
    echo "go: $(go env GOVERSION)"
    echo "node: $(node -v)"
    echo "pnpm: $(pnpm -v)"
    echo "frontend/dist: $(cd "$APP/frontend/dist" && find . -type f | LC_ALL=C sort | while IFS= read -r f; do echo "$(sha256 "$f" | cut -d' ' -f1) $f"; done | sha256 | cut -d' ' -f1)"
    echo "wails: $(sed -n 's/^replace github.com\/wailsapp\/wails\/v3 => //p' "$ROOT/go.mod") + $(cat "$ROOT"/app/wails/patches/*.patch | sha256 | cut -d' ' -f1)"
    echo "$(basename "$1"): $(sha256 "$1" | cut -d' ' -f1)"
  } > "$1.buildinfo"
}

build_windows() {
  mkdir -p "$OUT/windows-amd64"
  TMP=$(mktemp -d)
  # Ресурсы exe: имя «JetBox» (его показывает браузер в «Открыть JetBox?»), версия, значок, манифест asInvoker.
  cp "$APP/build/windows/versioninfo.json" "$APP/build/windows/icon.ico" "$APP/build/windows/jetbox.exe.manifest" "$TMP/"
  (cd "$TMP" && go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.7.0 \
    -64 -o "$SYSO" \
    -ver-major "$VMAJ" -ver-minor "$VMIN" -ver-patch "$VPAT" -ver-build "$BUILD" \
    -product-ver-major "$VMAJ" -product-ver-minor "$VMIN" -product-ver-patch "$VPAT" -product-ver-build "$BUILD" \
    -file-version "$VERSION.$BUILD" -product-version "$VERSION.$BUILD" \
    versioninfo.json)
  # Без CGO: Wails v3 на Windows говорит с WebView2 через COM на чистом Go.
  (cd "$ROOT" && GOWORK="$WAILS_WORK" GOOS=windows GOARCH=amd64 GOAMD64=v1 CGO_ENABLED=0 \
    go build -tags production -trimpath -buildvcs=false \
    -ldflags "$LDFLAGS -H windowsgui" -o "$OUT/windows-amd64/JetBox.exe" ./app)
  buildinfo "$OUT/windows-amd64/JetBox.exe"
  cleanup # .syso не должен попасть в сборку macOS (target all)
  TMP=""
  echo "$OUT/windows-amd64/JetBox.exe"
}

build_darwin() {
  BUNDLE="$OUT/darwin-universal/JetBox.app"
  rm -rf "$BUNDLE"
  mkdir -p "$BUNDLE/Contents/MacOS" "$BUNDLE/Contents/Resources" "$OUT/darwin-amd64" "$OUT/darwin-arm64"
  for ARCH in amd64 arm64; do
    (cd "$ROOT" && GOOS=darwin GOARCH=$ARCH CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=$MACOS_MIN \
      CGO_CFLAGS="-mmacosx-version-min=$MACOS_MIN" CGO_LDFLAGS="-mmacosx-version-min=$MACOS_MIN" \
      GOWORK="$WAILS_WORK" go build -tags production -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$OUT/darwin-$ARCH/JetBox" ./app)
  done
  lipo -create -output "$BUNDLE/Contents/MacOS/JetBox" "$OUT/darwin-amd64/JetBox" "$OUT/darwin-arm64/JetBox"
  sed -e "s/__VERSION__/$VERSION/" -e "s/__BUILD__/$BUILD/" -e "s/__MINOS__/$MACOS_MIN/" \
    "$APP/build/darwin/Info.plist" > "$BUNDLE/Contents/Info.plist"
  cp "$APP/build/darwin/JetBox.icns" "$BUNDLE/Contents/Resources/JetBox.icns"
  plutil -lint "$BUNDLE/Contents/Info.plist" >/dev/null
  echo "$BUNDLE"
}

case "$TARGET" in
  windows) build_windows ;;
  darwin) build_darwin ;;
  all) build_windows; build_darwin ;;
esac
