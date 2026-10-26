// Package othervpn находит другой VPN, который заберёт себе весь трафик: два VPN
// «весь интернет сюда» на одной машине не уживаются (спайки Windows и macOS).
package othervpn

import (
	"context"
	"strings"
	"time"

	"github.com/netbirdio/netbird/client/proto"

	"github.com/dsm150/jetbox-desktop/core/daemonclient"
	"github.com/dsm150/jetbox-desktop/core/exitpick"
)

// Kind — что нашли.
type Kind int

const (
	None            Kind = iota
	OfficialNetBird      // официальный NetBird подключён с выходом
	OtherTunnel          // трафик в интернет идёт через чужой туннель
)

// Finding — итог проверки.
type Finding struct {
	Kind   Kind
	Detail string // для OtherTunnel — имя интерфейса («NetBird» для wt0)
}

// netbirdIface — имя интерфейса официального NetBird по умолчанию.
const netbirdIface = "wt0"

// Detector проверяет перед подключением. Нулевое значение безопасно: признак без функции
// пропускается.
type Detector struct {
	OwnIface string
	foreign  func(ctx context.Context) (bool, error) // официальный NetBird с выходом?
	tunnels  func(own string) ([]string, error)      // чужие интерфейсы, через которые уходит интернет
}

// New — детектор для этой ОС: адрес чужой службы и своё имя интерфейса из core/platform.
func New(foreignAddr, ownIface string) Detector {
	return Detector{
		OwnIface: ownIface,
		foreign:  func(ctx context.Context) (bool, error) { return officialWithExit(ctx, foreignAddr) },
		tunnels:  internetTunnels,
	}
}

// Check — только чтение: в чужую службу ничего не пишем.
func (d Detector) Check(ctx context.Context) Finding {
	if d.foreign != nil {
		if on, err := d.foreign(ctx); err == nil && on {
			return Finding{Kind: OfficialNetBird, Detail: "NetBird"}
		}
	}
	if d.tunnels == nil {
		return Finding{}
	}
	ifaces, err := d.tunnels(d.OwnIface)
	if err != nil {
		return Finding{}
	}
	for _, name := range ifaces {
		if strings.EqualFold(name, d.OwnIface) {
			continue
		}
		if strings.EqualFold(name, netbirdIface) {
			return Finding{Kind: OtherTunnel, Detail: "NetBird"}
		}
		return Finding{Kind: OtherTunnel, Detail: name}
	}
	return Finding{}
}

// Hook — в форме flow.Config.OtherVPN.
func (d Detector) Hook() func(ctx context.Context) (string, bool) {
	return func(ctx context.Context) (string, bool) {
		f := d.Check(ctx)
		return f.Detail, f.Kind != None
	}
}

func officialWithExit(ctx context.Context, addr string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	c, err := daemonclient.Dial(addr)
	if err != nil {
		return false, err
	}
	defer c.Close()
	return exitSelected(ctx, c)
}

// daemon — то, что нужно от чужой службы (реализация — daemonclient.Client).
type daemon interface {
	Status(ctx context.Context) (*proto.StatusResponse, error)
	Networks(ctx context.Context) ([]exitpick.Network, error)
}

// exitSelected — служба подключена и у неё выбран выход (маршрут по умолчанию).
func exitSelected(ctx context.Context, c daemon) (bool, error) {
	st, err := c.Status(ctx)
	if err != nil || st.GetStatus() != "Connected" {
		return false, err
	}
	nets, err := c.Networks(ctx)
	if err != nil {
		return false, err
	}
	for _, n := range nets {
		if n.Selected && exitpick.IsDefaultRoute(n.Range) {
			return true, nil
		}
	}
	return false, nil
}
