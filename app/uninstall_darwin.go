//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"os/user"
	"regexp"
	"strings"
)

// Удаление на macOS — packaging/macos/uninstall.sh от root через стандартный запрос пароля
// администратора (osascript … with administrator privileges). Не через службу: в её gRPC такого
// вызова нет, а «удали от root» на общем канале мог бы вызвать любой процесс пользователя.
// Скрипт лежит в каталоге root — подменить его без пароля нельзя. Он сам снимает компьютер
// с учёта, закрывает приложение и удаляет всё, включая данные этого пользователя.
const uninstallScript = "/Library/Application Support/JetBox/uninstall.sh"

// Имя учётной записи уходит в командную строку — только безопасные символы.
var safeUserName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func platformUninstall() error {
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	out, err := exec.Command("/usr/bin/osascript", uninstallOsascriptArgs(name, systemLang())...).CombinedOutput()
	if err != nil {
		return uninstallError(string(out), err)
	}
	return nil
}

// uninstallOsascriptArgs — аргументы osascript (без оболочки). Плохое имя — без --user:
// скрипт тогда чистит данные всех пользователей.
func uninstallOsascriptArgs(userName, lang string) []string {
	cmd := `/bin/sh \"` + uninstallScript + `\"`
	if safeUserName.MatchString(userName) && !strings.HasPrefix(userName, ".") {
		cmd += " --user " + userName
	}
	prompt := "JetBox is being removed from this Mac. Enter an administrator password."
	if lang == "ru" {
		prompt = "JetBox удаляется с этого Mac. Введите пароль администратора."
	}
	return []string{"-e", `do shell script "` + cmd + `" with administrator privileges with prompt "` + prompt + `"`}
}

// uninstallError — «Отмена» в окне пароля (ошибка AppleScript -128) не считается ошибкой.
func uninstallError(out string, err error) error {
	if strings.Contains(out, "(-128)") {
		return errUninstallCanceled
	}
	if msg := strings.TrimSpace(out); msg != "" {
		return fmt.Errorf("uninstall: %s", msg)
	}
	return fmt.Errorf("uninstall: %w", err)
}
