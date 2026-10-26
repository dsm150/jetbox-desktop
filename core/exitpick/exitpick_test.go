package exitpick

import (
	"reflect"
	"testing"
)

var nets = []Network{
	{ID: "office-lan", Range: "192.168.1.0/24"},
	{ID: "box-ivanov", Range: "0.0.0.0/0, ::/0"},
}

func TestExactMatchWins(t *testing.T) {
	two := append(nets, Network{ID: "other-exit", Range: "0.0.0.0/0"})
	if got := Pick(two, "box-ivanov", Saved{}); got.ID != "box-ivanov" {
		t.Fatal(got)
	}
}

func TestSingleExitIsFallback(t *testing.T) {
	for _, e := range []string{"", "no-such", "BOX-IVANOV"} {
		if got := Pick(nets, e, Saved{}); got.ID != "box-ivanov" {
			t.Errorf("e=%q: %+v", e, got)
		}
	}
}

func TestManyOrNoneAsksHuman(t *testing.T) {
	two := append(nets, Network{ID: "other-exit", Range: "0.0.0.0/0"})
	got := Pick(two, "typo", Saved{})
	if got.ID != "" || !reflect.DeepEqual(got.Candidates, []Candidate{{ID: "box-ivanov"}, {ID: "other-exit"}}) {
		t.Fatal(got)
	}
	if got := Pick(nets[:1], "box", Saved{}); got.ID != "" || len(got.Candidates) != 0 {
		t.Fatal(got)
	}
}

// Кандидаты несут флаг Selected — что ядро включило сейчас.
func TestCandidatesCarrySelected(t *testing.T) {
	three := []Network{{ID: "lan", Range: "10.0.0.0/8", Selected: true},
		{ID: "a", Range: "0.0.0.0/0"}, {ID: "b", Range: "0.0.0.0/0, ::/0", Selected: true}}
	want := []Candidate{{ID: "a"}, {ID: "b", Selected: true}}
	if got := Pick(three, "", Saved{}); !reflect.DeepEqual(got.Candidates, want) {
		t.Fatal(got)
	}
	if got := Exits(three); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

// Запомненный выбор применяется молча, если e нет (или e не найден); e из ссылки сильнее.
func TestSavedChoice(t *testing.T) {
	three := []Network{{ID: "lan", Range: "10.0.0.0/8"}, {ID: "a", Range: "0.0.0.0/0"}, {ID: "b", Range: "0.0.0.0/0"}}
	if got := Pick(three, "", Saved{ID: "b"}); got.ID != "b" || got.NoExit {
		t.Fatal(got)
	}
	if got := Pick(three, "a", Saved{ID: "b"}); got.ID != "a" {
		t.Fatal(got)
	}
	// запомненного выхода больше нет — правила как раньше
	if got := Pick(three, "", Saved{ID: "gone"}); got.ID != "" || len(got.Candidates) != 2 {
		t.Fatal(got)
	}
	got := Pick(three, "", Saved{NoExit: true})
	if got.ID != "" || !got.NoExit || !reflect.DeepEqual(got.Exits, []string{"a", "b"}) || got.Candidates != nil {
		t.Fatal(got)
	}
	if got := Pick(three, "b", Saved{NoExit: true}); got.ID != "b" || got.NoExit {
		t.Fatal(got)
	}
}
