package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/alex0x12/aristotle/internal/fsutil"
)

// Допустимые значения Kind
const (
	KindAnthropic = "anthropic"
	KindOpenAI    = "openai"
)

// DefaultContextBudget — бюджет контекста встроенных профилей, токенов.
const DefaultContextBudget = 64000

// DefaultCorpusK — сколько глав возвращает поиск, если модель не попросила иначе.
const DefaultCorpusK = 6

// Prices — цены профиля в долларах за миллион токенов.
type Prices struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

// Profile описывает один способ достучаться до модели.
type Profile struct {
	Name    string `json:"-"`
	Kind    string `json:"kind,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model,omitempty"`
	KeyEnv  string `json:"key_env,omitempty"`
	// Reasoning — рассуждающая модель (поддерживает reasoning.effort и не
	// принимает temperature).
	Reasoning bool `json:"reasoning,omitempty"`
	// MaxOutputTokens переопределяет provider.DefaultMaxTokens для этого
	// профиля. DefaultMaxTokens = 64000 — факт про Anthropic, а не про всех
	// провайдеров: если профиль указывает на модель с меньшим потолком
	// вывода (например, конкретную модель на openrouter), каждый запрос
	// будет отвечать четырёхсотой ошибкой без возможности починить иначе
	// как правкой кода. 0 значит "не задано" — используется умолчание
	// провайдера.
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`
	// ReasoningEffort — low, medium или high. Уходит только рассуждающим
	// моделям OpenAI-совместимых провайдеров; пусто — умолчание провайдера.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// Temperature — только для обычных моделей; nil — умолчание провайдера.
	Temperature *float64 `json:"temperature,omitempty"`
	// ContextBudget — потолок входа запроса в токенах; 0 — без урезания.
	ContextBudget int `json:"context_budget,omitempty"`
	// Prices — цены за миллион токенов; nil — стоимость не считается.
	Prices *Prices `json:"prices,omitempty"`
	// Key — ключ из keys.json, его подставляет app.Resolve. В config.json
	// не пишется никогда.
	Key string `json:"-"`
}

// profileData — внутренний тип для чтения и записи JSON: указатели
// различают опущенное поле и явное значение (false, 0, пустую строку).
type profileData struct {
	Kind            string   `json:"kind,omitempty"`
	BaseURL         string   `json:"base_url,omitempty"`
	Model           string   `json:"model,omitempty"`
	KeyEnv          string   `json:"key_env,omitempty"`
	Reasoning       *bool    `json:"reasoning,omitempty"`
	MaxOutputTokens *int     `json:"max_output_tokens,omitempty"`
	ReasoningEffort *string  `json:"reasoning_effort,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	ContextBudget   *int     `json:"context_budget,omitempty"`
	Prices          *Prices  `json:"prices,omitempty"`
}

// fileData — содержимое config.json: только отличия от встроенного.
type fileData struct {
	Default  string                 `json:"default,omitempty"`
	CorpusK  *int                   `json:"corpus_k,omitempty"`
	Profiles map[string]profileData `json:"profiles,omitempty"`
}

func (d profileData) empty() bool { return d == profileData{} }

// apply накладывает присутствующие в файле поля на профиль.
func apply(cur Profile, pd profileData) Profile {
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
	if pd.MaxOutputTokens != nil {
		cur.MaxOutputTokens = *pd.MaxOutputTokens
	}
	if pd.ReasoningEffort != nil {
		cur.ReasoningEffort = *pd.ReasoningEffort
	}
	if pd.Temperature != nil {
		t := *pd.Temperature
		cur.Temperature = &t
	}
	if pd.ContextBudget != nil {
		cur.ContextBudget = *pd.ContextBudget
	}
	if pd.Prices != nil {
		p := *pd.Prices
		cur.Prices = &p
	}
	return cur
}

type Config struct {
	Default  string             `json:"default,omitempty"`
	Profiles map[string]Profile `json:"profiles,omitempty"`
	// CorpusK — глав на поиск по умолчанию; модель вправе попросить своё k.
	CorpusK int `json:"corpus_k,omitempty"`
}

