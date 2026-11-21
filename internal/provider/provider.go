package provider

import "context"

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
	// Reasoning — рассуждение модели (reasoning_content) к этому ответу. В режиме
	// размышления сервер требует присылать его назад в каждом следующем запросе,
	// где есть инструменты; у старых записей журнала его нет — тогда поле не
	// отправляется вовсе.
	Reasoning string
}

type ToolDef struct {
	Name        string
	Description string
	Schema      map[string]any
}

// Request — один запрос к модели. Потолка вывода, температуры и прочих ручек
// здесь нет намеренно: настраивается только подключение (адрес, модель, ключ),
// остальное решает провайдер.
type Request struct {
	System   string
	Messages []Message
	Tools    []ToolDef
}

type EventKind int

const (
	EventText EventKind = iota
	EventToolCall
	EventDone
	// EventUsage — расход токенов запроса; приходит один раз, перед EventDone.
	EventUsage
	// EventReasoning — кусок рассуждения модели (Text); на экран не выводится,
	// но сохраняется и уходит назад вместе с ответом.
	EventReasoning
	// EventNotice — разовое предупреждение провайдера (Text): например, что
	// сервер не принял параметры режима размышления.
	EventNotice
)

type Event struct {
	Kind       EventKind
	Text       string
	Call       ToolCall
	StopReason string
	Usage      Usage
}

// Usage — расход токенов запроса или суммы запросов. Input — входные токены
// без кэша; CacheRead — прочитанные из кэша. CacheWrite только читается из
// старых журналов (его сообщал прежний провайдер): новые запросы его не заполняют.
// Known = false: провайдер расход не сообщил, и сумма неизвестна.
type Usage struct {
	Input      int  `json:"input"`
	Output     int  `json:"output"`
	CacheRead  int  `json:"cache_read"`
	CacheWrite int  `json:"cache_write,omitempty"`
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
Запрос формулируй на языке выбранного корпуса и в понятиях самого Аристотеля,
а не в словах собеседника: в английском — "leisure and activity" вместо
"burnout", в русском — по-русски. Псевдо-аристотелевские сочинения из выдачи
исключены.`

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
					"description": "Поисковый запрос на языке корпуса (английском или русском), в понятиях Аристотеля.",
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
