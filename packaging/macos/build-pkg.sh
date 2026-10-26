#!/bin/sh
# Собирает установщик JetBox для Mac: build-out/macos/JetBox-<версия>.pkg (+ копия JetBox.pkg).
#
# Вход:
#   build-out/darwin-universal/JetBox.app  — приложение (app/build.sh, исполнитель Windows);
#   build-out/darwin-universal/jetboxd     — служба (daemon/scripts/build.sh).
# Служба ставится вне бандла: /Library/Application Support/JetBox/jetboxd (root:wheel — её не
# подменить без пароля, и она не пропадает вместе с приложением); launchd — net.jet-net.jetbox.daemon.
# Автозапуск приложения при входе — LaunchAgent net.jet-net.jetbox.desktop (`--autostart`).
#
# Флаги:
#   --daemon-only     приложения ещё нет: пакет только со службой (для прогонов службы)
#   --version X.Y.Z   версия пакета (по умолчанию — CFBundleShortVersionString приложения, иначе 1.0.0)
#   --no-notarize     только подпись (нотаризация ~1 мин, нужна сеть)
#   --unsigned        без подписи вовсе (проверка сборки на машине без идентичностей)
#
# Подпись и нотаризация — из окружения (значений в репо нет):
#   JETBOX_APP_SIGN   идентичность «Developer ID Application: <владелец> (<Team ID>)» из связки ключей
#   JETBOX_PKG_SIGN   идентичность «Developer ID Installer: <владелец> (<Team ID>)»
#   JETBOX_ASC_DIR    каталог ключа App Store Connect API для notarytool: asc.p8, asc.keyid, asc.issuer
# Удобно держать их в локальном файле вне git — он подхватывается сам, если есть:
#   JETBOX_SIGN_ENV (по умолчанию ~/.config/jetbox-desktop/macos-sign.env), строки вида KEY='значение'.
set -eu
HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/../.." && pwd)
IN=$REPO/build-out/darwin-universal
# JETBOX_APP — другой источник приложения (проверка упаковки поддельным: testdata/make-fake-app.sh).
APP_SRC=${JETBOX_APP:-$IN/JetBox.app}
OUT=$REPO/build-out/macos
SIGN_ENV=${JETBOX_SIGN_ENV:-$HOME/.config/jetbox-desktop/macos-sign.env}
# shellcheck disable=SC1090
[ -f "$SIGN_ENV" ] && . "$SIGN_ENV"
APP_SIGN=${JETBOX_APP_SIGN:-}
PKG_SIGN=${JETBOX_PKG_SIGN:-}
ASC=${JETBOX_ASC_DIR:-}
BUNDLE_ID=net.jet-net.jetbox.desktop
LABEL=net.jet-net.jetbox.daemon
# Минимальная macOS: рантайм Go 1.27 (служба и приложение) собран под macOS 13 — ниже не запустится.
# Если приложение требует больше (LSMinimumSystemVersion), берём его значение.
MIN_OS=13.0

DAEMON_ONLY=0; NOTARIZE=1; SIGN=1; VER=""
while [ $# -gt 0 ]; do
  case "$1" in
    --daemon-only) DAEMON_ONLY=1 ;;
    --no-notarize) NOTARIZE=0 ;;
    --unsigned) SIGN=0; NOTARIZE=0 ;;
    --version) VER=$2; shift ;;
    *) echo "неизвестный флаг $1"; exit 2 ;;
  esac
  shift
done

if [ "$SIGN" = 1 ] && { [ -z "$APP_SIGN" ] || [ -z "$PKG_SIGN" ]; }; then
  echo "нужны JETBOX_APP_SIGN и JETBOX_PKG_SIGN (или $SIGN_ENV) — либо --unsigned"; exit 1
fi
if [ "$NOTARIZE" = 1 ] && [ ! -f "$ASC/asc.p8" ]; then
  echo "нужен JETBOX_ASC_DIR с asc.p8/asc.keyid/asc.issuer (или $SIGN_ENV) — либо --no-notarize"; exit 1
