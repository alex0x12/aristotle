// Package config — настройки подключения к модели: один OpenAI-совместимый
// адрес, одна модель и бюджет контекста. Профилей нет: Claude и другие модели
// доступны через любой OpenAI-совместимый шлюз (например, OpenRouter), а ключ
// лежит отдельно, в keys.json (пакет keys).
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alex0x12/aristotle/internal/fsutil"
)

// Умолчания. DefaultCorpusK — не настройка: сколько глав возвращает поиск,
// если модель не попросила своё k.
const (
	DefaultBaseURL       = "https://api.deepseek.com/v1"
	DefaultModel         = "deepseek-flash"
	DefaultContextBudget = 128000
	DefaultCorpusK       = 6
)

// Пределы значений. Верхние нужны не как мнение о «разумном», а чтобы опечатка
// (лишний нуль) не стала настройкой.
const (
	MaxContextBudget = 10_000_000
	maxModelRunes    = 200
	maxURLBytes      = 2048
)

// Config — всё, что настраивается. Ключа здесь нет и не будет: config.json
// безопасно показывать и копировать.
type Config struct {
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	// ContextBudget — потолок входа запроса в токенах; 0 — без урезания.
	ContextBudget int `json:"context_budget"`
}

// Default — конфиг без файла.
func Default() Config {
	return Config{BaseURL: DefaultBaseURL, Model: DefaultModel, ContextBudget: DefaultContextBudget}
}

// Normalized — конфиг с обрезанными пробелами по краям адреса и модели: след
// копирования из чата. Всё остальное, включая ошибки, остаётся как есть.
func (c Config) Normalized() Config {
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	c.Model = strings.TrimSpace(c.Model)
	return c
}

// fieldErr — ошибка с названием поля, по-русски. Присланное значение в ней не
// повторяется: в адресе мог оказаться пароль, а в модели — вставленный ключ.
func fieldErr(field, format string, a ...any) error {
	return fmt.Errorf("%s: %s", field, fmt.Sprintf(format, a...))
}

func hasSpaceOrControl(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// Validate проверяет значения; его зовут Load, Save и веб при сохранении
// настроек, так что файл и экран принимают одно и то же.
func (c Config) Validate() error {
	if c.ContextBudget < 0 || c.ContextBudget > MaxContextBudget {
		return fieldErr("context_budget", "от 0 до %d (0 — без урезания)", MaxContextBudget)
	}
	model := strings.TrimSpace(c.Model)
	if model == "" || utf8.RuneCountInString(model) > maxModelRunes ||
		strings.ContainsFunc(model, unicode.IsControl) {
		return fieldErr("model", "название модели обязательно, без управляющих знаков, до %d знаков", maxModelRunes)
	}
	return ValidateBaseURL(c.BaseURL)
}

// ValidateBaseURL — адрес API: только http и https, хост обязателен, логина и
// пароля в адресе нет (они попали бы в сообщения об ошибках и журнал).
func ValidateBaseURL(raw string) error {
	if raw == "" {
		return fieldErr("base_url", "адрес API обязателен")
	}
	if len(raw) > maxURLBytes || hasSpaceOrControl(raw) {
		return fieldErr("base_url", "адрес без пробелов и управляющих знаков, до %d знаков", maxURLBytes)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return fieldErr("base_url", "адрес должен начинаться с http:// или https:// и называть хост")
	}
	if u.User != nil {
		return fieldErr("base_url", "логин и пароль в адресе не допускаются")
	}
	return nil
}

// Load читает config.json поверх умолчаний: опущенное поле остаётся
// умолчанием. Нет файла — не ошибка. Неизвестное поле — ошибка с его именем:
// так старый формат (профили, corpus_k) или файл из будущей версии не
// принимается молча за пустой и не теряется при следующем сохранении
// (вызывающая сторона решает, что делать: веб показывает предупреждение и
// делает копию, командная строка идёт на умолчаниях). Вместе с ошибкой
// возвращаются умолчания.
func Load(path string) (Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return Default(), fmt.Errorf("конфиг %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Default(), fmt.Errorf("конфиг %s: %w", path, decodeError(err))
	}
	if _, err := dec.Token(); err != io.EOF {
		return Default(), fmt.Errorf("конфиг %s: после JSON есть лишнее", path)
	}
	c = c.Normalized()
	if err := c.Validate(); err != nil {
		return Default(), fmt.Errorf("конфиг %s: %w", path, err)
	}
	return c, nil
}

// decodeError переводит ошибки разбора на русский и называет поле, на котором
// разбор остановился.
func decodeError(err error) error {
	var ute *json.UnmarshalTypeError
	var se *json.SyntaxError
	switch {
	case errors.As(err, &ute):
		return fmt.Errorf("поле %s: значение не того типа", ute.Field)
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return fmt.Errorf("неизвестное поле %s (старый формат конфигурации или файл из другой версии)",
			strings.TrimPrefix(err.Error(), "json: unknown field "))
	case errors.As(err, &se):
		return fmt.Errorf("не JSON (ошибка на байте %d)", se.Offset)
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return errors.New("файл пуст или оборван")
	default:
		return err
	}
}

// Save проверяет конфиг и пишет его в path целиком, всеми тремя полями, даже
// равными умолчаниям: файл читается сам по себе и не меняется вместе с
// умолчаниями программы. Недопустимый конфиг не пишется. Запись атомарная,
// права 0600.
func Save(path string, c Config) error {
	c = c.Normalized()
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, append(data, '\n'), 0o600)
}
