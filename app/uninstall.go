package main

import "errors"

// errUninstallCanceled — пользователь отказался в системном окне (пароль администратора / UAC):
// окно молча возвращается в «Настройки», не как ошибка.
var errUninstallCanceled = errors.New("uninstall canceled")
