//go:build darwin

package main

import (
	"errors"
	"strings"
	"testing"
)

func TestUninstallOsascriptArgs(t *testing.T) {
	a := uninstallOsascriptArgs("alice", "ru")
	if len(a) != 2 || a[0] != "-e" {
		t.Fatalf("аргументы: %q", a)
	}
	s := a[1]
	for _, want := range []string{
		`do shell script "/bin/sh \"/Library/Application Support/JetBox/uninstall.sh\" --user alice"`,
		"with administrator privileges",
		"Введите пароль администратора",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("нет %q в %q", want, s)
		}
	}
	if !strings.Contains(uninstallOsascriptArgs("bob", "en")[1], "Enter an administrator password") {
		t.Error("английский текст запроса")
	}
}

func TestUninstallOsascriptArgsRejectsUnsafeUser(t *testing.T) {
	for _, bad := range []string{"", "a b", `a"; rm -rf /`, "../x", ".hidden", "a$(id)"} {
		s := uninstallOsascriptArgs(bad, "en")[1]
		if strings.Contains(s, "--user") {
			t.Errorf("имя %q попало в команду: %q", bad, s)
		}
	}
}

func TestUninstallErrorCancel(t *testing.T) {
	err := uninstallError("0:42: execution error: User canceled. (-128)\n", errors.New("exit status 1"))
	if !errors.Is(err, errUninstallCanceled) {
		t.Fatalf("отмена не распознана: %v", err)
	}
	err = uninstallError("execution error: something broke (1)\n", errors.New("exit status 1"))
	if errors.Is(err, errUninstallCanceled) || !strings.Contains(err.Error(), "something broke") {
		t.Fatalf("ошибка: %v", err)
	}
}
