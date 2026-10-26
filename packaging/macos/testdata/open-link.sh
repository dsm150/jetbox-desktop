#!/bin/sh
# Открыть ссылку подключения в приложении JetBox на Mac — как кнопка «Открыть в приложении JetBox»
# в Кабинете: jetbox://connect + фрагмент. Ссылка — из $JETBOX_LIVE_DIR/demo-link.local (600; каталог по
# умолчанию ~/jetbox-live), в вывод не печатается. Запуск от вошедшего пользователя: sh open-link.sh;
# затем «Подключить» в окне.
set -eu
LINK=${JETBOX_LIVE_DIR:-$HOME/jetbox-live}/demo-link.local
[ -f "$LINK" ] || { echo "нет $LINK"; exit 1; }
FRAG=$(sed -n 's/^[^#]*#//p' "$LINK" | head -1)
[ -n "$FRAG" ] || { echo "в ссылке нет фрагмента"; exit 1; }
open "jetbox://connect#$FRAG"
echo "ссылка открыта (длина фрагмента ${#FRAG}); дальше — «Подключить» в окне JetBox"
