package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"
)

// autoConnectMarker — метка «флаг автоподключения службы выставлен по её состоянию» в
// каталоге настроек (рядом с app.log). Нужна при переходе с ≤1.0.3: там выключение щитом
// флаг не ставило, и служба после перезагрузки подключалась сама.
const autoConnectMarker = "autoconnect-synced"

// syncAutoConnectOnce — один раз на пользователя выставляет флаг службы по её текущему
// состоянию (Connected/Connecting — по галочке, иначе «не подключаться»). Метка — только
// после успешного SetConfig: служба не ответила — повторим при следующем запуске.
func (b *bridge) syncAutoConnectOnce(ctx context.Context) {
	if b.dir == "" {
		return
	}
	marker := filepath.Join(b.dir, autoConnectMarker)
	if _, err := os.Stat(marker); !errors.Is(err, fs.ErrNotExist) {
		if err != nil {
			log.Printf("autoconnect sync: %v", err)
		}
		return
	}
	if err := b.ctl.SyncAutoConnect(ctx); err != nil {
		log.Printf("autoconnect sync: %v", err)
		return
	}
	if err := os.WriteFile(marker, []byte("1\n"), 0o600); err != nil {
		log.Printf("autoconnect sync: %v", err)
		return
	}
	log.Print("autoconnect sync: done")
}

// syncAutoConnectSoon — syncAutoConnectOnce в фоне, не дольше 10 с: показ окна его не ждёт.
func (b *bridge) syncAutoConnectSoon() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		b.syncAutoConnectOnce(ctx)
	}()
}