func Builtin() Config {
	return Config{
		Default: "deepseek",
		CorpusK: DefaultCorpusK,
		Profiles: map[string]Profile{
			"claude": {
				Kind:          KindAnthropic,
				Model:         "claude-opus-5",
				KeyEnv:        "ANTHROPIC_API_KEY",
				ContextBudget: DefaultContextBudget,
			},
			"deepseek": {
				Kind:          KindOpenAI,
				BaseURL:       "https://api.deepseek.com/v1",
				Model:         "deepseek-v4-pro",
				KeyEnv:        "DEEPSEEK_API_KEY",
				ContextBudget: DefaultContextBudget,
			},
			"openai": {
				Kind:          KindOpenAI,
				BaseURL:       "https://api.openai.com/v1",
				Model:         "gpt-5.6",
				KeyEnv:        "OPENAI_API_KEY",
				Reasoning:     true, // gpt-5.6 — рассуждающая модель с поддержкой reasoning.effort
				ContextBudget: DefaultContextBudget,
			},
			"openrouter": {
				Kind:          KindOpenAI,
				BaseURL:       "https://openrouter.ai/api/v1",
				Model:         "anthropic/claude-opus-5",
				KeyEnv:        "OPENROUTER_API_KEY",
				ContextBudget: DefaultContextBudget,
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

	// Промежуточный тип различает опущенные поля и явные значения.
	var over fileData
	if err := json.Unmarshal(data, &over); err != nil {
		return base, fmt.Errorf("конфиг %s: %w", path, err)
	}
	if over.Default != "" {
		base.Default = over.Default
	}
	if over.CorpusK != nil {
		base.CorpusK = *over.CorpusK
	}
	for name, pd := range over.Profiles {
		// Нового профиля в base нет — накладывается на пустой.
		base.Profiles[name] = apply(base.Profiles[name], pd)
	}
	return base, nil
}

// Save пишет в path только отличия c от встроенного — так, чтобы Load(path)
// вернул c. Встроенный профиль, которого в c нет, при следующей загрузке
// вернётся со своими умолчаниями: удалить встроенный нельзя, только сбросить.
// Запись атомарная, права 0600.
func Save(path string, c Config) error {
	base := Builtin()
	out := fileData{Profiles: map[string]profileData{}}
	if c.Default != base.Default {
		out.Default = c.Default
	}
	if c.CorpusK != base.CorpusK {
		k := c.CorpusK
		out.CorpusK = &k
	}
	for name, p := range c.Profiles {
		b, builtin := base.Profiles[name]
		d := diff(b, p, !builtin)
		if !builtin || !d.empty() {
			out.Profiles[name] = d
		}
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, append(data, '\n'), 0o600)
}

// diff — поля p, отличные от b; full — профиль новый, пишется целиком.
func diff(b, p Profile, full bool) profileData {
	var d profileData
	if full || p.Kind != b.Kind {
		d.Kind = p.Kind
	}
	if full || p.BaseURL != b.BaseURL {
		d.BaseURL = p.BaseURL
	}
	if full || p.Model != b.Model {
		d.Model = p.Model
	}
	if full || p.KeyEnv != b.KeyEnv {
		d.KeyEnv = p.KeyEnv
	}
	if full || p.Reasoning != b.Reasoning {
		v := p.Reasoning
		d.Reasoning = &v
	}
	if full || p.MaxOutputTokens != b.MaxOutputTokens {
		v := p.MaxOutputTokens
		d.MaxOutputTokens = &v
	}
	if full || p.ReasoningEffort != b.ReasoningEffort {
		v := p.ReasoningEffort
		d.ReasoningEffort = &v
	}
	if full || !sameFloat(p.Temperature, b.Temperature) {
		d.Temperature = p.Temperature
	}
	if full || p.ContextBudget != b.ContextBudget {
		v := p.ContextBudget
		d.ContextBudget = &v
	}
	if full || !samePrices(p.Prices, b.Prices) {
		d.Prices = p.Prices
	}
	return d
}

func sameFloat(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func samePrices(a, b *Prices) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
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

// APIKey — ключ профиля: переменная окружения, если задана, иначе ключ из
// keys.json, подставленный app.Resolve.
func (p Profile) APIKey() (string, error) {
	if p.KeyEnv != "" {
		if key := os.Getenv(p.KeyEnv); key != "" {
			return key, nil
		}
	}
	if p.Key != "" {
		return p.Key, nil
	}
	return "", fmt.Errorf("профиль %s: не задана переменная %s и нет ключа в keys.json",
		p.Name, p.KeyEnv)
}
