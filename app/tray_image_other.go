//go:build !darwin

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// setTrayImage — значок трея: Wails сам берёт тёмный вариант, когда панель задач тёмная
// (w32.IsSystemCurrentlyDarkMode), и переключает его при смене темы.
//
// Первый вызов — из newTray до app.Run: Wails создаёт оба значка раздельно. Это важно:
// сеттеры форка «держат в паре» светлый и тёмный, пока у них один дескриптор, и после
// общего дескриптора развести их уже нельзя (systemtray_windows.go setIcon/setDarkModeIcon).
func setTrayImage(st *application.SystemTray, light, dark []byte) bool {
	st.SetIcon(light)
	st.SetDarkModeIcon(dark)
	return true
}
