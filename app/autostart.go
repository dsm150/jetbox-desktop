package main

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/dsm150/jetbox-desktop/app/controller"
)

// autostarter — «Запускать JetBox при входе» для этой ОС (Windows — HKCU\…\Run и отметка
// «Диспетчера задач» в StartupApproved\Run; macOS — launchctl enable/disable общего LaunchAgent из
// .pkg). Где приложение автозапуск не ведёт, supported=false: строка в «Настройках» скрыта.
type autostarter interface {
	autostartSupported() bool
	autostartEnabled() (bool, error)
	setAutostart(on bool) error
}

// autostartID — имя значения в HKCU\…\Run; то же пишет установщик (jetbox.nsi).
const autostartID = "JetBox"

// startupDisabled — отметка StartupApproved\Run «выключено в Диспетчере задач»: младший
// бит первого байта (0x03 — выключено, 0x02 и 0x06 — включено).
func startupDisabled(b []byte) bool { return len(b) > 0 && b[0]&1 == 1 }

// autostartMarker — метка «автозапуск по умолчанию уже применён» в каталоге настроек
// (рядом с app.log и box.json): у каждого пользователя Windows свой %APPDATA%.
const autostartMarker = "autostart-initialized"

// applyAutostartDefault — один раз на пользователя при первом запуске включает автозапуск,
// если его ещё нет; дальше выбор человека не трогает (как апстрим, ui/autostart_default.go).
// Метка пишется до включения: сбой посередине оставит «не включали», а не повтор при
// каждом запуске.
func applyAutostartDefault(a autostarter, dir string) {
	if a == nil || !a.autostartSupported() {
		return
	}
	marker := filepath.Join(dir, autostartMarker)
	if _, err := os.Stat(marker); !errors.Is(err, fs.ErrNotExist) {
		if err != nil {
			log.Printf("autostart default: %v", err)
		}
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("autostart default: %v", err)
		return
	}
	if err := os.WriteFile(marker, []byte("1\n"), 0o600); err != nil {
		log.Printf("autostart default: %v", err)
		return
	}
	on, err := a.autostartEnabled()
	if err != nil {
		log.Printf("autostart default: %v", err)
		return
	}
	if on {
		log.Print("autostart default: already enabled")
		return
	}
	if err := a.setAutostart(true); err != nil {
		log.Printf("autostart default: %v", err)
		return
	}
	log.Print("autostart default: enabled")
}

// autostartInitFlag — запуск из Active Setup (HKLM\…\Active Setup\Installed Components\JetBox,
// пишет установщик) при первом входе каждого пользователя Windows после установки, в том
// числе тихой: Run в HKCU установщик пишет только тому, от чьего имени он идёт.
const autostartInitFlag = "--autostart-init"

// autostartInitOnly — запуск с --autostart-init: включить автозапуск по умолчанию (метка
// autostart-initialized: выключенный человеком не включаем) и выйти. Active Setup ждёт процесс
// до появления рабочего стола — поэтому без Wails, службы и проверки единственного экземпляра.
func autostartInitOnly(args []string, a autostarter, dir string) bool {
	if !hasArg(args, autostartInitFlag) {
		return false
	}
	applyAutostartDefault(a, dir)
	return true
}

// syncAutostart — состояние автозапуска ОС в окно.
func (b *bridge) syncAutostart() {
	if b.auto == nil {
		return
	}
	st := controller.Autostart{Supported: b.auto.autostartSupported()}
	if st.Supported {
		on, err := b.auto.autostartEnabled()
		if err != nil {
			log.Printf("autostart: %v", err)
		}
		st.Enabled = on
	}
	b.ctl.SetAutostart(st)
}

// setAutostart — галочка «Запускать JetBox при входе». Отказ — только в журнал:
// окно покажет то, что на деле записано в ОС.
func (b *bridge) setAutostart(on bool) {
	if b.auto == nil || !b.auto.autostartSupported() {
		return
	}
	if err := b.auto.setAutostart(on); err != nil {
		log.Printf("autostart: set %v: %v", on, err)
	}
	b.syncAutostart()
}
