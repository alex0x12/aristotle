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
)

type Event struct {
	Kind       EventKind
	Text       string
	Call       ToolCall
	StopReason string
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
