// Package icons — значки трея и окна JetBox (генерирует app/tools/icons.py).
package icons

import _ "embed"

// Значки трея по состояниям, у каждого — вариант для светлой и для тёмной панели
// (строки меню macOS / панели задач Windows).
var (
	//go:embed tray-on-light.png
	TrayOnLight []byte
	//go:embed tray-on-dark.png
	TrayOnDark []byte
	//go:embed tray-off-light.png
	TrayOffLight []byte
	//go:embed tray-off-dark.png
	TrayOffDark []byte
	//go:embed tray-connecting-light.png
	TrayConnectingLight []byte
	//go:embed tray-connecting-dark.png
	TrayConnectingDark []byte
	//go:embed tray-alert-light.png
	TrayAlertLight []byte
	//go:embed tray-alert-dark.png
	TrayAlertDark []byte
)

//go:embed app-256.png
var App []byte
