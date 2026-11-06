package main

import (
	"errors"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
	// approvedKey — куда «Диспетчер задач → Автозагрузка» пишет выключение (значение по имени из Run).
	approvedKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
)

// regAutostart — HKCU\…\Run\JetBox = "<путь>\JetBox.exe" --autostart (то же пишет установщик).
// Своё, без Wails: нужно и в --autostart-init, где Wails не поднимается. Включено — есть
// значение в Run и нет отметки «выключено» в StartupApproved\Run.
type regAutostart struct{}

func newAutostart() autostarter { return regAutostart{} }

func (regAutostart) autostartSupported() bool { return true }

func (regAutostart) autostartEnabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer k.Close()
	if _, _, err := k.GetStringValue(autostartID); errors.Is(err, registry.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	a, err := registry.OpenKey(registry.CURRENT_USER, approvedKey, registry.QUERY_VALUE)
	if err != nil {
		return true, nil // отметок нет — Windows запускает всё из Run
	}
	defer a.Close()
	b, _, err := a.GetBinaryValue(autostartID)
	if err != nil {
		return true, nil
	}
	return !startupDisabled(b), nil
}

func (regAutostart) setAutostart(on bool) error {
	if on {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer k.Close()
		if err := k.SetStringValue(autostartID, `"`+exe+`" `+autostartFlag); err != nil {
			return err
		}
	} else if err := deleteValue(runKey, autostartID); err != nil {
		return err
	}
	// Отметка «выключено в Диспетчере задач» перебила бы включение из JetBox.
	return deleteValue(approvedKey, autostartID)
}

// deleteValue удаляет значение HKCU\key\name; нет ключа или значения — не ошибка.
func deleteValue(key, name string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
