#!/bin/sh
# Сборка установщика: build-out/windows-amd64/JetBox-Setup.exe
#   packaging/windows/build.sh [версия X.Y.Z] [номер сборки]
# Версия продукта во всех полях установщика — X.Y.Z.<сборка>, как у JetBox.exe (app/build.sh).
# Нужно: build-out/windows-amd64/JetBox.exe (app/build.sh windows) и jetboxd.exe (daemon/scripts/build.sh),
# makensis (NSIS 3), wails3 (go install …/wails3) для загрузчика WebView2.
set -eu
VERSION=${1:-1.0.0}
BUILD=${2:-0}
# sha256: shasum (macOS) или sha256sum (Linux, Git Bash).
sha256_check() { if command -v sha256sum >/dev/null 2>&1; then sha256sum -c -; else shasum -a 256 -c -; fi; }
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
OUT="$ROOT/build-out/windows-amd64"
CACHE="$ROOT/build-out/cache"
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT

for f in JetBox.exe jetboxd.exe; do
  [ -f "$OUT/$f" ] || { echo "нет $OUT/$f — сначала соберите app и daemon" >&2; exit 1; }
done
mkdir -p "$CACHE"

# wintun 0.14.1 — с wintun.net, сверка sha256 (та же, что в CI апстрима)
WINTUN_SHA=07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51
if [ ! -f "$CACHE/wintun-0.14.1.zip" ]; then
  curl -sSL -m 120 -o "$CACHE/wintun-0.14.1.zip" https://www.wintun.net/builds/wintun-0.14.1.zip
fi
echo "$WINTUN_SHA  $CACHE/wintun-0.14.1.zip" | sha256_check >/dev/null
unzip -q -o -j "$CACHE/wintun-0.14.1.zip" wintun/bin/amd64/wintun.dll -d "$STAGE"

# Загрузчик WebView2 — из wails3 (так же делает CI апстрима)
WAILS3=${WAILS3:-$(command -v wails3 || echo "$HOME/go/bin/wails3")}
"$WAILS3" generate webview2bootstrapper -dir "$STAGE" >/dev/null
[ -f "$STAGE/MicrosoftEdgeWebview2Setup.exe" ] || { echo "нет загрузчика WebView2" >&2; exit 1; }

cp "$OUT/JetBox.exe" "$OUT/jetboxd.exe" "$STAGE/"
cp "$ROOT/LICENSE" "$STAGE/LICENSE.txt"
cp "$ROOT/NOTICE" "$STAGE/NOTICE.txt"
cp "$ROOT/app/build/windows/icon.ico" "$STAGE/icon.ico"
cp "$ROOT/packaging/windows/jetbox.nsi" "$ROOT/packaging/windows/lang.nsh" "$STAGE/"

if command -v makensis >/dev/null 2>&1; then
  # Git Bash на Windows (CI): makensis — родная программа, пути ему — в виде Windows.
  winpath() { if command -v cygpath >/dev/null 2>&1; then cygpath -w "$1"; else printf '%s' "$1"; fi; }
  (cd "$STAGE" && makensis -V2 -DVERSION="$VERSION" -DBUILD="$BUILD" -DSRC="$(winpath "$STAGE")" \
    -DOUT="$(winpath "$OUT/JetBox-Setup.exe")" jetbox.nsi)
elif [ -n "${NSIS_REMOTE:-}" ]; then
  # Без makensis на этой машине — собираем на машине Windows с NSIS (winget NSIS.NSIS) по ssh:
  # NSIS_REMOTE=пользователь@хост, NSIS_SSH_OPTS — доп. параметры ssh/scp (ключ, known_hosts).
  SSHO="-o BatchMode=yes ${NSIS_SSH_OPTS:-}"
  RID="jbbuild$$"
  ssh $SSHO "$NSIS_REMOTE" "New-Item -ItemType Directory -Force C:\\JetBoxBuild\\$RID | Out-Null"
  scp -q $SSHO "$STAGE"/* "$NSIS_REMOTE:C:/JetBoxBuild/$RID/"
  ssh $SSHO "$NSIS_REMOTE" "& 'C:\\Program Files (x86)\\NSIS\\makensis.exe' /V2 /DVERSION=$VERSION /DBUILD=$BUILD /DSRC=C:\\JetBoxBuild\\$RID /DOUT=C:\\JetBoxBuild\\$RID\\JetBox-Setup.exe C:\\JetBoxBuild\\$RID\\jetbox.nsi"
  scp -q $SSHO "$NSIS_REMOTE:C:/JetBoxBuild/$RID/JetBox-Setup.exe" "$OUT/JetBox-Setup.exe"
  ssh $SSHO "$NSIS_REMOTE" "Remove-Item -Recurse -Force C:\\JetBoxBuild\\$RID"
else
  echo "нет makensis: поставьте NSIS или задайте NSIS_REMOTE=пользователь@хост-с-NSIS" >&2
  exit 1
fi
cp "$OUT/JetBox-Setup.exe" "$OUT/JetBox-Setup-$VERSION.exe"
if command -v sha256sum >/dev/null 2>&1; then sha256sum "$OUT/JetBox-Setup.exe"; else shasum -a 256 "$OUT/JetBox-Setup.exe"; fi
