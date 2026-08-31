package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Допустимые значения Kind
const (
	KindAnthropic = "anthropic"
	KindOpenAI    = "openai"
)

// Profile описывает один способ достучаться до модели.
type Profile struct {
	Name      string `json:"-"`
	Kind      string `json:"kind,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	Model     string `json:"model,omitempty"`
	KeyEnv    string `json:"key_env,omitempty"`
	Reasoning bool   `json:"reasoning,omitempty"`
}

// profileData — внутренний тип для разбора JSON, позволяет различать
// опущенные поля и явные значения для Reasoning.
type profileData struct {
	Kind      string `json:"kind,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	Model     string `json:"model,omitempty"`
	KeyEnv    string `json:"key_env,omitempty"`
	Reasoning *bool  `json:"reasoning,omitempty"`
}

type Config struct {
	Default  string             `json:"default,omitempty"`
	Profiles map[string]Profile `json:"profiles,omitempty"`
}

func Builtin() Config {
	return Config{
		Default: "deepseek",
		Profiles: map[string]Profile{
			"claude": {
				Kind:   KindAnthropic,
				Model:  "claude-opus-5",
				KeyEnv: "ANTHROPIC_API_KEY",
			},
			"deepseek": {
				Kind:    KindOpenAI,
				BaseURL: "https://api.deepseek.com/v1",
				Model:   "deepseek-v4-pro",
				KeyEnv:  "DEEPSEEK_API_KEY",
			},
			"openai": {
				Kind:      KindOpenAI,
				BaseURL:   "https://api.openai.com/v1",
				Model:     "gpt-5.6",
				KeyEnv:    "OPENAI_API_KEY",
				Reasoning: true, // gpt-5.6 — рассуждающая модель с поддержкой reasoning.effort
			},
			"openrouter": {
				Kind:    KindOpenAI,
				BaseURL: "https://openrouter.ai/api/v1",
				Model:   "anthropic/claude-opus-5",
				KeyEnv:  "OPENROUTER_API_KEY",
			},
		},
	}
}

// Load накладывает JSON из path поверх встроенных профилей.
// Отсутствие файла — не ошибка: встроенных достаточно.
func Load(path string) (Config, error) {
	base := Builtin()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return base, nil
	}
	if err != nil {
		return base, fmt.Errorf("конфиг %s: %w", path, err)
	}

	// Разбираем в промежуточный тип, чтобы различать опущенные поля и явные значения.
	var over struct {
		Default  string                  `json:"default,omitempty"`
		Profiles map[string]profileData  `json:"profiles,omitempty"`
	}
	if err := json.Unmarshal(data, &over); err != nil {
		return base, fmt.Errorf("конфиг %s: %w", path, err)
	}
	if over.Default != "" {
		base.Default = over.Default
	}
	for name, pd := range over.Profiles {
		cur, ok := base.Profiles[name]
		if !ok {
			// Новый профиль: создаём из данных файла
			cur = Profile{
				Kind:    pd.Kind,
				BaseURL: pd.BaseURL,
				Model:   pd.Model,
				KeyEnv:  pd.KeyEnv,
			}
			if pd.Reasoning != nil {
				cur.Reasoning = *pd.Reasoning
			}
			base.Profiles[name] = cur
			continue
		}
		// Существующий профиль: наложение только присутствующих полей
		if pd.Kind != "" {
			cur.Kind = pd.Kind
		}
		if pd.BaseURL != "" {
			cur.BaseURL = pd.BaseURL
		}
		if pd.Model != "" {
			cur.Model = pd.Model
		}
		if pd.KeyEnv != "" {
			cur.KeyEnv = pd.KeyEnv
		}
		if pd.Reasoning != nil {
			cur.Reasoning = *pd.Reasoning
		}
		base.Profiles[name] = cur
	}
	return base, nil
}

func (c Config) Resolve(name string) (Profile, error) {
	if name == "" {
		name = c.Default
	}
	p, ok := c.Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("неизвестный профиль %q", name)
	}
	// Проверяем, что Kind имеет допустимое значение
	if p.Kind != KindAnthropic && p.Kind != KindOpenAI {
		return Profile{}, fmt.Errorf("профиль %q: недопустимое значение kind=%q, ожидается %q или %q",
			name, p.Kind, KindAnthropic, KindOpenAI)
	}
	p.Name = name
	return p, nil
}

func (p Profile) APIKey() (string, error) {
	key := os.Getenv(p.KeyEnv)
	if key == "" {
		return "", fmt.Errorf("профиль %s: не задана переменная %s",
			p.Name, p.KeyEnv)
	}
	return key, nil
}
