//go:build darwin

package platform

// DaemonAddr — сокет службы JetBox.
const DaemonAddr = "unix:///var/run/jetbox.sock"

// ForeignDaemonAddr — сокет официального NetBird (только чтение состояния).
const ForeignDaemonAddr = "unix:///var/run/netbird.sock"

// InterfaceName — utun службы JetBox.
const InterfaceName = "utun120"

// DeviceName — имя устройства в меше. Служба JetBox не отправляет серверу системное имя
// компьютера (приватность), поэтому имя приходит только из LoginRequest.Hostname; Кабинет
// показывает собственные имена устройств. Только ASCII: имя становится DNS-меткой в меше
// (дубли NetBird различает сам).
const DeviceName = "JetBox-Mac"
