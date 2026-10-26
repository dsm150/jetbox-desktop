#!/bin/sh
# Все тесты JetBox_Desktop — работает и на чистом клоне.
#   sh test.sh                 фронтенд + Go (core/, app/) + служба (daemon/)
#   SKIP_DAEMON=1 sh test.sh   без тестов службы (им нужен исходник NetBird v0.81.4 и несколько минут)
# Фронтенд собирается первым: пакет app встраивает app/frontend/dist (//go:embed all:frontend/dist),
# а dist в git не хранится — без сборки `go test ./app/...` падает на этапе компиляции.
# Нужно: Go 1.26+, Node + pnpm; для службы — то, что требует daemon/scripts/test.sh.
set -eu
ROOT=$(cd "$(dirname "$0")" && pwd)

echo "== фронтенд"
(cd "$ROOT/app/frontend" && pnpm install --frozen-lockfile --ignore-scripts >/dev/null && pnpm build >/dev/null)

cd "$ROOT"
echo "== NSIS lint"
sh "$ROOT/packaging/windows/lint.sh"

echo "== go vet (своя ОС, Windows, macOS)"
go vet ./core/... ./app/...
GOOS=windows go vet ./core/... ./app/...
GOOS=darwin go vet ./core/...

echo "== go test -race"
go test -race -count=1 ./core/... ./app/...

if [ "${SKIP_DAEMON:-}" = 1 ]; then
  echo "== служба: пропущено (SKIP_DAEMON=1)"
else
  echo "== служба"
  sh "$ROOT/daemon/scripts/test.sh"
fi
echo "ВСЁ ЗЕЛЁНОЕ"
