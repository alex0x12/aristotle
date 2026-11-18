// Package keys хранит ключи API, введённые в веб-интерфейсе, в
// ~/.config/aristotle/keys.json с правами 0600. Переменная окружения
// профиля всё равно важнее — это решает config.Profile.APIKey.
package keys

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/alex0x12/aristotle/internal/fsutil"
)

type Store struct {
	Path string
}

type file struct {
	Profiles map[string]string `json:"profiles"`
}

func (s Store) load() (file, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return file{Profiles: map[string]string{}}, nil
	}
	if err != nil {
		return file{}, fmt.Errorf("ключи %s: %w", s.Path, err)
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return file{}, fmt.Errorf("ключи %s: %w", s.Path, err)
	}
	if f.Profiles == nil {
		f.Profiles = map[string]string{}
	}
	return f, nil
}

func (s Store) save(f file) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(s.Path, append(data, '\n'), 0o600)
}

// Get — ключ профиля; пусто, если не сохранён.
func (s Store) Get(profile string) (string, error) {
	f, err := s.load()
	if err != nil {
		return "", err
	}
	return f.Profiles[profile], nil
}

// Set сохраняет ключ профиля; пробелы по краям — след копирования, срезаются.
func (s Store) Set(profile, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("пустой ключ")
	}
	f, err := s.load()
	if err != nil {
		return err
	}
	f.Profiles[profile] = key
	return s.save(f)
}

// Delete убирает ключ профиля; отсутствующий — не ошибка.
func (s Store) Delete(profile string) error {
	f, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := f.Profiles[profile]; !ok {
		return nil
	}
	delete(f.Profiles, profile)
	return s.save(f)
}

// Hint — последние четыре символа ключа для показа в интерфейсе. Ключ из
// четырёх символов и короче не показывается вовсе.
func Hint(key string) string {
	r := []rune(key)
	if len(r) <= 4 {
		return "…"
	}
	return "…" + string(r[len(r)-4:])
}
