//go:build windows

package flow

import (
	"os"
	"time"
)

// renameFile на Windows повторяет переименование: файл назначения может быть
// ненадолго занят (антивирус, индексатор, чтение другим процессом).
func renameFile(from, to string) error {
	return retryRename(os.Rename, from, to, 5, 50*time.Millisecond)
}
