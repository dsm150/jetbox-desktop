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

// sshOffMarker — метка «SSH-сервер NetBird в профиле службы выключен» рядом с app.log.
// До 1.0.5 профиль мог остаться с ним включённым, а включённый SSH запрещает обычному
// пользователю менять management и снимать пир с учёта (server/ssh_gate.go).
const sshOffMarker = "ssh-off"

// disableSSHOnce — один раз на пользователя выключает SSH-сервер NetBird в профилях
// службы (активном и адресно в остальных — daemonclient.DisableServerSSH). Метка —
// только после успеха по активному: служба не ответила — повторим при следующем
// запуске; ошибки — только в журнал.
func (b *bridge) disableSSHOnce(ctx context.Context) {
	if b.dir == "" || b.sshOff == nil {
		return
	}
	marker := filepath.Join(b.dir, sshOffMarker)
	if _, err := os.Stat(marker); !errors.Is(err, fs.ErrNotExist) {
		if err != nil {
			log.Printf("ssh off: %v", err)
		}
		return
	}
	if err := b.sshOff(ctx); err != nil {
		log.Printf("ssh off: %v", err)
		return
	}
	if err := os.WriteFile(marker, []byte("1\n"), 0o600); err != nil {
		log.Printf("ssh off: %v", err)
		return
	}
	log.Print("ssh off: done")
}

// recoverReplace — довести замену подключения, прерванную прошлым запуском
// (flow.Recover); ошибка — в журнал, Connect и Reconnect попробуют ещё раз.
func (b *bridge) recoverReplace(ctx context.Context) {
	if b.recover == nil {
		return
	}
	changed, err := b.recover(ctx)
	if err != nil {
		log.Printf("recover: %v", err)
	}
	if changed && b.ctl != nil {
		b.ctl.Refresh(ctx) // профиль службы вернули — панель по новому состоянию
	}
}

// syncAutoConnectSoon — recoverReplace (до 30 с), syncAutoConnectOnce и disableSSHOnce
// (до 10 с каждое) в фоне: показ окна их не ждёт. Ссылку из argv они не задерживают:
// Connect сам сверяет профиль и шлёт ServerSSHAllowed=false первым запросом Configure.
func (b *bridge) syncAutoConnectSoon() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		b.recoverReplace(ctx)
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		b.syncAutoConnectOnce(ctx)
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		b.disableSSHOnce(ctx)
	}()
}
