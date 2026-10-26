package flow

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/netbirdio/netbird/client/proto"

	"github.com/dsm150/jetbox-desktop/core/exitpick"
)

func TestSnapshotPicksBoxPeer(t *testing.T) {
	since := time.Date(2026, 10, 26, 11, 37, 25, 0, time.UTC)
	st := &proto.StatusResponse{Status: "Connected", FullStatus: &proto.FullStatus{
		ManagementState: &proto.ManagementState{Connected: true},
		Peers: []*proto.PeerState{
			{IP: "100.64.1.1", ConnStatus: "Connected", Networks: []string{"192.168.1.0/24"}},
			{IP: "100.64.239.180", Fqdn: "jetbox-demo.example", ConnStatus: "Connected", Relayed: false,
				BytesRx: 2048, BytesTx: 1024, Latency: durationpb.New(3 * time.Millisecond),
				ConnStatusUpdate: timestamppb.New(since), Networks: []string{"0.0.0.0/0", "::/0"}},
		},
	}}
	s := SnapshotFrom(st, nil, false)
	if !s.Connected || s.BoxIP != "100.64.239.180" || s.Relayed || s.RxBytes != 2048 || s.TxBytes != 1024 ||
		s.Latency != 3*time.Millisecond || !s.Since.Equal(since) || !s.ManagementConnected {
		t.Fatalf("%+v", s)
	}
}

func TestSnapshotRelayedAndNoBox(t *testing.T) {
	st := &proto.StatusResponse{Status: "Connected", FullStatus: &proto.FullStatus{
		Peers: []*proto.PeerState{{IP: "100.64.0.9", ConnStatus: "Connected", Relayed: true, Networks: []string{"0.0.0.0/0"}}},
	}}
	if s := SnapshotFrom(st, nil, false); !s.Relayed || s.BoxIP != "100.64.0.9" {
		t.Fatalf("%+v", s)
	}
	if s := SnapshotFrom(&proto.StatusResponse{Status: "Idle"}, nil, false); s.Connected || s.BoxIP != "" || s.State != "Idle" {
		t.Fatalf("%+v", s)
	}
}

// exitNets — ListNetworks аккаунта ROOT: 8 выходов и сети клиентов; ядро выбрало sel.
func exitNets(sel string) []exitpick.Network {
	nets := []exitpick.Network{{ID: "client-lan-1", Range: "10.1.0.0/24", Selected: true}}
	for _, id := range []string{"exit-1", "exit-2", "exit-3", "exit-4", "exit-5", "exit-6", "exit-7", "exit-8"} {
		nets = append(nets, exitpick.Network{ID: id, Range: "0.0.0.0/0, ::/0", Selected: id == sel})
	}
	return append(nets, exitpick.Network{ID: "client-lan-2", Range: "10.2.0.0/24", Selected: true})
}

func connectedStatus(peers ...*proto.PeerState) *proto.StatusResponse {
	return &proto.StatusResponse{Status: "Connected", FullStatus: &proto.FullStatus{
		ManagementState: &proto.ManagementState{Connected: true}, Peers: peers}}
}

func TestSnapshotExitFromNetworks(t *testing.T) {
	exit := &proto.PeerState{IP: "100.64.0.5", ConnStatus: "Connected", Networks: []string{"0.0.0.0/0"}}
	s := SnapshotFrom(connectedStatus(&proto.PeerState{IP: "100.64.1.1", ConnStatus: "Connected"}, exit), exitNets("exit-3"), false)
	if s.ExitID != "exit-3" || !s.ExitUp || s.BoxIP != "100.64.0.5" || !s.Connected || s.Exits != 8 {
		t.Fatalf("%+v", s)
	}
}

// Выход выбран, но его пира с 0/0 нет (офлайн) — не «Под защитой», но выход известен.
func TestSnapshotExitSelectedButPeerAbsent(t *testing.T) {
	s := SnapshotFrom(connectedStatus(&proto.PeerState{IP: "100.64.1.1", ConnStatus: "Connected"}), exitNets("exit-2"), false)
	if s.ExitID != "exit-2" || s.ExitUp || s.BoxIP != "" || s.Connected || !s.ExitSelected {
		t.Fatalf("%+v", s)
	}
}

