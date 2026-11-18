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
sha256() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }
# Воспроизводимость: установщик собирает ровно эта версия makensis — на ВМ (winget NSIS.NSIS) и в CI
# (choco nsis --version=3.13.0). Другая версия — другие заглушки NSIS и другой sha256, поэтому стоп.
NSIS_VERSION=${NSIS_VERSION:-v3.13}
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
# Файл зашит в wails3 v3.0.0-beta.3 (internal/commands/webview2); другая версия wails3 — стоп.
WEBVIEW2_SHA=7ebc4ce80143ef89cea86a61ea151502868db6caaa678b8b43660a66ace11c3a
"$WAILS3" generate webview2bootstrapper -dir "$STAGE" >/dev/null
[ -f "$STAGE/MicrosoftEdgeWebview2Setup.exe" ] || { echo "нет загрузчика WebView2" >&2; exit 1; }
echo "$WEBVIEW2_SHA  $STAGE/MicrosoftEdgeWebview2Setup.exe" | sha256_check >/dev/null ||
  { echo "загрузчик WebView2 не тот (sha256) — нужен wails3 v3.0.0-beta.3" >&2; exit 1; }

cp "$OUT/JetBox.exe" "$OUT/jetboxd.exe" "$STAGE/"
cp "$ROOT/LICENSE" "$STAGE/LICENSE.txt"
cp "$ROOT/NOTICE" "$STAGE/NOTICE.txt"
cp "$ROOT/app/build/windows/icon.ico" "$STAGE/icon.ico"
cp "$ROOT/packaging/windows/jetbox.nsi" "$ROOT/packaging/windows/lang.nsh" "$STAGE/"

nsis_version_check() { # фактическая версия makensis
  [ "$1" = "$NSIS_VERSION" ] || { echo "makensis $1, а нужен $NSIS_VERSION (воспроизводимая сборка)" >&2; exit 1; }
}

if command -v makensis >/dev/null 2>&1; then
  NSIS_GOT=$(makensis -VERSION | tr -d '\r')
  nsis_version_check "$NSIS_GOT"
  # Git Bash на Windows (CI): makensis — родная программа, пути ему — в виде Windows.
  winpath() { if command -v cygpath >/dev/null 2>&1; then cygpath -w "$1"; else printf '%s' "$1"; fi; }
  (cd "$STAGE" && makensis -V2 -DVERSION="$VERSION" -DBUILD="$BUILD" -DSRC="$(winpath "$STAGE")" \
    -DOUT="$(winpath "$OUT/JetBox-Setup.exe")" jetbox.nsi)
elif [ -n "${NSIS_REMOTE:-}" ]; then
  # Без makensis на этой машине — собираем на машине Windows с NSIS (winget NSIS.NSIS) по ssh:
  # NSIS_REMOTE=пользователь@хост, NSIS_SSH_OPTS — доп. параметры ssh/scp (ключ, known_hosts).
  SSHO="-o BatchMode=yes ${NSIS_SSH_OPTS:-}"
  RID="jbbuild$$"
  NSIS_GOT=$(ssh $SSHO "$NSIS_REMOTE" "& 'C:\\Program Files (x86)\\NSIS\\makensis.exe' /VERSION" | tr -d '\r')
  nsis_version_check "$NSIS_GOT"
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
# JetBox-Setup.exe.buildinfo — версия makensis и sha256 всего, что вошло в установщик (compare.sh).
{
  echo "makensis: $NSIS_GOT"
  # «имя: sha256» сами, а не вывод sha256sum: в Git Bash он помечает файлы «*» (двоичный режим).
  (cd "$STAGE" && for f in $(ls | LC_ALL=C sort); do echo "$f: $(sha256 "$f" | cut -d' ' -f1)"; done)
  echo "JetBox-Setup.exe: $(sha256 "$OUT/JetBox-Setup.exe" | cut -d' ' -f1)"
} > "$OUT/JetBox-Setup.exe.buildinfo"
if command -v sha256sum >/dev/null 2>&1; then sha256sum "$OUT/JetBox-Setup.exe"; else shasum -a 256 "$OUT/JetBox-Setup.exe"; fi
