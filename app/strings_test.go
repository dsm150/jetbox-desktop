package main

import "testing"

func TestTrayStrings(t *testing.T) {
	if tr("ru", "quit") != "Выход" || tr("en", "quit") != "Quit" {
		t.Fatal(tr("ru", "quit"), tr("en", "quit"))
	}
	if tr("de", "open") != "Open JetBox" {
		t.Fatal("неизвестный язык — английский")
	}
	for k := range trayRU {
		if trayEN[k] == "" {
			t.Errorf("нет английской строки %q", k)
		}
	}
}

func TestNormalizeLang(t *testing.T) {
	for in, want := range map[string]string{"ru-RU": "ru", "ru": "ru", "en-US": "en", "uk-UA": "en", "": "en", "RU_ru.UTF-8": "ru"} {
		if got := normalizeLang(in); got != want {
			t.Errorf("normalizeLang(%q) = %q", in, got)
		}
	}
}
