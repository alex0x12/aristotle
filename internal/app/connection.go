package app

import (
	"errors"
	"fmt"

	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/keys"
	"github.com/alex0x12/aristotle/internal/provider"
	"github.com/alex0x12/aristotle/internal/provider/openaicompat"
	"github.com/alex0x12/aristotle/internal/provider/opencodecli"
)

// ErrNoKey — ключа нет. Текст один для командной строки и веб-интерфейса: он
// называет оба места, где ключ задаётся.
var ErrNoKey = errors.New("ключ API не задан: откройте «Настройки» в веб-интерфейсе или выполните `aristotle key`")

// Connection — всё, что нужно для обращения к модели: один OpenAI-совместимый
// адрес, модель, ключ, усилие размышления и бюджет контекста. Профилей нет.
type Connection struct {
	BaseURL         string
	Model           string
	Key             string
	ReasoningEffort string // одно из config.ReasoningEfforts
	ContextBudget   int    // токенов на вход запроса; 0 — без урезания
}

// LoadConnection собирает подключение из config.json и keys.json.
//
//   - cfgErr — config.json не читается (в том числе старого формата). Это не
//     отказ: conn собран из умолчаний, а вызывающий решает, как предупредить
//     (командная строка пишет строку на stderr и идёт дальше; веб показывает
//     предупреждение). Ошибка хранит путь к файлу — для командной строки.
//   - err — keys.json не читается (*keys.UnreadableError); conn тогда без ключа.
//     Отсутствие ключа ошибкой не считается: conn.Key пуст, и проверяет это
//     NewProvider (ErrNoKey).
func LoadConnection(paths Paths) (conn Connection, cfgErr error, err error) {
	cfg, cfgErr := config.Load(paths.Config)
	conn = Connection{BaseURL: cfg.BaseURL, Model: cfg.Model,
		ReasoningEffort: cfg.ReasoningEffort, ContextBudget: cfg.ContextBudget}
	key, err := keys.Store{Path: paths.Keys}.Get()
	if err != nil {
		return conn, cfgErr, err
	}
	conn.Key = key
	return conn, cfgErr, nil
}

// NewProvider создаёт провайдера. Без ключа — ErrNoKey, до всякой работы;
// для opencode-cli ключ не нужен.
// Настройки проверяются так же, как при сохранении настроек.
func NewProvider(c Connection) (provider.Provider, error) {
	if c.Key == "" && c.BaseURL != config.OpenCodeCLI {
		return nil, ErrNoKey
	}
	cfg := config.Config{BaseURL: c.BaseURL, Model: c.Model,
		ReasoningEffort: c.ReasoningEffort, ContextBudget: c.ContextBudget}.Normalized()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("настройки: %w", err)
	}
	if cfg.BaseURL == config.OpenCodeCLI {
		return opencodecli.New(cfg.Model, cfg.ReasoningEffort), nil
	}
	if _, err := keys.Normalize(c.Key); err != nil {
		return nil, errors.New("ключ API недопустим") // сам ключ в тексте не повторяется
	}
	return openaicompat.New(cfg.BaseURL, cfg.Model, c.Key, cfg.ReasoningEffort), nil
}
