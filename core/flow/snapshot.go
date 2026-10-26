package flow

import (
	"net/netip"
	"time"

	"github.com/netbirdio/netbird/client/proto"

	"github.com/dsm150/jetbox-desktop/core/exitpick"
)

// Snapshot — то, что показывает панель дома.
type Snapshot struct {
	State string // как у службы: Idle, Connecting, Connected, NeedsLogin…
	// Connected — «Под защитой»: служба Connected, management Connected и либо
	// пир выхода поднят, либо выхода нет наверняка (см. Pending).
	Connected           bool
	ManagementConnected bool
	// ExitID — выбранная сеть с маршрутом по умолчанию (ListNetworks); "" — выхода
	// нет или сети неизвестны.
	ExitID string
	// ExitSelected — выход выбран: по списку сетей или по пиру с 0/0 в статусе.
	ExitSelected bool
	// ExitUp — пир выхода (с 0/0 в networks) найден и Connected.
	ExitUp bool
	// Pending — выхода не видно, но и «без выхода» не доказано: сети неизвестны
	// или выходов в списке нет (карта от management ещё не пришла — ядро ставит
	// management и статус Connected раньше первого Sync с маршрутами), а в
	// коробке не запомнено «без выхода». Сколько так ждать — решает вызывающий.
	Pending bool
	// Exits — сколько выходов в списке сетей (для «Сменить»).
	Exits            int
	BoxIP            string        // адрес пира выхода в сети JetBox — для «до дома»
	Relayed          bool          // через ретранслятор (иначе напрямую)
	Latency          time.Duration // задержка WireGuard от ядра (бывает 0)
	Since            time.Time     // с какого момента пир выхода в этом состоянии
	RxBytes, TxBytes int64
}

// SnapshotFrom собирает снимок из ответов Status и ListNetworks. nets == nil —
// сети неизвестны (служба не ответила); пустой список — сетей (пока) нет.
// noExit — в коробке запомнено «без выхода — только сети».
//
// «Без выхода» = под защитой, только если выходы в списке есть и ни один не
// выбран или запомнено noExit. Иначе (Pending) — ждём карту.
//
// «0.0.0.0/0» в networks пира ядро пишет только активному маршрутизатору
// выбранного выхода и только когда маршрут поднят (routemanager/client
// addAllowedIPs → AddPeerStateRoute). Если таких пиров вдруг несколько, берём
// детерминированно: сначала Connected, затем меньший IP, затем ключ.
func SnapshotFrom(st *proto.StatusResponse, nets []exitpick.Network, noExit bool) Snapshot {
	s := Snapshot{State: st.GetStatus()}
	fs := st.GetFullStatus()
	s.ManagementConnected = fs.GetManagementState().GetConnected()
	for _, n := range nets {
		if !exitpick.IsDefaultRoute(n.Range) {
			continue
		}
		s.Exits++
		if n.Selected && (s.ExitID == "" || n.ID < s.ExitID) {
			s.ExitID = n.ID
		}
	}
	var exit *proto.PeerState
	for _, p := range fs.GetPeers() {
		if hasDefaultRoute(p.GetNetworks()) && (exit == nil || betterExitPeer(p, exit)) {
			exit = p
		}
	}
	if exit != nil {
		s.BoxIP = exit.GetIP()
		s.Relayed = exit.GetRelayed()
		s.Latency = exit.GetLatency().AsDuration()
		if exit.GetConnStatusUpdate() != nil {
			s.Since = exit.GetConnStatusUpdate().AsTime()
		}
		s.RxBytes, s.TxBytes = exit.GetBytesRx(), exit.GetBytesTx()
		s.ExitUp = exit.GetConnStatus() == "Connected"
	}
	s.ExitSelected = s.ExitID != "" || exit != nil
	s.Pending = !s.ExitSelected && !noExit && s.Exits == 0
	s.Connected = s.State == "Connected" && s.ManagementConnected && (s.ExitUp || (!s.ExitSelected && !s.Pending))
	return s
}

// betterExitPeer — a предпочтительнее b: Connected, затем меньший IP, затем ключ.
func betterExitPeer(a, b *proto.PeerState) bool {
	ac, bc := a.GetConnStatus() == "Connected", b.GetConnStatus() == "Connected"
	if ac != bc {
		return ac
	}
	if c := compareIP(a.GetIP(), b.GetIP()); c != 0 {
		return c < 0
	}
	return a.GetPubKey() < b.GetPubKey()
}

// compareIP сравнивает адреса как адреса (100.64.0.10 > 100.64.0.9), нечитаемые — строкой.
func compareIP(a, b string) int {
	pa, ea := netip.ParseAddr(a)
	pb, eb := netip.ParseAddr(b)
	if ea == nil && eb == nil {
		return pa.Compare(pb)
	}
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func hasDefaultRoute(nets []string) bool {
	for _, n := range nets {
		if exitpick.IsDefaultRoute(n) {
			return true
		}
	}
	return false
}
