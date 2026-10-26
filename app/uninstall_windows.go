//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// platformUninstall — Windows: штатный деинсталлятор рядом с JetBox.exe. Запуск через
// ShellExecute: он сам спросит права (UAC); деинсталлятор снимает компьютер с учёта и
// убирает всё, что поставил установщик.
func platformUninstall() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	un := filepath.Join(dir, "Uninstall.exe")
	if _, err := os.Stat(un); err != nil {
		return fmt.Errorf("нет деинсталлятора: %w", err)
	}
	verb, _ := windows.UTF16PtrFromString("open")
	file, _ := windows.UTF16PtrFromString(un)
	cwd, _ := windows.UTF16PtrFromString(dir)
	err = windows.ShellExecute(0, verb, file, nil, cwd, windows.SW_SHOWNORMAL)
	if errors.Is(err, windows.ERROR_CANCELLED) {
		return errUninstallCanceled
	}
	return err
}
