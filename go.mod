module github.com/dsm150/jetbox-desktop

go 1.26.0

require (
	github.com/Microsoft/go-winio v0.6.4
	github.com/netbirdio/netbird v0.81.4
	github.com/wailsapp/wails/v3 v3.0.0-beta.3
	golang.org/x/sys v0.49.0
	golang.zx2c4.com/wireguard/windows v0.5.3
	google.golang.org/grpc v1.84.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
)

require (
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.26.3 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260706201446-f0a921348800 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
)

replace github.com/wailsapp/wails/v3 => github.com/netbirdio/wails/v3 v3.0.0-beta.3.0.20261009120932-4e087a8bfd6d