fi
[ -x "$IN/jetboxd" ] || { echo "нет $IN/jetboxd — сначала: sh daemon/scripts/build.sh darwin"; exit 1; }
lipo "$IN/jetboxd" -verify_arch x86_64 arm64 || { echo "jetboxd не universal"; exit 1; }

WORK=$OUT/work
rm -rf "$WORK"
HELPER="$WORK/root/Library/Application Support/JetBox"
mkdir -p "$HELPER" "$WORK/root/Library/LaunchDaemons" "$WORK/scripts"
APP=$WORK/root/Applications/JetBox.app

ditto "$IN/jetboxd" "$HELPER/jetboxd"
cp "$HERE/uninstall.sh" "$HELPER/uninstall.sh"
chmod 755 "$HELPER/jetboxd" "$HELPER/uninstall.sh"
cp "$HERE/$LABEL.plist" "$WORK/root/Library/LaunchDaemons/$LABEL.plist"
cp "$HERE/scripts/preinstall" "$HERE/scripts/postinstall" "$HERE/scripts/launchd.sh" "$WORK/scripts/"
chmod 755 "$WORK/scripts/preinstall" "$WORK/scripts/postinstall"
chmod 644 "$WORK/scripts/launchd.sh"
plutil -lint "$WORK/root/Library/LaunchDaemons/$LABEL.plist" >/dev/null

EXEC=""
if [ "$DAEMON_ONLY" = 0 ]; then
  [ -d "$APP_SRC" ] || { echo "нет $APP_SRC (app/build.sh) — или --daemon-only"; exit 1; }
  mkdir -p "$WORK/root/Applications" "$WORK/root/Library/LaunchAgents"
  ditto "$APP_SRC" "$APP"
  got=$(defaults read "$APP/Contents/Info" CFBundleIdentifier)
  [ "$got" = "$BUNDLE_ID" ] || { echo "bundle ID приложения $got, ждали $BUNDLE_ID"; exit 1; }
  EXEC=$(defaults read "$APP/Contents/Info" CFBundleExecutable)
  sed "s|@EXEC@|$EXEC|" "$HERE/$BUNDLE_ID.plist.in" > "$WORK/root/Library/LaunchAgents/$BUNDLE_ID.plist"
  plutil -lint "$WORK/root/Library/LaunchAgents/$BUNDLE_ID.plist" >/dev/null
  [ -n "$VER" ] || VER=$(defaults read "$APP/Contents/Info" CFBundleShortVersionString 2>/dev/null || true)
  app_min=$(defaults read "$APP/Contents/Info" LSMinimumSystemVersion 2>/dev/null || echo 0)
  if [ "$(printf '%s\n%s\n' "$MIN_OS" "$app_min" | sort -t. -k1,1n -k2,2n | tail -1)" = "$app_min" ]; then MIN_OS=$app_min; fi
fi
[ -n "$VER" ] || VER=1.0.0

