package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

type Config struct {
	Default  string             `json:"default,omitempty"`
	Profiles map[string]Profile `json:"profiles,omitempty"`
}

func Builtin() Config {
	return Config{
		Default: "claude",
		Profiles: map[string]Profile{
			"claude": {
				Kind:   "anthropic",
				Model:  "claude-opus-5",
				KeyEnv: "ANTHROPIC_API_KEY",
			},
			"deepseek": {
				Kind:    "openai",
				BaseURL: "https://api.deepseek.com/v1",
				Model:   "deepseek-chat",
				KeyEnv:  "DEEPSEEK_API_KEY",
			},
			"openai": {
				Kind:    "openai",
				BaseURL: "https://api.openai.com/v1",
				Model:   "gpt-5.6",
				KeyEnv:  "OPENAI_API_KEY",
			},
			"openrouter": {
				Kind:    "openai",
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

	var over Config
	if err := json.Unmarshal(data, &over); err != nil {
		return base, fmt.Errorf("конфиг %s: %w", path, err)
	}
	if over.Default != "" {
		base.Default = over.Default
	}
	for name, p := range over.Profiles {
		cur, ok := base.Profiles[name]
		if !ok {
			base.Profiles[name] = p
			continue
		}
		if p.Kind != "" {
			cur.Kind = p.Kind
		}
		if p.BaseURL != "" {
			cur.BaseURL = p.BaseURL
		}
		if p.Model != "" {
			cur.Model = p.Model
		}
		if p.KeyEnv != "" {
			cur.KeyEnv = p.KeyEnv
		}
		cur.Reasoning = cur.Reasoning || p.Reasoning
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
