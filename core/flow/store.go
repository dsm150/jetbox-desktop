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
	// AutoConnect — «Подключаться автоматически»: служба включает защиту при старте,
	// если человек её не выключал. Нет в файле (≤1.0.3) — включено.
	AutoConnect bool `json:"auto_connect"`
	// KeyFingerprint — отпечаток setup key, которым компьютер вошёл
	// (jetboxlink.Payload.KeyFingerprint); сам ключ не хранится. Пусто — запись
	// сделана до 1.0.5: «та же ссылка» тогда решает Identity.
	KeyFingerprint string `json:"key_fingerprint,omitempty"`
	// ProfileID и ProfileUser — профиль службы, в котором заведена эта регистрация
	// (ID из GetActiveProfile и его пользователь). Пишутся при каждом сохранении
	// коробки. Пусто — запись до 1.0.5, где профили были выключены: default.
	ProfileID   string `json:"profile_id,omitempty"`
	ProfileUser string `json:"profile_user,omitempty"`
}

// Replace — незаконченная замена подключения (replace.json рядом с box.json).
// Пишется перед AddProfile, удаляется после сохранения коробки или после отката:
// осталась — замену прервали (выход, сбой питания), и Flow доводит откат.
type Replace struct {
	OldID   string `json:"old_id"`             // профиль, активный до замены
	OldUser string `json:"old_user,omitempty"` // его пользователь
	NewName string `json:"new_name"`           // имя нового профиля (jetbox-<unixtime>)
	NewID   string `json:"new_id,omitempty"`   // ID нового, когда служба его назвала
	NewUser string `json:"new_user,omitempty"` // пользователь нового
	// WasOn — защита была включена до замены: откат (и сверка, доводящая его) поднимает
	// прежнее подключение. Нет в файле (1.0.5) — не поднимать.
	WasOn bool      `json:"was_on,omitempty"`
	At    time.Time `json:"at"`
}

// Store хранит Box в JSON-файле в каталоге настроек приложения (и Replace рядом).
// Безопасен для одновременных вызовов из одного процесса: Load читает под
// RLock, Save и Clear пишут под Lock (на Windows чтение во время
// переименования иначе получает отказ доступа).
type Store struct {
	mu   sync.RWMutex
	path string
	// saveErr — только для тестов: Save отвечает этой ошибкой (сбой записи box.json).
	saveErr error
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
	b := Box{AutoConnect: true} // поля нет в файле — значение по умолчанию
	if err := json.Unmarshal(raw, &b); err != nil {
		// Испорченный или пустой файл (сбой питания при старой записи и т. п.) —
		// считаем, что коробки нет: человек откроет ссылку ещё раз.
		return Box{}, false, nil
	}
	return b, true, nil
}

// Save пишет атомарно: во временный файл рядом, Sync и переименованием.
// При любой ошибке временный файл удаляется.
func (s *Store) Save(b Box) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	return writeJSON(s.path, b)
}

func writeJSON(path string, v any) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(v, "", "  ")
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
	return renameFile(tmp, path)
}

func (s *Store) replacePath() string { return filepath.Join(filepath.Dir(s.path), "replace.json") }

// LoadReplace читает запись о незаконченной замене; ok=false — её нет. Испорченная
// запись — как будто её нет: без неё откат доводит сверка профиля с коробкой.
func (s *Store) LoadReplace() (Replace, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, err := os.ReadFile(s.replacePath())
	if err != nil {
		return Replace{}, false
	}
	var r Replace
	if json.Unmarshal(raw, &r) != nil || r.OldID == "" {
		return Replace{}, false
	}
	return r, true
}

// SaveReplace пишет запись о замене атомарно.
func (s *Store) SaveReplace(r Replace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.replacePath(), r)
}

// ClearReplace удаляет запись о замене.
func (s *Store) ClearReplace() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.replacePath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
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
