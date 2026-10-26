//go:build !windows && !darwin

package platform

// Сборка на прочих ОС нужна только для тестов; приложение там не выпускается.
const (
	DaemonAddr        = "unix:///var/run/jetbox.sock"
	ForeignDaemonAddr = "unix:///var/run/netbird.sock"
	InterfaceName     = "jetbox0"

	// DeviceName — имя устройства в меше (служба не шлёт системное имя компьютера; только ASCII).
	DeviceName = "JetBox"
)
