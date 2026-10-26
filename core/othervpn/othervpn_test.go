package othervpn

import (
	"context"
	"errors"
	"testing"

	"github.com/netbirdio/netbird/client/proto"

	"github.com/dsm150/jetbox-desktop/core/exitpick"
)

func noTunnels(string) ([]string, error) { return nil, nil }

func TestOfficialNetBirdWithExit(t *testing.T) {
	d := Detector{
		OwnIface: "JetBox",
		foreign:  func(context.Context) (bool, error) { return true, nil },
		tunnels:  noTunnels,
	}
	if f := d.Check(context.Background()); f.Kind != OfficialNetBird {
		t.Fatal(f)
	}
}

func TestForeignTunnel(t *testing.T) {
	d := Detector{
		OwnIface: "JetBox",
		foreign:  func(context.Context) (bool, error) { return false, errors.New("нет канала") },
		tunnels:  func(string) ([]string, error) { return []string{"JetBox", "OpenVPN TAP"}, nil },
	}
	f := d.Check(context.Background())
	if f.Kind != OtherTunnel || f.Detail != "OpenVPN TAP" {
		t.Fatal(f)
	}
}

func TestOwnTunnelIsFine(t *testing.T) {
	d := Detector{
		OwnIface: "utun120",
		foreign:  func(context.Context) (bool, error) { return false, nil },
		tunnels:  func(string) ([]string, error) { return []string{"utun120"}, nil },
	}
	if f := d.Check(context.Background()); f.Kind != None {
		t.Fatal(f)
	}
}

func TestOwnTunnelCaseInsensitive(t *testing.T) {
	d := Detector{OwnIface: "JetBox", tunnels: func(string) ([]string, error) { return []string{"jetbox"}, nil }}
	if f := d.Check(context.Background()); f.Kind != None {
		t.Fatal(f)
	}
}

func TestOwnIfacePassedToTunnels(t *testing.T) {
	var got string
	d := Detector{OwnIface: "JetBox", tunnels: func(own string) ([]string, error) { got = own; return nil, nil }}
	d.Check(context.Background())
	if got != "JetBox" {
		t.Fatal(got)
	}
}

// wt0 — интерфейс официального NetBird по умолчанию: человеку показываем «NetBird».
func TestNetBirdTunnelIsNamedForHumans(t *testing.T) {
	d := Detector{OwnIface: "JetBox", tunnels: func(string) ([]string, error) { return []string{"wt0"}, nil }}
	if f := d.Check(context.Background()); f.Kind != OtherTunnel || f.Detail != "NetBird" {
		t.Fatal(f)
	}
}

func TestZeroDetectorIsSafe(t *testing.T) {
	if f := (Detector{}).Check(context.Background()); f.Kind != None {
		t.Fatal(f)
	}
	d := Detector{OwnIface: "JetBox", tunnels: func(string) ([]string, error) { return []string{"tun0"}, nil }}
	if f := d.Check(context.Background()); f.Kind != OtherTunnel || f.Detail != "tun0" {
		t.Fatal(f)
	}
}

func TestAsFlowHook(t *testing.T) {
	d := Detector{OwnIface: "JetBox",
		foreign: func(context.Context) (bool, error) { return true, nil },
		tunnels: noTunnels}
	detail, found := d.Hook()(context.Background())
	if !found || detail != "NetBird" {
		t.Fatal(detail, found)
	}
}

func TestAsFlowHookNothingFound(t *testing.T) {
	d := Detector{OwnIface: "JetBox",
		foreign: func(context.Context) (bool, error) { return false, nil },
		tunnels: noTunnels}
	detail, found := d.Hook()(context.Background())
	if found || detail != "" {
		t.Fatal(detail, found)
	}
}

type fakeDaemon struct {
	status    string
	statusErr error
	nets      []exitpick.Network
	netsErr   error
}

func (f fakeDaemon) Status(context.Context) (*proto.StatusResponse, error) {
	return &proto.StatusResponse{Status: f.status}, f.statusErr
}
func (f fakeDaemon) Networks(context.Context) ([]exitpick.Network, error) { return f.nets, f.netsErr }

func TestExitSelected(t *testing.T) {
	exit := exitpick.Network{ID: "exit", Range: "0.0.0.0/0, ::/0", Selected: true}
	lan := exitpick.Network{ID: "lan", Range: "192.168.1.0/24", Selected: true}
	idleExit := exitpick.Network{ID: "exit", Range: "0.0.0.0/0", Selected: false}
	for _, tc := range []struct {
		name    string
		d       fakeDaemon
		want    bool
		wantErr bool
	}{
		{"подключён с выбранным выходом", fakeDaemon{status: "Connected", nets: []exitpick.Network{lan, exit}}, true, false},
		{"подключён без выхода", fakeDaemon{status: "Connected", nets: []exitpick.Network{lan, idleExit}}, false, false},
		{"не подключён", fakeDaemon{status: "Idle", nets: []exitpick.Network{exit}}, false, false},
		{"ошибка статуса", fakeDaemon{statusErr: errors.New("x")}, false, true},
		{"ошибка сетей", fakeDaemon{status: "Connected", netsErr: errors.New("x")}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := exitSelected(context.Background(), tc.d)
			if got != tc.want || (err != nil) != tc.wantErr {
				t.Fatal(got, err)
			}
		})
	}
}
