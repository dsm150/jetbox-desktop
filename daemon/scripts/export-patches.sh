#!/bin/sh
# Выгружает коммиты поверх `upstream` из daemon/upstream/netbird-<тег> в daemon/patches/ (format-patch).
# Порядок работы с патчем: prepare.sh → правка и `git commit` (или `git commit --amend`/rebase -i
# над нужным коммитом) в дереве → export-patches.sh → тесты → коммит в репо JetBox.
set -eu
# sha256: shasum (macOS) или sha256sum (Linux, Git Bash).
sha256_sum() { if command -v sha256sum >/dev/null 2>&1; then sha256sum; else shasum -a 256; fi; }
sha256_check() { if command -v sha256sum >/dev/null 2>&1; then sha256sum -c -; else shasum -a 256 -c -; fi; }
DAEMON=$(cd "$(dirname "$0")/.." && pwd)
. "$DAEMON/upstream.env"
SRC=$DAEMON/upstream/netbird-${TAG#v}
rm -f "$DAEMON"/patches/*.patch
git -C "$SRC" format-patch -q --no-signature --zero-commit --no-numbered -N \
  -o "$DAEMON/patches" upstream..HEAD
cat "$DAEMON"/patches/*.patch | sha256_sum | cut -d' ' -f1 > "$SRC.patches.sha256"
ls "$DAEMON/patches"
