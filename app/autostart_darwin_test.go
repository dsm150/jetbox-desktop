//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchdDisabled(t *testing.T) {
	for out, want := range map[string]bool{
		"\t\"net.jet-net.jetbox.desktop\" => disabled\n":        true,
		"\t\"net.jet-net.jetbox.desktop\" => true\n":            true,
		"\t\"net.jet-net.jetbox.desktop\" => enabled\n":         false,
		"\t\"net.jet-net.jetbox.desktop.helper\" => disabled\n": false,
		"\t\"com.docker.helper\" => disabled\n":                 false,
		"":                                                      false,
	} {
		if got := launchdDisabled(out, launchAgentLabel); got != want {
			t.Errorf("%q: got %v, want %v", out, got, want)
		}
	}
}

func TestLaunchdAutostart(t *testing.T) {
	dir := t.TempDir()
	var calls []string
	disabled := false
	a := launchdAutostart{
		plist: filepath.Join(dir, "agent.plist"),
		uid:   501,
		run: func(args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(args, " "))
			switch args[0] {
			case "disable":
				disabled = true
			case "enable":
				disabled = false
			case "print-disabled":
				if disabled {
					return []byte("\t\"net.jet-net.jetbox.desktop\" => disabled\n"), nil
				}
			}
			return nil, nil
		},
	}
	if a.autostartSupported() {
		t.Fatal("без LaunchAgent (запуск не из .pkg) строки в «Настройках» нет")
	}
	if err := os.WriteFile(a.plist, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !a.autostartSupported() {
		t.Fatal("LaunchAgent есть — строка есть")
	}
	if on, err := a.autostartEnabled(); err != nil || !on {
		t.Fatalf("по умолчанию включён: %v %v", on, err)
	}
	if err := a.setAutostart(false); err != nil {
		t.Fatal(err)
	}
	if on, _ := a.autostartEnabled(); on {
		t.Fatal("после выключения — выключен")
	}
	if err := a.setAutostart(true); err != nil {
		t.Fatal(err)
	}
	want := "disable gui/501/net.jet-net.jetbox.desktop"
	found := false
	for _, c := range calls {
		found = found || c == want
	}
	if !found {
		t.Fatalf("ждали %q среди %q", want, calls)
	}
}
