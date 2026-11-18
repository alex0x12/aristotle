package provider

import "context"

// DefaultMaxTokens — при стриминге занижать нечего, таймаутов тут не бывает.
const DefaultMaxTokens = 64000

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type ToolCall struct {
	ID    string
	Name  string
	Input map[string]any
}

type ToolResult struct {
	ID      string
	Content string
	IsError bool
}

type Message struct {
	Role        Role
	Text        string
	ToolCalls   []ToolCall
	ToolResults []ToolResult
}

type ToolDef struct {
	Name        string
	Description string
	Schema      map[string]any
}

type Request struct {
	System    string
	Messages  []Message
	Tools     []ToolDef
	MaxTokens int
}

func (r Request) EffectiveMaxTokens() int {
	return r.EffectiveMaxTokensFor(0)
}

// EffectiveMaxTokensFor — то же самое, но с учётом потолка вывода профиля.
// DefaultMaxTokens — факт про Anthropic, а не про всех провайдеров: если у
// профиля меньший потолок (например, конкретная модель на openrouter), явный
// per-запросный MaxTokens побеждает, иначе используется потолок профиля
// (profileMax, 0 значит "не задан"), и только если оба не заданы — умолчание.
func (r Request) EffectiveMaxTokensFor(profileMax int) int {
	if r.MaxTokens > 0 {
		return r.MaxTokens
	}
	if profileMax > 0 {
		return profileMax
	}
	return DefaultMaxTokens
}

type EventKind int

const (
	EventText EventKind = iota
	EventToolCall
	EventDone
	// EventUsage — расход токенов запроса; приходит один раз, перед EventDone.
	EventUsage
)

type Event struct {
	Kind       EventKind
	Text       string
	Call       ToolCall
	StopReason string
	Usage      Usage
}

// Usage — расход токенов запроса или суммы запросов. Input — входные токены
// без кэша; CacheRead и CacheWrite — прочитанные из кэша и записанные в него.
// Known = false: провайдер расход не сообщил, и сумма неизвестна.
type Usage struct {
	Input      int  `json:"input"`
	Output     int  `json:"output"`
	CacheRead  int  `json:"cache_read"`
	CacheWrite int  `json:"cache_write"`
	Known      bool `json:"known"`
}

// Prompt — весь вход запроса, вместе с кэшем: по нему калибруется оценка
// размера контекста.
func (u Usage) Prompt() int { return u.Input + u.CacheRead + u.CacheWrite }

// Total — все токены запроса.
func (u Usage) Total() int { return u.Prompt() + u.Output }

// Add складывает расход. Сумма известна, только если известны оба слагаемых:
// иначе показанная цифра была бы заниженной без всякой пометки.
func (u Usage) Add(o Usage) Usage {
	return Usage{
		Input:      u.Input + o.Input,
		Output:     u.Output + o.Output,
		CacheRead:  u.CacheRead + o.CacheRead,
		CacheWrite: u.CacheWrite + o.CacheWrite,
		Known:      u.Known && o.Known,
	}
}

type Stream interface {
	Next() bool
	Event() Event
	Err() error
	Close() error
}

type Provider interface {
	Stream(ctx context.Context, req Request) (Stream, error)
}

const searchDescription = `Ищет в корпусе сочинений Аристотеля и возвращает главы целиком.
Запрос формулируй на английском языке корпуса и в понятиях самого Аристотеля,
а не в словах собеседника: не "burnout", а "leisure and activity" или
"the mean in exertion". Псевдо-аристотелевские сочинения из выдачи исключены.`

// SearchCorpusTool возвращает определение инструмента. Значение обязано быть
// байт-в-байт одинаковым между запросами: оно входит в кэшируемый префикс.
func SearchCorpusTool() ToolDef {
	return ToolDef{
		Name:        "search_corpus",
		Description: searchDescription,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Поисковый запрос на английском, в понятиях корпуса.",
				},
				"group": map[string]any{
					"type": "string",
					"enum": []string{
						"logic", "physics", "psychology", "biology",
						"metaphysics", "ethics", "politics", "rhetoric-poetics",
					},
					"description": "Необязательное сужение области поиска.",
				},
				"k": map[string]any{
					"type":        "integer",
					"description": "Сколько глав вернуть, по умолчанию 6.",
				},
			},
			"required": []string{"query"},
		},
	}
}
