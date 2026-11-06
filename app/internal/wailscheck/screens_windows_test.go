//go:build windows

package wailscheck

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/w32"
)

// Wails на каждый WM_DISPLAYCHANGE перечисляет мониторы (GetAllScreens). Без исправления каждый
// вызов занимает новый слот syscall.NewCallback; их на процесс около 2000, дальше — fatal
// «too many callback functions», который recover не ловит. Так приложение падало через несколько
// дней на машине с частыми RDP-подключениями. Ошибка перечисления (нет экрана у CI) неважна: слот
// занимался до вызова.
func TestGetAllScreensReusesCallback(t *testing.T) {
	for i := 0; i < 2500; i++ {
		_, _ = w32.GetAllScreens()
	}
}
