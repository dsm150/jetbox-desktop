package flow

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Box — коробка, к которой подключено устройство. Ключ сюда не попадает никогда.
type Box struct {
	Identity      string `json:"identity"`
	Name          string `json:"name"`
	ManagementURL string `json:"management_url"`
	ExitNode      string `json:"exit_node"`
	// NoExit — человек выбрал «без выхода — только сети»; тогда ExitNode пуст.
	NoExit       bool      `json:"no_exit,omitempty"`
	RegisteredAt time.Time `json:"registered_at"`
}

// Store хранит Box в JSON-файле в каталоге настроек приложения.
// Безопасен для одновременных вызовов из одного процесса: Load читает под
// RLock, Save и Clear пишут под Lock (на Windows чтение во время
// переименования иначе получает отказ доступа).
type Store struct {
	mu   sync.RWMutex
	path string
}

// NewStore — хранилище по пути к файлу (каталоги создаются при записи).
func NewStore(path string) *Store { return &Store{path: path} }

// Load читает коробку; ok=false, если её нет.
func (s *Store) Load() (Box, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return Box{}, false, nil
	}
	if err != nil {
		return Box{}, false, err
	}
	var b Box
	if err := json.Unmarshal(raw, &b); err != nil {
		// Испорченный или пустой файл (сбой питания при старой записи и т. п.) —
		// считаем, что коробки нет: человек откроет ссылку ещё раз.
		return Box{}, false, nil
	}
	return b, true, nil
}

// Save пишет атомарно: во временный файл рядом, Sync и переименованием.
// При любой ошибке временный файл удаляется.
func (s *Store) Save(b Box) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "box-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(raw); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return renameFile(tmp, s.path)
}

// retryRename зовёт rename и при ошибке повторяет до retries раз с паузой
// delay; возвращает последнюю ошибку. Нужно на Windows: антивирус, индексатор
// или чужое чтение ненадолго держат файл, и MoveFileEx получает отказ доступа.
func retryRename(rename func(from, to string) error, from, to string, retries int, delay time.Duration) error {
	err := rename(from, to)
	for i := 0; err != nil && i < retries; i++ {
		time.Sleep(delay)
		err = rename(from, to)
	}
	return err
}

// Clear забывает коробку.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
