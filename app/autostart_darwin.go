//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
)

// launchdAutostart — macOS: автозапуск при входе делает общий LaunchAgent из .pkg
// (/Library/LaunchAgents/net.jet-net.jetbox.desktop.plist, --autostart). Выключить его одному
// пользователю без прав администратора — `launchctl disable gui/<uid>/<метка>`: launchd помнит
// выбор сам, и postinstall при обновлении его не трогает (packaging/macos/scripts/postinstall).
type launchdAutostart struct {
	plist string
	uid   int
	run   func(args ...string) ([]byte, error) // launchctl (подменяется в тестах)
}

const launchAgentLabel = "net.jet-net.jetbox.desktop"

func newAutostart() autostarter {
	return launchdAutostart{
		plist: "/Library/LaunchAgents/" + launchAgentLabel + ".plist",
		uid:   os.Getuid(),
		run: func(args ...string) ([]byte, error) {
			return exec.Command("/bin/launchctl", args...).CombinedOutput()
		},
	}
}

// autostartSupported — строка в «Настройках» есть, только если JetBox поставлен .pkg (есть LaunchAgent).
func (a launchdAutostart) autostartSupported() bool {
	_, err := os.Stat(a.plist)
	return err == nil
}

func (a launchdAutostart) domain() string { return "gui/" + strconv.Itoa(a.uid) }

func (a launchdAutostart) autostartEnabled() (bool, error) {
	out, err := a.run("print-disabled", a.domain())
	if err != nil {
		return false, fmt.Errorf("launchctl print-disabled: %v: %s", err, out)
	}
	return !launchdDisabled(string(out), launchAgentLabel), nil
}

func (a launchdAutostart) setAutostart(on bool) error {
	verb := "disable"
	if on {
		verb = "enable"
	}
	if out, err := a.run(verb, a.domain()+"/"+launchAgentLabel); err != nil {
		return fmt.Errorf("launchctl %s: %v: %s", verb, err, out)
	}
	return nil
}

// launchdDisabled — в выводе `launchctl print-disabled` метка выключена: macOS 13+ пишет
// «"метка" => disabled», старые версии — «=> true».
func launchdDisabled(out, label string) bool {
	return regexp.MustCompile(`"` + regexp.QuoteMeta(label) + `" => (disabled|true)\b`).MatchString(out)
}
