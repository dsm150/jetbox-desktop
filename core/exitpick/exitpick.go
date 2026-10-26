// Package exitpick выбирает выход коробки по LINK-CONTRACT §4.3.
package exitpick

import (
	"net/netip"
	"strings"
)

// Network — то, что демон отдаёт в ListNetworks (ID — Network ID маршрута).
type Network struct {
	ID       string
	Range    string
	Selected bool
}

// Candidate — выход для экрана «Выберите выход».
type Candidate struct {
	ID       string
	Selected bool // ядро включило его сейчас
}

// Saved — выбор человека, запомненный в коробке: выход или явное «без выхода».
type Saved struct {
	ID     string
	NoExit bool
}

// Choice — исход выбора.
type Choice struct {
	// ID выбранного выхода; пусто, если выбирать должен человек (или выбрано «без выхода»).
	ID string
	// NoExit — запомнено «без выхода»: снять все выходы (Exits).
	NoExit bool
	// Exits — ID всех выходов в списке (для NoExit).
	Exits []string
	// Candidates — все выходы, если выбрать молча нельзя (экран «Выберите выход»).
	Candidates []Candidate
}

// Pick: (1) `e` есть в списке — молча он; (2) запомненный выход есть в списке — он,
// запомнено «без выхода» — NoExit; (3) ровно один выход — он; (4) ноль или больше
// одного — спросить человека. Сравнение `e` точное.
//
// `e` сильнее запомненного выбора НАМЕРЕННО: это контракт ссылки (LINK-CONTRACT
// §4.3), `e` — выход самой коробки клиента. Повторно открытая ссылка с `e`
// возвращает выход коробки, даже если раньше человек выбрал другой или «без выхода».
func Pick(networks []Network, e string, saved Saved) Choice {
	exits := Exits(networks)
	ids := make([]string, 0, len(exits))
	for _, c := range exits {
		ids = append(ids, c.ID)
	}
	for _, want := range []string{e, saved.ID} {
		if want == "" {
			continue
		}
		for _, id := range ids {
			if id == want {
				return Choice{ID: id, Exits: ids}
			}
		}
	}
	if saved.NoExit {
		return Choice{NoExit: true, Exits: ids}
	}
	if len(exits) == 1 {
		return Choice{ID: ids[0], Exits: ids}
	}
	return Choice{Exits: ids, Candidates: exits}
}

// Exits — выходы (сети с маршрутом по умолчанию) в порядке списка, с пометкой включённого.
func Exits(networks []Network) []Candidate {
	var out []Candidate
	for _, n := range networks {
		if IsDefaultRoute(n.Range) {
			out = append(out, Candidate{ID: n.ID, Selected: n.Selected})
		}
	}
	return out
}

// IsDefaultRoute — диапазон содержит маршрут по умолчанию; демон может склеить
// пару v4+v6 через запятую («0.0.0.0/0, ::/0»).
func IsDefaultRoute(r string) bool {
	for _, part := range strings.Split(r, ",") {
		p, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err == nil && p.Bits() == 0 {
			return true
		}
	}
	return false
}
