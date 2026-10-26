# Общее для build.sh и test.sh: пути, версия, флаги -X из brand.env.
# shellcheck shell=sh
DAEMON=$(cd "$(dirname "$0")/.." && pwd)
REPO=$(cd "$DAEMON/.." && pwd)
. "$DAEMON/upstream.env"
SRC=$DAEMON/upstream/netbird-${TAG#v}
OUT=$REPO/build-out
VERSION=${TAG#v}+jetbox.$JETBOX_N
BRAND_PKG=github.com/netbirdio/netbird/client/jetboxbrand
# GUID адаптера wintun службы JetBox (постоянный, uuid5).
WINDOWS_GUID='{f062c963-bee2-512d-92aa-136b1984b0ae}'

# brand_lines <goos> — строки «Переменная=значение» для ОС (all + своя, своя перекрывает).
brand_lines() {
  awk -v os="$1" '
    /^[ \t]*(#|$)/ { next }
    ($1 == "all" || $1 == os) {
      line = $0; sub(/^[^ \t]+[ \t]+/, "", line)
      split(line, kv, "="); val[kv[1]] = substr(line, length(kv[1]) + 2); ord[kv[1]] = NR
    }
    END { for (k in val) print k "=" val[k] }
  ' "$DAEMON/brand.env" | sort
}

# ldflags <goos> — строка -ldflags для go build/test (версия + jetboxbrand + GUID на Windows).
ldflags() {
  f="-s -w -X github.com/netbirdio/netbird/version.version=$VERSION"
  brand_lines "$1" > "$DAEMON/upstream/.brand.$1"
  while IFS= read -r kv; do
    f="$f -X '$BRAND_PKG.$kv'"
  done < "$DAEMON/upstream/.brand.$1"
  [ "$1" = windows ] && f="$f -X github.com/netbirdio/netbird/client/iface/device.CustomWindowsGUIDString=$WINDOWS_GUID"
  printf '%s' "$f"
}
