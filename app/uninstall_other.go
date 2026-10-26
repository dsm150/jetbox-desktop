//go:build !windows && !darwin

package main

import "errors"

func platformUninstall() error {
	return errors.New("удаление на этой ОС не поддерживается")
}
