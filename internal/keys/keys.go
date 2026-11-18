// Package keys хранит единственный ключ API в ~/.config/aristotle/keys.json
// ({"key": "…"}, права 0600). Другого источника ключа нет: переменные
// окружения не читаются нигде.
package keys

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alex0x12/aristotle/internal/fsutil"
)

// MaxKeyRunes — предел длины ключа; настоящие ключи на порядок короче.
const MaxKeyRunes = 4096

// Ошибки проверки ключа. Текст ключа в них не повторяется — они уходят в
// ответы сервера.
var (
	ErrEmpty    = errors.New("ключ пустой")
	ErrTooLong  = fmt.Errorf("ключ длиннее %d знаков", MaxKeyRunes)
	ErrBadChars = errors.New("в ключе пробел, перевод строки или управляющий знак — похоже на неудачную вставку")
)

// Normalize срезает пробелы по краям (след копирования) и проверяет ключ.
func Normalize(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	switch {
	case key == "":
		return "", ErrEmpty
	case utf8.RuneCountInString(key) > MaxKeyRunes:
		return "", ErrTooLong
	case strings.ContainsFunc(key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }):
		return "", ErrBadChars
	}
	return key, nil
}

// UnreadableText — то, что видит пользователь, когда keys.json не читается.
const UnreadableText = "keys.json не читается"

// UnreadableError — keys.json есть, но не разбирается (или не читается). Error()
// отдаёт только фиксированный текст: он идёт в ответы, а в файле лежит ключ.
// Причина (Unwrap) тоже не содержит ни его знаков, ни текста разбора — только
// путь и класс поломки; её можно писать в журнал сервера.
type UnreadableError struct{ cause error }

func (e *UnreadableError) Error() string { return UnreadableText }
func (e *UnreadableError) Unwrap() error { return e.cause }

func unreadable(path, reason string) error {
	return &UnreadableError{cause: fmt.Errorf("%s: %s", path, reason)}
}

type file struct {
	Key string `json:"key"`
}

// Store — файл ключа. Замка нет: один процесс сериализует записи сам (веб —
// своим keysMu), а замена файла атомарна для остальных.
type Store struct {
	Path string
}

// Get — ключ; пусто и nil, если файла нет или в нём нет ключа. Старый формат
// ({"profiles": …}) — допустимый JSON без поля key, то есть «ключа нет»: миграции
// нет. Всё, что не разбирается, — *UnreadableError.
func (s Store) Get() (string, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		var pe *os.PathError
		if errors.As(err, &pe) {
			return "", unreadable(s.Path, "не читается: "+pe.Err.Error())
		}
		return "", unreadable(s.Path, "не читается")
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		var se *json.SyntaxError
		var ute *json.UnmarshalTypeError
		switch {
		case errors.As(err, &ute):
			return "", unreadable(s.Path, "поле "+ute.Field+" не того типа")
		case errors.As(err, &se):
			return "", unreadable(s.Path, fmt.Sprintf("не JSON (ошибка на байте %d)", se.Offset))
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			return "", unreadable(s.Path, "файл пуст или оборван")
		default:
			return "", unreadable(s.Path, "не разобран")
		}
	}
	key := strings.TrimSpace(f.Key)
	if key == "" {
		return "", nil
	}
	if _, err := Normalize(key); err != nil {
		return "", unreadable(s.Path, "ключ в файле недопустим (пробел, управляющий знак или слишком длинный)")
	}
	return key, nil
}

// Set сохраняет ключ. Испорченный файл не перезаписывается: возвращается его
// *UnreadableError, файл остаётся как был.
func (s Store) Set(raw string) error {
	key, err := Normalize(raw)
	if err != nil {
		return err
	}
	if _, err := s.Get(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(file{Key: key}, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(s.Path, append(data, '\n'), 0o600)
}

// Delete убирает ключ вместе с файлом; отсутствующий — не ошибка. Испорченный
// файл не трогается.
func (s Store) Delete() error {
	if _, err := s.Get(); err != nil {
		return err
	}
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
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