if [ "$SIGN" = 1 ]; then
  codesign --force --options runtime --timestamp --identifier "$LABEL" --sign "$APP_SIGN" "$HELPER/jetboxd"
  codesign --verify --strict "$HELPER/jetboxd"
  if [ "$DAEMON_ONLY" = 0 ]; then
    # Изнутри наружу, без --deep: вложенные бандлы (глубже — раньше), затем отдельные Mach-O вне
    # вложенных бандлов, затем сам бандл. Ошибка любой подписи валит сборку (set -e).
    find "$APP/Contents" -depth -type d \( -name '*.framework' -o -name '*.app' -o -name '*.xpc' \
      -o -name '*.appex' -o -name '*.bundle' \) > "$WORK/nested.txt"
    while IFS= read -r b; do
      codesign --force --options runtime --timestamp --sign "$APP_SIGN" "$b"
    done < "$WORK/nested.txt"
    find "$APP/Contents" -type f -perm -u+x > "$WORK/exec.txt"
    while IFS= read -r f; do
      [ "$f" = "$APP/Contents/MacOS/$EXEC" ] && continue
      inside=0
      while IFS= read -r b; do case "$f" in "$b"/*) inside=1 ;; esac; done < "$WORK/nested.txt"
      [ "$inside" = 1 ] && continue
      file "$f" | grep -q Mach-O || continue
      codesign --force --options runtime --timestamp --sign "$APP_SIGN" "$f"
    done < "$WORK/exec.txt"
    codesign --force --options runtime --timestamp --sign "$APP_SIGN" "$APP"
    codesign --verify --strict --deep "$APP"
  fi
fi

# Компонент: бандл не «переезжает» в найденную где-то копию с тем же ID (иначе installer
# поставит его не в /Applications, а LaunchAgent будет искать его там).
pkgbuild --analyze --root "$WORK/root" "$WORK/component.plist" >/dev/null
if [ "$DAEMON_ONLY" = 0 ]; then
  plutil -replace 0.BundleIsRelocatable -bool NO "$WORK/component.plist"
  # Без проверки версии бандла: иначе installer молча пропускает приложение, если считает
  # установленное «не старее» (та же CFBundleShortVersionString при новой сборке и т.п.).
  plutil -replace 0.BundleIsVersionChecked -bool NO "$WORK/component.plist"
fi
pkgbuild --root "$WORK/root" --component-plist "$WORK/component.plist" --scripts "$WORK/scripts" \
  --identifier "$BUNDLE_ID" --version "$VER" --install-location / "$WORK/JetBox-component.pkg" >/dev/null

# Дистрибутив: заголовок «JetBox», нативно на arm64 и x86_64 (без запроса Rosetta), macOS $MIN_OS+.
cat > "$WORK/distribution.xml" <<XML
<?xml version="1.0" encoding="utf-8"?>
<installer-gui-script minSpecVersion="2">
    <title>JetBox</title>
    <options customize="never" require-scripts="true" hostArchitectures="arm64,x86_64"/>
    <domains enable_anywhere="false" enable_currentUserHome="false" enable_localSystem="true"/>
    <volume-check><allowed-os-versions><os-version min="$MIN_OS"/></allowed-os-versions></volume-check>
    <choices-outline><line choice="default"><line choice="$BUNDLE_ID"/></line></choices-outline>
    <choice id="default"/>
    <choice id="$BUNDLE_ID" visible="false"><pkg-ref id="$BUNDLE_ID"/></choice>
    <pkg-ref id="$BUNDLE_ID" version="$VER" onConclusion="none">JetBox-component.pkg</pkg-ref>
</installer-gui-script>
XML

# Пробные сборки (без подписи/нотаризации) — под своим именем: релизное JetBox-<версия>.pkg и
# JetBox.pkg ими не затираются.
SUFFIX=""
[ "$SIGN" = 0 ] && SUFFIX=-unsigned
[ "$SIGN" = 1 ] && [ "$NOTARIZE" = 0 ] && SUFFIX=-not-notarized
PKG=$OUT/JetBox-$VER$SUFFIX.pkg
rm -f "$PKG"
if [ "$SIGN" = 1 ]; then
  productbuild --distribution "$WORK/distribution.xml" --package-path "$WORK" --sign "$PKG_SIGN" --timestamp "$PKG" >/dev/null
  pkgutil --check-signature "$PKG" | sed -n 2,3p
else
  productbuild --distribution "$WORK/distribution.xml" --package-path "$WORK" "$PKG" >/dev/null
fi

if [ "$NOTARIZE" = 1 ]; then
  xcrun notarytool submit "$PKG" --key "$ASC/asc.p8" --key-id "$(cat "$ASC/asc.keyid")" \
    --issuer "$(cat "$ASC/asc.issuer")" --wait | grep -E "status:|id:" | tail -2
  xcrun stapler staple "$PKG" >/dev/null
  spctl -a -vv -t install "$PKG" 2>&1 | sed -n 1,2p
fi

[ -z "$SUFFIX" ] && cp "$PKG" "$OUT/JetBox.pkg"
shasum -a 256 "$PKG" | tee "$PKG.sha256"
echo "готово: $PKG (версия $VER, служба $(cat "$REPO/build-out/jetboxd.version" 2>/dev/null || echo ?))"
