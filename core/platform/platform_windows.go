//go:build windows

package platform

// DaemonAddr — канал службы JetBox.
const DaemonAddr = "npipe://jetbox"

// ForeignDaemonAddr — канал официального NetBird (только чтение состояния).
const ForeignDaemonAddr = "npipe://netbird"

// InterfaceName — имя сетевого адаптера JetBox.
const InterfaceName = "JetBox"

// DeviceName — имя устройства в меше. Служба JetBox не отправляет серверу системное имя
// компьютера (приватность), поэтому имя приходит только из LoginRequest.Hostname; Кабинет
// показывает собственные имена устройств. Только ASCII: имя становится DNS-меткой в меше
// (дубли NetBird различает сам).
const DeviceName = "JetBox-Windows"
