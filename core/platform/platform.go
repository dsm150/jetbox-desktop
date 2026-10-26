// Package platform — имена и адреса JetBox (те же, что в daemon/brand.env).
// Меняются согласованно с установщиками обеих платформ: на них завязаны packaging/windows и packaging/macos.
package platform

// WireGuardPort — UDP-порт WireGuard службы JetBox (у официального NetBird 51820).
const WireGuardPort int64 = 51830

// SingleInstanceID — идентификатор единственного экземпляра приложения.
const SingleInstanceID = "net.jet-net.jetbox.desktop"
