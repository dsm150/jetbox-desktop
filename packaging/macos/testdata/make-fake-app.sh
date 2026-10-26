#!/bin/sh
# Поддельное JetBox.app для проверки упаковки, пока настоящего (app/build.sh) нет:
# universal-исполняемый «JetBox», вложенный .framework с dylib и отдельный вспомогательный Mach-O —
# чтобы build-pkg.sh прошёл ветку подписи вложенного, а test.sh — проверки приложения и LaunchAgent.
# Кладёт в каталог из $1 (по умолчанию — временный); НЕ в build-out (там место настоящему).
set -eu
DST=${1:-$(mktemp -d)}
APP=$DST/JetBox.app
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources" "$APP/Contents/Frameworks/Fake.framework/Versions/A"
printf 'int main(void){return 0;}\n' > "$T/main.c"
printf 'int fake(void){return 1;}\n' > "$T/lib.c"
xcrun clang -arch x86_64 -arch arm64 -mmacosx-version-min=11.0 -o "$APP/Contents/MacOS/JetBox" "$T/main.c"
xcrun clang -arch x86_64 -arch arm64 -mmacosx-version-min=11.0 -o "$APP/Contents/MacOS/helper" "$T/main.c"
xcrun clang -arch x86_64 -arch arm64 -mmacosx-version-min=11.0 -dynamiclib \
  -install_name @rpath/Fake.framework/Versions/A/Fake -o "$APP/Contents/Frameworks/Fake.framework/Versions/A/Fake" "$T/lib.c"
( cd "$APP/Contents/Frameworks/Fake.framework/Versions" && ln -s A Current )
( cd "$APP/Contents/Frameworks/Fake.framework" && ln -s Versions/Current/Fake Fake )
mkdir -p "$APP/Contents/Frameworks/Fake.framework/Versions/A/Resources"
cat > "$APP/Contents/Frameworks/Fake.framework/Versions/A/Resources/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
	<key>CFBundleIdentifier</key><string>net.jet-net.jetbox.fake</string>
	<key>CFBundleExecutable</key><string>Fake</string>
	<key>CFBundlePackageType</key><string>FMWK</string>
</dict></plist>
PLIST
( cd "$APP/Contents/Frameworks/Fake.framework" && ln -s Versions/Current/Resources Resources )
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleIdentifier</key><string>net.jet-net.jetbox.desktop</string>
	<key>CFBundleName</key><string>JetBox</string>
	<key>CFBundleExecutable</key><string>JetBox</string>
	<key>CFBundleShortVersionString</key><string>0.0.0</string>
	<key>CFBundleVersion</key><string>0</string>
	<key>LSMinimumSystemVersion</key><string>11.0</string>
	<key>LSUIElement</key><true/>
</dict></plist>
PLIST
echo "$APP"