// Без выхода («только сети»): служба и management Connected — уже под защитой.
func TestSnapshotNoExitIsProtected(t *testing.T) {
	s := SnapshotFrom(connectedStatus(&proto.PeerState{IP: "100.64.1.1", ConnStatus: "Connected"}), exitNets(""), false)
	if s.ExitID != "" || s.ExitUp || s.ExitSelected || !s.Connected {
		t.Fatalf("%+v", s)
	}
	// management не на связи — не под защитой
	st := connectedStatus()
	st.FullStatus.ManagementState.Connected = false
	if s := SnapshotFrom(st, exitNets(""), false); s.Connected {
		t.Fatalf("%+v", s)
	}
}

// Сети неизвестны (ListNetworks не ответил) — как раньше: нужен поднятый пир с 0/0.
func TestSnapshotNetworksUnknownNeedsExitPeer(t *testing.T) {
	if s := SnapshotFrom(connectedStatus(&proto.PeerState{IP: "100.64.1.1", ConnStatus: "Connected"}), nil, false); s.Connected {
		t.Fatalf("%+v", s)
	}
	exit := &proto.PeerState{IP: "100.64.0.5", ConnStatus: "Connected", Networks: []string{"0.0.0.0/0"}}
	if s := SnapshotFrom(connectedStatus(exit), nil, false); !s.Connected || !s.ExitUp {
		t.Fatalf("%+v", s)
	}
}

// Пир с 0/0 есть, но не Connected — выход выбран и не поднят, даже если список сетей молчит о нём.
func TestSnapshotExitPeerConnecting(t *testing.T) {
	exit := &proto.PeerState{IP: "100.64.0.5", ConnStatus: "Connecting", Networks: []string{"0.0.0.0/0"}}
	s := SnapshotFrom(connectedStatus(exit), exitNets(""), false)
	if s.Connected || s.ExitUp || !s.ExitSelected || s.BoxIP != "100.64.0.5" {
		t.Fatalf("%+v", s)
	}
}

// Порядок пиров в FullStatus не влияет на снимок, даже если пиров с 0/0 несколько.
func TestSnapshotPeerOrderDoesNotMatter(t *testing.T) {
	peers := []*proto.PeerState{
		{IP: "100.64.1.1", ConnStatus: "Connected", Networks: []string{"10.1.0.0/24"}},
		{IP: "100.64.0.9", PubKey: "b", ConnStatus: "Connecting", Networks: []string{"0.0.0.0/0"}},
		{IP: "100.64.0.10", PubKey: "c", ConnStatus: "Connected", Networks: []string{"0.0.0.0/0"}, BytesRx: 10},
		{IP: "100.64.0.2", PubKey: "a", ConnStatus: "Connected", Networks: []string{"::/0", "0.0.0.0/0"}, BytesRx: 2},
	}
	want := SnapshotFrom(connectedStatus(peers...), exitNets("exit-1"), false)
	if want.BoxIP != "100.64.0.2" || want.RxBytes != 2 || !want.ExitUp {
		t.Fatalf("%+v", want)
	}
	perms := [][]int{{3, 2, 1, 0}, {1, 0, 3, 2}, {2, 3, 0, 1}, {0, 2, 1, 3}}
	for _, p := range perms {
		shuffled := make([]*proto.PeerState, len(p))
		for i, j := range p {
			shuffled[i] = peers[j]
		}
		if got := SnapshotFrom(connectedStatus(shuffled...), exitNets("exit-1"), false); got != want {
			t.Fatalf("порядок %v: %+v, ждали %+v", p, got, want)
		}
	}
}

// Карты от management ещё нет: служба и management уже Connected, ListNetworks — [].
// Это не «без выхода»: под защитой только если выходы есть и ни один не выбран
// или в коробке запомнено «без выхода».
func TestSnapshotEmptyNetworksIsPending(t *testing.T) {
	st := connectedStatus(&proto.PeerState{IP: "100.64.1.1", ConnStatus: "Connected"})
	s := SnapshotFrom(st, []exitpick.Network{}, false)
	if s.Connected || !s.Pending || s.ExitSelected {
		t.Fatalf("%+v", s)
	}
	if s := SnapshotFrom(st, nil, false); s.Connected || !s.Pending {
		t.Fatalf("сети неизвестны: %+v", s)
	}
	// в коробке «без выхода» — под защитой сразу
	if s := SnapshotFrom(st, []exitpick.Network{}, true); !s.Connected || s.Pending {
		t.Fatalf("NoExit: %+v", s)
	}
	// только сети клиентов, выходов нет — тоже ждём
	lan := []exitpick.Network{{ID: "lan", Range: "10.0.0.0/8", Selected: true}}
	if s := SnapshotFrom(st, lan, false); s.Connected || !s.Pending {
		t.Fatalf("без выходов: %+v", s)
	}
}
